package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"openlimit/internal/schema/openai"
)

// ServerChatExecutor executes chat completions for MCP server mode tool calls.
// It resolves tool names to virtual keys and routes requests through the
// provider pipeline.
type ServerChatExecutor struct {
	keyResolver KeyResolver
	chatHandler ChatHandler
	logger      *slog.Logger
}

// KeyResolver looks up a virtual key by its MCP tool name.
type KeyResolver interface {
	ResolveToolName(ctx context.Context, toolName string) (*ResolvedKey, error)
}

// ResolvedKey is the result of resolving a tool name to a virtual key.
type ResolvedKey struct {
	KeyID            string
	KeyPrefix        string
	KeyHash          string
	ProjectID        string
	KeyName          string
	AllowedModels    []string
	AllowedProviders []string
	RPMLimit         int
	TPMLimit         int
	BudgetLimitUSD   float64
	BudgetPeriod     string
}

// ChatHandler executes a chat completion request and returns the response.
// The identity parameter is optional and typed as any to avoid circular imports;
// implementations type-assert to their concrete GovernanceIdentity type.
type ChatHandler interface {
	ExecuteForMCP(ctx context.Context, req openai.ChatCompletionRequest, identity any) (*openai.ChatCompletionResponse, error)
}

// IdentityProvider is implemented by identity structs to provide governance
// parameters without creating a circular import dependency.
// The openaiapi.Handler type-asserts the `any` identity parameter to
// IdentityProvider when building a GovernanceIdentity.
type IdentityProvider interface {
	GetProjectID() string
	GetVirtualKeyID() string
	GetKeyPrefix() string
	GetName() string
	GetAllowedModels() []string
	GetAllowedProviders() []string
	GetRPMLimit() int
	GetTPMLimit() int
	GetBudgetLimitUSD() float64
	GetBudgetPeriod() string
	GetSource() string
	GetSkipRateLimit() bool
	GetSkipBudget() bool
	GetSkipUsageLog() bool
}

// MCPIdentity carries per-key governance parameters for MCP tool calls.
// It implements IdentityProvider so the openaiapi.Handler can extract
// governance fields via interface methods without importing this package.
type MCPIdentity struct {
	ProjectID        string
	VirtualKeyID     string
	KeyPrefix        string
	Name             string
	AllowedModels    []string
	AllowedProviders []string
	RPMLimit         int
	TPMLimit         int
	BudgetLimitUSD   float64
	BudgetPeriod     string
	Source           string
	SkipRateLimit    bool
	SkipBudget       bool
	SkipUsageLog     bool
}

func (m *MCPIdentity) GetProjectID() string          { return m.ProjectID }
func (m *MCPIdentity) GetVirtualKeyID() string       { return m.VirtualKeyID }
func (m *MCPIdentity) GetKeyPrefix() string          { return m.KeyPrefix }
func (m *MCPIdentity) GetName() string               { return m.Name }
func (m *MCPIdentity) GetAllowedModels() []string    { return m.AllowedModels }
func (m *MCPIdentity) GetAllowedProviders() []string { return m.AllowedProviders }
func (m *MCPIdentity) GetRPMLimit() int              { return m.RPMLimit }
func (m *MCPIdentity) GetTPMLimit() int              { return m.TPMLimit }
func (m *MCPIdentity) GetBudgetLimitUSD() float64    { return m.BudgetLimitUSD }
func (m *MCPIdentity) GetBudgetPeriod() string       { return m.BudgetPeriod }
func (m *MCPIdentity) GetSource() string             { return m.Source }
func (m *MCPIdentity) GetSkipRateLimit() bool        { return m.SkipRateLimit }
func (m *MCPIdentity) GetSkipBudget() bool           { return m.SkipBudget }
func (m *MCPIdentity) GetSkipUsageLog() bool         { return m.SkipUsageLog }

// Verify MCPIdentity implements IdentityProvider at compile time.
var _ IdentityProvider = (*MCPIdentity)(nil)

// GovernanceBlockedError is an interface for errors from the governance pipeline.
// The openaiapi.GovernanceError implements this interface via structural
// typing — no import from openaiapi needed.
type GovernanceBlockedError interface {
	error
	GovernanceBlocked() bool
}

// NewServerChatExecutor creates a new executor for MCP server tool calls.
func NewServerChatExecutor(resolver KeyResolver, handler ChatHandler, logger *slog.Logger) *ServerChatExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &ServerChatExecutor{
		keyResolver: resolver,
		chatHandler: handler,
		logger:      logger.With("component", "mcp_server_executor"),
	}
}

// Execute runs a tool call: resolves the tool name, builds a chat request,
// executes it, and returns the result.
func (e *ServerChatExecutor) Execute(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
	// 1. Resolve tool name → virtual key
	key, err := e.keyResolver.ResolveToolName(ctx, toolName)
	if err != nil {
		return nil, fmt.Errorf("tool %q not found: %w", toolName, err)
	}

	// 2. Build the chat completion request from tool arguments
	req, err := e.buildRequest(args)
	if err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// 3. Validate model against key's allowed models
	if len(key.AllowedModels) > 0 {
		allowed := false
		for _, m := range key.AllowedModels {
			if m == req.Model || matchGlob(m, req.Model) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("model %q not allowed for key %q", req.Model, key.KeyName)
		}
	}

	// 4. Set a timeout for the execution
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
	}

	// 5. Execute the chat completion
	start := time.Now()
	identity := &MCPIdentity{
		ProjectID:        key.ProjectID,
		VirtualKeyID:     key.KeyID,
		KeyPrefix:        key.KeyPrefix,
		Name:             key.KeyName,
		AllowedModels:    key.AllowedModels,
		AllowedProviders: key.AllowedProviders,
		RPMLimit:         key.RPMLimit,
		TPMLimit:         key.TPMLimit,
		BudgetLimitUSD:   key.BudgetLimitUSD,
		BudgetPeriod:     key.BudgetPeriod,
		Source:           "mcp_tool",
		SkipRateLimit:    false,
		SkipBudget:       false,
		SkipUsageLog:     false,
	}
	resp, err := e.chatHandler.ExecuteForMCP(ctx, req, identity)
	if err != nil {
		e.logger.Error("chat completion failed",
			"tool", toolName, "key", key.KeyID,
			"model", req.Model, "error", err, "duration_ms", time.Since(start).Milliseconds(),
		)
		return nil, fmt.Errorf("chat completion failed: %w", err)
	}

	e.logger.Info("tool call executed",
		"tool", toolName, "key", key.KeyID,
		"model", resp.Model,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	// 6. Extract the response content
	content := extractResponseContent(resp)

	result := &ChatResult{
		Content:      content,
		Model:        resp.Model,
		FinishReason: extractFinishReason(resp),
	}

	if resp.Usage != nil {
		result.Usage = &ChatUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}

	return result, nil
}

// buildRequest converts MCP tool arguments to a chat completion request.
func (e *ServerChatExecutor) buildRequest(args map[string]any) (openai.ChatCompletionRequest, error) {
	var req openai.ChatCompletionRequest

	// Model (required)
	model, _ := args["model"].(string)
	if model == "" {
		return req, fmt.Errorf("model is required")
	}
	req.Model = model

	// Messages (required)
	msgsRaw, ok := args["messages"]
	if !ok {
		return req, fmt.Errorf("messages are required")
	}

	msgs, ok := msgsRaw.([]any)
	if !ok {
		return req, fmt.Errorf("messages must be an array")
	}

	for i, m := range msgs {
		msgMap, ok := m.(map[string]any)
		if !ok {
			return req, fmt.Errorf("message %d: must be an object", i)
		}
		role, _ := msgMap["role"].(string)
		if role == "" {
			return req, fmt.Errorf("message %d: role is required", i)
		}

		content, _ := msgMap["content"].(string)
		contentJSON, _ := json.Marshal(content)

		req.Messages = append(req.Messages, openai.ChatMessage{
			Role:    role,
			Content: contentJSON,
		})
	}

	if len(req.Messages) == 0 {
		return req, fmt.Errorf("at least one message is required")
	}

	// Optional parameters
	if temp, ok := args["temperature"].(float64); ok {
		req.Temperature = &temp
	}
	if maxTokens, ok := args["max_tokens"].(float64); ok {
		v := int(maxTokens)
		req.MaxTokens = &v
	}
	if stream, ok := args["stream"].(bool); ok {
		req.Stream = stream
	}

	return req, nil
}

// matchGlob does simple glob matching (* = wildcard).
func matchGlob(pattern, s string) bool {
	if pattern == "*" || pattern == "**" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		return strings.HasPrefix(s, parts[0]) && strings.HasSuffix(s, parts[1])
	}
	return strings.Contains(s, strings.Trim(pattern, "*"))
}

// extractResponseContent returns the text content from the first choice.
func extractResponseContent(resp *openai.ChatCompletionResponse) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	msg := resp.Choices[0].Message
	// Content is json.RawMessage, try to unmarshal as string
	var s string
	if err := json.Unmarshal(msg.Content, &s); err == nil {
		return s
	}
	return string(msg.Content)
}

// extractFinishReason returns the finish reason from the first choice.
func extractFinishReason(resp *openai.ChatCompletionResponse) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].FinishReason
}
