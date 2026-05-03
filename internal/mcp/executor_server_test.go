package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"openlimit/internal/schema/openai"
)

// mockKeyResolver is a test double for KeyResolver.
type mockKeyResolver struct {
	keys map[string]*ResolvedKey
}

func (m *mockKeyResolver) ResolveToolName(_ context.Context, toolName string) (*ResolvedKey, error) {
	if k, ok := m.keys[toolName]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("tool %q not found", toolName)
}

// mockChatHandler is a test double for ChatHandler.
type mockChatHandler struct {
	response *openai.ChatCompletionResponse
	err      error
	lastReq  *openai.ChatCompletionRequest
}

func (m *mockChatHandler) ExecuteForMCP(_ context.Context, req openai.ChatCompletionRequest, _ any) (*openai.ChatCompletionResponse, error) {
	m.lastReq = &req
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func TestServerChatExecutor_Execute(t *testing.T) {
	resolver := &mockKeyResolver{
		keys: map[string]*ResolvedKey{
			"weather_agent": {
				KeyID:         "key-123",
				KeyName:       "Weather Agent",
				AllowedModels: []string{"gpt-4o", "gpt-4*"},
			},
			"restricted": {
				KeyID:         "key-456",
				KeyName:       "Restricted",
				AllowedModels: []string{"claude-3"},
			},
		},
	}

	handler := &mockChatHandler{
		response: &openai.ChatCompletionResponse{
			ID:    "chatcmpl-test",
			Model: "gpt-4o",
			Choices: []openai.Choice{
				{
					Index:        0,
					FinishReason: "stop",
					Message: openai.ChatMessage{
						Role:    "assistant",
						Content: json.RawMessage(`"The weather is sunny"`),
					},
				},
			},
			Usage: &openai.Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		},
	}

	exec := NewServerChatExecutor(resolver, handler, nil)

	tests := []struct {
		name        string
		toolName    string
		args        map[string]any
		wantErr     bool
		wantContent string
	}{
		{
			name:     "successful execution",
			toolName: "weather_agent",
			args: map[string]any{
				"model": "gpt-4o",
				"messages": []any{
					map[string]any{"role": "user", "content": "What's the weather?"},
				},
			},
			wantErr:     false,
			wantContent: "The weather is sunny",
		},
		{
			name:     "model not allowed",
			toolName: "restricted",
			args: map[string]any{
				"model": "gpt-4o",
				"messages": []any{
					map[string]any{"role": "user", "content": "test"},
				},
			},
			wantErr: true,
		},
		{
			name:     "tool not found",
			toolName: "nonexistent",
			args:     map[string]any{"model": "gpt-4o"},
			wantErr:  true,
		},
		{
			name:     "missing model",
			toolName: "weather_agent",
			args: map[string]any{
				"messages": []any{
					map[string]any{"role": "user", "content": "test"},
				},
			},
			wantErr: true,
		},
		{
			name:     "missing messages",
			toolName: "weather_agent",
			args: map[string]any{
				"model": "gpt-4o",
			},
			wantErr: true,
		},
		{
			name:     "glob model match",
			toolName: "weather_agent",
			args: map[string]any{
				"model": "gpt-4o-mini",
				"messages": []any{
					map[string]any{"role": "user", "content": "test"},
				},
			},
			wantErr:     false,
			wantContent: "The weather is sunny",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := exec.Execute(context.Background(), tt.toolName, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Content != tt.wantContent {
				t.Errorf("content = %q, want %q", result.Content, tt.wantContent)
			}
			if result.Model != "gpt-4o" {
				t.Errorf("model = %q, want 'gpt-4o'", result.Model)
			}
			if result.Usage == nil {
				t.Error("expected usage")
			} else if result.Usage.TotalTokens != 15 {
				t.Errorf("total tokens = %d, want 15", result.Usage.TotalTokens)
			}
		})
	}
}

func TestServerChatExecutor_OptionalParams(t *testing.T) {
	resolver := &mockKeyResolver{
		keys: map[string]*ResolvedKey{
			"test_tool": {KeyID: "k1", KeyName: "test"},
		},
	}

	handler := &mockChatHandler{
		response: &openai.ChatCompletionResponse{
			Model: "gpt-4o",
			Choices: []openai.Choice{
				{
					Message: openai.ChatMessage{
						Role:    "assistant",
						Content: json.RawMessage(`"ok"`),
					},
				},
			},
		},
	}

	exec := NewServerChatExecutor(resolver, handler, nil)

	_, err := exec.Execute(context.Background(), "test_tool", map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"temperature": 0.7,
		"max_tokens":  100.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := handler.lastReq
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Error("temperature not set correctly")
	}
	if req.MaxTokens == nil || *req.MaxTokens != 100 {
		t.Error("max_tokens not set correctly")
	}
}

func TestDBKeyResolver(t *testing.T) {
	// DBKeyResolver requires a real DB, so we test via mockKeyResolver instead.
	// The mock tests cover the resolver interface contract.
	// Integration tests with a real DB cover DBKeyResolver.
	t.Skip("DBKeyResolver tested via integration tests")
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		s       string
		want    bool
	}{
		{"*", "anything", true},
		{"**", "anything", true},
		{"gpt-4o", "gpt-4o", true},
		{"gpt-4o", "gpt-4o-mini", false},
		{"gpt-4*", "gpt-4o", true},
		{"gpt-4*", "gpt-4o-mini", true},
		{"claude*", "claude-3-opus", true},
		{"*-mini", "gpt-4o-mini", true},
		{"*-mini", "gpt-4o", false},
	}

	for _, tt := range tests {
		got := matchGlob(tt.pattern, tt.s)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
		}
	}
}

func TestExtractResponseContent(t *testing.T) {
	tests := []struct {
		name string
		resp *openai.ChatCompletionResponse
		want string
	}{
		{
			name: "string content",
			resp: &openai.ChatCompletionResponse{
				Choices: []openai.Choice{
					{
						Message: openai.ChatMessage{
							Content: json.RawMessage(`"hello"`),
						},
					},
				},
			},
			want: "hello",
		},
		{
			name: "no choices",
			resp: &openai.ChatCompletionResponse{},
			want: "",
		},
		{
			name: "raw JSON content",
			resp: &openai.ChatCompletionResponse{
				Choices: []openai.Choice{
					{
						Message: openai.ChatMessage{
							Content: json.RawMessage(`{"text": "hello"}`),
						},
					},
				},
			},
			want: `{"text": "hello"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractResponseContent(tt.resp)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildRequest(t *testing.T) {
	exec := NewServerChatExecutor(nil, nil, nil)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
		checkFn func(t *testing.T, req openai.ChatCompletionRequest)
	}{
		{
			name: "valid request",
			args: map[string]any{
				"model": "gpt-4o",
				"messages": []any{
					map[string]any{"role": "user", "content": "hello"},
				},
			},
			wantErr: false,
			checkFn: func(t *testing.T, req openai.ChatCompletionRequest) {
				if req.Model != "gpt-4o" {
					t.Errorf("model = %q", req.Model)
				}
				if len(req.Messages) != 1 {
					t.Fatalf("messages count = %d", len(req.Messages))
				}
				if req.Messages[0].Role != "user" {
					t.Errorf("role = %q", req.Messages[0].Role)
				}
			},
		},
		{
			name:    "missing model",
			args:    map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}},
			wantErr: true,
		},
		{
			name:    "missing messages",
			args:    map[string]any{"model": "gpt-4o"},
			wantErr: true,
		},
		{
			name:    "empty messages array",
			args:    map[string]any{"model": "gpt-4o", "messages": []any{}},
			wantErr: true,
		},
		{
			name:    "messages not array",
			args:    map[string]any{"model": "gpt-4o", "messages": "invalid"},
			wantErr: true,
		},
		{
			name:    "message missing role",
			args:    map[string]any{"model": "gpt-4o", "messages": []any{map[string]any{"content": "hi"}}},
			wantErr: true,
		},
		{
			name: "multi-message conversation",
			args: map[string]any{
				"model": "gpt-4o",
				"messages": []any{
					map[string]any{"role": "system", "content": "You are helpful."},
					map[string]any{"role": "user", "content": "Hello"},
				},
			},
			wantErr: false,
			checkFn: func(t *testing.T, req openai.ChatCompletionRequest) {
				if len(req.Messages) != 2 {
					t.Errorf("expected 2 messages, got %d", len(req.Messages))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := exec.buildRequest(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, req)
			}
		})
	}
}
