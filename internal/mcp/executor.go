package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	openaischema "openlimit/internal/schema/openai"
)

// Executor handles MCP tool-call interception and multi-turn execution.
type Executor struct {
	registry  *Registry
	manager   *Manager
	logger    *slog.Logger
	maxRounds int
	maxTotal  time.Duration
	toolLogFn ToolLogFunc
}

// ToolLogFunc records a tool execution to the audit log.
type ToolLogFunc func(ctx context.Context, entry ToolLogEntry)

// ToolLogEntry represents an auditable tool execution.
type ToolLogEntry struct {
	RequestID    string
	ProjectID    string
	VirtualKeyID string
	ServerName   string
	ToolName     string
	Arguments    map[string]any
	Result       json.RawMessage
	IsError      bool
	DurationMS   int64
}

// NewExecutor creates a tool execution handler.
func NewExecutor(registry *Registry, manager *Manager, maxRounds int, maxTotal time.Duration, toolLogFn ToolLogFunc, logger *slog.Logger) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		registry:  registry,
		manager:   manager,
		logger:    logger.With("component", "mcp_executor"),
		maxRounds: maxRounds,
		maxTotal:  maxTotal,
		toolLogFn: toolLogFn,
	}
}

// ExecuteResult holds the outcome of tool execution.
type ExecuteResult struct {
	Response   *openaischema.ChatCompletionResponse
	RoundsUsed int
	MaxReached bool
	Timeout    bool
}

// Execute checks a provider response for MCP tool calls, executes them,
// and continues the multi-turn loop until no more MCP tool calls remain
// or limits are reached.
//
// The callProviderFn is used to re-invoke the LLM after tool results are appended.
func (e *Executor) Execute(
	ctx context.Context,
	req *openaischema.ChatCompletionRequest,
	resp *openaischema.ChatCompletionResponse,
	callProviderFn func(ctx context.Context, req openaischema.ChatCompletionRequest) (*openaischema.ChatCompletionResponse, error),
) (*ExecuteResult, error) {
	deadline := time.Time{}
	if e.maxTotal > 0 {
		deadline = time.Now().Add(e.maxTotal)
	}

	currentResp := resp
	round := 0

	for round < e.maxRounds {
		mcpCalls, nonMCPCalls := e.partitionToolCalls(currentResp)

		if len(mcpCalls) == 0 {
			// No MCP tool calls — we're done
			break
		}

		// Check deadline
		if !deadline.IsZero() && time.Now().After(deadline) {
			e.logger.Warn("MCP execution timed out", "round", round, "max_total", e.maxTotal)
			return &ExecuteResult{Response: currentResp, RoundsUsed: round, Timeout: true}, nil
		}

		// Execute MCP tool calls
		toolMessages, err := e.executeToolCalls(ctx, mcpCalls)
		if err != nil {
			return nil, fmt.Errorf("MCP tool execution failed: %w", err)
		}

		// Build updated messages: original messages + assistant response + tool results
		assistantMsg := e.buildAssistantMessage(currentResp, mcpCalls, nonMCPCalls)
		req.Messages = append(req.Messages, assistantMsg)
		req.Messages = append(req.Messages, toolMessages...)

		round++

		// Re-invoke provider with deadline-aware context
		nextResp, roundErr := e.invokeWithDeadline(ctx, deadline, *req, callProviderFn)
		if roundErr != nil {
			if roundErr == context.DeadlineExceeded {
				e.logger.Warn("MCP execution timed out during re-invocation", "round", round)
				return &ExecuteResult{Response: currentResp, RoundsUsed: round, Timeout: true}, nil
			}
			return nil, fmt.Errorf("provider re-invocation failed after tool execution: %w", roundErr)
		}
		currentResp = nextResp
	}

	maxReached := round >= e.maxRounds && e.hasToolCalls(currentResp)
	return &ExecuteResult{
		Response:   currentResp,
		RoundsUsed: round,
		MaxReached: maxReached,
	}, nil
}

// partitionToolCalls splits tool calls into MCP and non-MCP groups.
func (e *Executor) partitionToolCalls(resp *openaischema.ChatCompletionResponse) ([]ToolCallInfo, []ToolCallInfo) {
	if resp == nil || len(resp.Choices) == 0 {
		return nil, nil
	}

	choice := resp.Choices[0]
	if len(choice.Message.ToolCalls) == 0 || string(choice.Message.ToolCalls) == "null" {
		return nil, nil
	}

	var calls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(choice.Message.ToolCalls, &calls); err != nil {
		e.logger.Warn("failed to parse tool_calls", "error", err)
		return nil, nil
	}

	var mcpCalls, nonMCPCalls []ToolCallInfo
	for _, c := range calls {
		info := ToolCallInfo{
			ID:        c.ID,
			Name:      c.Function.Name,
			Arguments: c.Function.Arguments,
		}
		if _, ok := e.registry.ToolByName(c.Function.Name); ok {
			mcpCalls = append(mcpCalls, info)
		} else {
			nonMCPCalls = append(nonMCPCalls, info)
		}
	}

	return mcpCalls, nonMCPCalls
}

// ToolCallInfo represents a parsed tool call from the LLM response.
type ToolCallInfo struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// executeToolCalls executes MCP tool calls in parallel and returns tool result messages.
func (e *Executor) executeToolCalls(ctx context.Context, calls []ToolCallInfo) ([]openaischema.ChatMessage, error) {
	type toolResult struct {
		callIndex int
		content   string
		isError   bool
		duration  time.Duration
		server    string
		rawName   string
		args      map[string]any
	}

	results := make([]toolResult, len(calls))
	done := make(chan struct{}, len(calls))

	for i, call := range calls {
		go func(idx int, c ToolCallInfo) {
			defer func() { done <- struct{}{} }()

			tool, ok := e.registry.ToolByName(c.Name)
			if !ok {
				results[idx] = toolResult{
					callIndex: idx,
					content:   fmt.Sprintf("Tool %q not found in MCP registry", c.Name),
					isError:   true,
				}
				return
			}

			var args map[string]any
			if len(c.Arguments) > 0 {
				_ = json.Unmarshal(c.Arguments, &args)
			}

			start := time.Now()
			result, err := e.callToolOnServer(ctx, tool, args)
			duration := time.Since(start)

			if err != nil {
				results[idx] = toolResult{
					callIndex: idx,
					content:   fmt.Sprintf("MCP tool error: %s", err.Error()),
					isError:   true,
					duration:  duration,
					server:    tool.ServerName,
					rawName:   tool.RawName,
					args:      args,
				}
				return
			}

			content := extractTextContent(result)

			// Log the tool execution
			if e.toolLogFn != nil {
				resultJSON, _ := json.Marshal(result)
				e.toolLogFn(ctx, ToolLogEntry{
					ServerName: tool.ServerName,
					ToolName:   tool.RawName,
					Arguments:  args,
					Result:     resultJSON,
					IsError:    result.IsError,
					DurationMS: duration.Milliseconds(),
				})
			}

			results[idx] = toolResult{
				callIndex: idx,
				content:   content,
				isError:   result.IsError,
				duration:  duration,
				server:    tool.ServerName,
				rawName:   tool.RawName,
				args:      args,
			}
		}(i, call)
	}

	for range calls {
		<-done
	}

	// Build tool result messages in order
	messages := make([]openaischema.ChatMessage, len(calls))
	for i, r := range results {
		messages[i] = openaischema.ChatMessage{
			Role:       "tool",
			ToolCallID: calls[i].ID,
			Content:    json.RawMessage(`"` + jsonEscapeString(r.content) + `"`),
		}

		if r.server != "" {
			e.logger.Info("MCP tool executed",
				"tool", calls[i].Name,
				"server", r.server,
				"duration_ms", r.duration.Milliseconds(),
				"is_error", r.isError,
			)
		}
	}

	return messages, nil
}

// callToolOnServer finds the MCP client for the tool's server and calls it.
func (e *Executor) callToolOnServer(ctx context.Context, tool Tool, args map[string]any) (*CallToolResult, error) {
	if e.manager == nil {
		return nil, fmt.Errorf("MCP manager not available")
	}

	client := e.manager.GetClient(tool.ServerName)
	if client == nil {
		return nil, fmt.Errorf("MCP server %q not connected", tool.ServerName)
	}

	return client.CallTool(ctx, tool.RawName, args)
}

// buildAssistantMessage constructs the assistant message with tool_calls for the multi-turn conversation.
func (e *Executor) buildAssistantMessage(resp *openaischema.ChatCompletionResponse, mcpCalls, nonMCPCalls []ToolCallInfo) openaischema.ChatMessage {
	// Include all tool calls (MCP + non-MCP) in the assistant message
	// so the conversation history is consistent for the LLM
	return openaischema.ChatMessage{
		Role:      "assistant",
		Content:   resp.Choices[0].Message.Content,
		ToolCalls: resp.Choices[0].Message.ToolCalls,
	}
}

// hasToolCalls checks if a response has tool calls.
func (e *Executor) hasToolCalls(resp *openaischema.ChatCompletionResponse) bool {
	if resp == nil || len(resp.Choices) == 0 {
		return false
	}
	return len(resp.Choices[0].Message.ToolCalls) > 0 && string(resp.Choices[0].Message.ToolCalls) != "null"
}

// invokeWithDeadline calls the provider with a deadline-aware context.
// The cancel function is called immediately after the provider call
// completes — no defer-in-loop context leak.
func (e *Executor) invokeWithDeadline(
	ctx context.Context,
	deadline time.Time,
	req openaischema.ChatCompletionRequest,
	callProviderFn func(ctx context.Context, req openaischema.ChatCompletionRequest) (*openaischema.ChatCompletionResponse, error),
) (*openaischema.ChatCompletionResponse, error) {
	callCtx := ctx
	if !deadline.IsZero() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, context.DeadlineExceeded
		}
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, remaining)
		defer cancel()
	}

	return callProviderFn(callCtx, req)
}

// extractTextContent extracts text from tool result content blocks.
func extractTextContent(result *CallToolResult) string {
	if result == nil {
		return ""
	}
	var texts []string
	for _, c := range result.Content {
		if c.Type == "text" && c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	if len(texts) == 0 {
		return "{}"
	}
	if len(texts) == 1 {
		return texts[0]
	}
	// Multiple text blocks → JSON array
	b, _ := json.Marshal(texts)
	return string(b)
}

// jsonEscapeString escapes a string for embedding in a JSON string.
func jsonEscapeString(s string) string {
	b, _ := json.Marshal(s)
	// Strip surrounding quotes
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return s
}
