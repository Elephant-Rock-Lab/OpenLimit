package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"openlimit/internal/config"
	openaischema "openlimit/internal/schema/openai"
)

// setupTestExecutor creates an executor with a registry and manager backed by a mock MCP server.
func setupTestExecutor(t *testing.T) (*Executor, *httptest.Server) {
	t.Helper()
	server := mockMCPServer(t)

	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{Name: "weather", URL: server.URL, ToolPrefix: "weather"},
		},
	}, registry, nil)

	ctx := context.Background()
	mgr.Start(ctx)

	// Wait for connection
	time.Sleep(100 * time.Millisecond)

	executor := NewExecutor(registry, mgr, 5, 120*time.Second, nil, nil)
	return executor, server
}

func TestExecutorSingleToolCall(t *testing.T) {
	executor, server := setupTestExecutor(t)
	defer server.Close()

	// Simulate a provider response with an MCP tool call
	providerResp := &openaischema.ChatCompletionResponse{
		ID:    "chatcmpl-1",
		Model: "gpt-4",
		Choices: []openaischema.Choice{
			{
				Index: 0,
				Message: openaischema.ChatMessage{
					Role: "assistant",
					ToolCalls: json.RawMessage(`[
						{"id":"call_1","type":"function","function":{"name":"weather.get_forecast","arguments":"{\"location\":\"NYC\"}"}}
					]`),
				},
				FinishReason: "tool_calls",
			},
		},
	}

	callCount := 0
	callProviderFn := func(ctx context.Context, req openaischema.ChatCompletionRequest) (*openaischema.ChatCompletionResponse, error) {
		callCount++
		// Return a final response (no more tool calls)
		return &openaischema.ChatCompletionResponse{
			ID:    "chatcmpl-final",
			Model: "gpt-4",
			Choices: []openaischema.Choice{
				{
					Index:        0,
					Message:      openaischema.ChatMessage{Role: "assistant", Content: json.RawMessage(`"The weather in NYC is sunny"`), ToolCalls: nil},
					FinishReason: "stop",
				},
			},
		}, nil
	}

	req := &openaischema.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"What's the weather?"`)}},
	}

	result, err := executor.Execute(context.Background(), req, providerResp, callProviderFn)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.RoundsUsed != 1 {
		t.Errorf("expected 1 round, got %d", result.RoundsUsed)
	}
	if result.MaxReached {
		t.Error("should not have reached max rounds")
	}
	if result.Timeout {
		t.Error("should not have timed out")
	}
	if callCount != 1 {
		t.Errorf("expected 1 provider call, got %d", callCount)
	}
	if result.Response.ID != "chatcmpl-final" {
		t.Errorf("expected final response, got %q", result.Response.ID)
	}
}

func TestExecutorNoToolCalls(t *testing.T) {
	executor, server := setupTestExecutor(t)
	defer server.Close()

	providerResp := &openaischema.ChatCompletionResponse{
		ID:    "chatcmpl-1",
		Model: "gpt-4",
		Choices: []openaischema.Choice{
			{
				Index:        0,
				Message:      openaischema.ChatMessage{Role: "assistant", Content: json.RawMessage(`"Hello!"`)},
				FinishReason: "stop",
			},
		},
	}

	callProviderFn := func(ctx context.Context, req openaischema.ChatCompletionRequest) (*openaischema.ChatCompletionResponse, error) {
		t.Fatal("should not call provider when there are no tool calls")
		return nil, nil
	}

	req := &openaischema.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
	}

	result, err := executor.Execute(context.Background(), req, providerResp, callProviderFn)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.RoundsUsed != 0 {
		t.Errorf("expected 0 rounds, got %d", result.RoundsUsed)
	}
}

func TestExecutorMaxRoundsReached(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{Name: "weather", URL: server.URL, ToolPrefix: "weather"},
		},
	}, registry, nil)
	mgr.Start(context.Background())
	time.Sleep(100 * time.Millisecond)

	// Create executor with max 2 rounds
	executor := NewExecutor(registry, mgr, 2, 120*time.Second, nil, nil)

	round := 0
	// Provider always returns a tool call → will hit max rounds
	alwaysToolCallResp := &openaischema.ChatCompletionResponse{
		ID:    "chatcmpl-loop",
		Model: "gpt-4",
		Choices: []openaischema.Choice{
			{
				Index: 0,
				Message: openaischema.ChatMessage{
					Role: "assistant",
					ToolCalls: json.RawMessage(`[
						{"id":"call_loop","type":"function","function":{"name":"weather.get_forecast","arguments":"{\"location\":\"NYC\"}"}}
					]`),
				},
				FinishReason: "tool_calls",
			},
		},
	}

	callProviderFn := func(ctx context.Context, req openaischema.ChatCompletionRequest) (*openaischema.ChatCompletionResponse, error) {
		round++
		return alwaysToolCallResp, nil
	}

	req := &openaischema.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"weather"`)}},
	}

	result, err := executor.Execute(context.Background(), req, alwaysToolCallResp, callProviderFn)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.MaxReached {
		t.Error("expected max rounds to be reached")
	}
	if result.RoundsUsed != 2 {
		t.Errorf("expected 2 rounds, got %d", result.RoundsUsed)
	}
}

func TestExecutorCumulativeTimeout(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{Name: "weather", URL: server.URL, ToolPrefix: "weather"},
		},
	}, registry, nil)
	mgr.Start(context.Background())
	time.Sleep(100 * time.Millisecond)

	// Very short timeout
	executor := NewExecutor(registry, mgr, 10, 1*time.Nanosecond, nil, nil)

	providerResp := &openaischema.ChatCompletionResponse{
		ID:    "chatcmpl-1",
		Model: "gpt-4",
		Choices: []openaischema.Choice{
			{
				Index: 0,
				Message: openaischema.ChatMessage{
					Role: "assistant",
					ToolCalls: json.RawMessage(`[
						{"id":"call_1","type":"function","function":{"name":"weather.get_forecast","arguments":"{}"}}
					]`),
				},
				FinishReason: "tool_calls",
			},
		},
	}

	callProviderFn := func(ctx context.Context, req openaischema.ChatCompletionRequest) (*openaischema.ChatCompletionResponse, error) {
		return providerResp, nil
	}

	req := &openaischema.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"weather"`)}},
	}

	result, err := executor.Execute(context.Background(), req, providerResp, callProviderFn)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Timeout {
		t.Error("expected timeout")
	}
}

func TestExecutorNonMCPToolCallsPassThrough(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{Name: "weather", URL: server.URL, ToolPrefix: "weather"},
		},
	}, registry, nil)
	mgr.Start(context.Background())
	time.Sleep(100 * time.Millisecond)

	executor := NewExecutor(registry, mgr, 5, 120*time.Second, nil, nil)

	// Response with only non-MCP tool calls
	providerResp := &openaischema.ChatCompletionResponse{
		ID:    "chatcmpl-1",
		Model: "gpt-4",
		Choices: []openaischema.Choice{
			{
				Index: 0,
				Message: openaischema.ChatMessage{
					Role: "assistant",
					ToolCalls: json.RawMessage(`[
						{"id":"call_1","type":"function","function":{"name":"my_custom_tool","arguments":"{}"}}
					]`),
				},
				FinishReason: "tool_calls",
			},
		},
	}

	callProviderFn := func(ctx context.Context, req openaischema.ChatCompletionRequest) (*openaischema.ChatCompletionResponse, error) {
		t.Fatal("should not call provider for non-MCP tool calls")
		return nil, nil
	}

	req := &openaischema.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"test"`)}},
	}

	result, err := executor.Execute(context.Background(), req, providerResp, callProviderFn)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.RoundsUsed != 0 {
		t.Errorf("expected 0 rounds for non-MCP tool calls, got %d", result.RoundsUsed)
	}
	// Response should be returned as-is (with the non-MCP tool calls)
	if result.Response.ID != "chatcmpl-1" {
		t.Errorf("expected original response to pass through")
	}
}

func TestExtractTextContent(t *testing.T) {
	tests := []struct {
		name     string
		result   *CallToolResult
		expected string
	}{
		{
			name: "single text block",
			result: &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: "hello"}},
			},
			expected: "hello",
		},
		{
			name: "multiple text blocks",
			result: &CallToolResult{
				Content: []ToolContent{
					{Type: "text", Text: "part1"},
					{Type: "text", Text: "part2"},
				},
			},
			expected: `["part1","part2"]`,
		},
		{
			name: "no text content",
			result: &CallToolResult{
				Content: []ToolContent{},
			},
			expected: "{}",
		},
		{
			name:     "nil result",
			result:   nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTextContent(tt.result)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
