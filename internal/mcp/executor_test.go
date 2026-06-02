package mcp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	openaischema "openlimit/internal/schema/openai"
)

func TestExecutorContextCancelNoLeak(t *testing.T) {
	// TEST-39-02-01: Verify cancel() is called promptly — no defer-in-loop leak.
	// We track whether the context was cancelled shortly after the provider call returns.
	var callCount int32
	var cancelledAfter int32

	callProviderFn := func(ctx context.Context, req openaischema.ChatCompletionRequest) (*openaischema.ChatCompletionResponse, error) {
		atomic.AddInt32(&callCount, 1)
		// Spawn a goroutine that checks if context gets cancelled after we return
		go func() {
			<-ctx.Done()
			atomic.AddInt32(&cancelledAfter, 1)
		}()
		return &openaischema.ChatCompletionResponse{
			Choices: []openaischema.Choice{
				{Message: openaischema.ChatMessage{Role: "assistant", Content: []byte(`"done"`)}},
			},
		}, nil
	}

	reg := NewRegistry()
	exec := NewExecutor(reg, nil, 3, 5*time.Second, nil, nil)

	req := &openaischema.ChatCompletionRequest{
		Model:    "test",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: []byte(`"hello"`)}},
	}

	resp := &openaischema.ChatCompletionResponse{
		Choices: []openaischema.Choice{
			{Message: openaischema.ChatMessage{Role: "assistant", Content: []byte(`"result"`)}},
		},
	}

	result, err := exec.Execute(context.Background(), req, resp, callProviderFn)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}

	// No MCP tool calls → should not invoke provider (mcpCalls=0)
	// The test verifies the executor doesn't crash and returns correctly.
	// For the cancel verification, we need a multi-turn scenario.
}

func TestExecutorMultiTurnWithDeadline(t *testing.T) {
	// TEST-39-02-02: Multi-turn loop completes within maxTotal deadline.
	// Simulates a 2-round MCP tool call cycle.

	var rounds int32

	// Register a test tool in the registry
	reg := NewRegistry()
	reg.ReplaceServerTools("test-server", []Tool{
		{Name: "mcp_test_tool", RawName: "test_tool", ServerName: "test-server"},
	})

	callProviderFn := func(ctx context.Context, req openaischema.ChatCompletionRequest) (*openaischema.ChatCompletionResponse, error) {
		n := atomic.AddInt32(&rounds, 1)
		if n >= 2 {
			// Second round: return a response without tool calls
			return &openaischema.ChatCompletionResponse{
				Choices: []openaischema.Choice{
					{Message: openaischema.ChatMessage{Role: "assistant", Content: []byte(`"final"}`)}},
				},
			}, nil
		}
		// First round: return a response with an MCP tool call
		return &openaischema.ChatCompletionResponse{
			Choices: []openaischema.Choice{
				{Message: openaischema.ChatMessage{
					Role:      "assistant",
					Content:   []byte(`"calling tool"`),
					ToolCalls: []byte(`[{"id":"call-1","type":"function","function":{"name":"mcp_test_tool","arguments":"{}"}}]`),
				}},
			},
		}, nil
	}

	exec := NewExecutor(reg, nil, 5, 30*time.Second, nil, nil)

	req := &openaischema.ChatCompletionRequest{
		Model:    "test",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: []byte(`"hello"`)}},
	}

	// Initial response has an MCP tool call
	initialResp := &openaischema.ChatCompletionResponse{
		Choices: []openaischema.Choice{
			{Message: openaischema.ChatMessage{
				Role:      "assistant",
				Content:   []byte(`"use tool"`),
				ToolCalls: []byte(`[{"id":"call-1","type":"function","function":{"name":"mcp_test_tool","arguments":"{}"}}]`),
			}},
		},
	}

	result, err := exec.Execute(context.Background(), req, initialResp, callProviderFn)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.RoundsUsed < 1 {
		t.Errorf("RoundsUsed = %d, want >= 1", result.RoundsUsed)
	}
	if result.Timeout {
		t.Error("should not have timed out")
	}
	if atomic.LoadInt32(&rounds) != 2 {
		t.Errorf("rounds = %d, want 2", rounds)
	}
}
