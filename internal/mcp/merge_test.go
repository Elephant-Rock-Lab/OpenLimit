package mcp

import (
	"encoding/json"
	"testing"
)

func TestMergeToolsBasic(t *testing.T) {
	callerTools := json.RawMessage(`[
		{"type":"function","function":{"name":"my_tool","parameters":{"type":"object"}}}
	]`)
	mcpTools := []Tool{
		{Name: "weather.forecast", ServerName: "weather", Description: "Get forecast", InputSchema: map[string]any{"type": "object"}},
	}

	result, err := MergeTools(callerTools, nil, mcpTools, MergeConfig{ToolConflictStrategy: "skip"}, nil)
	if err != nil {
		t.Fatalf("MergeTools failed: %v", err)
	}

	if len(result.Tools) != 2 {
		t.Fatalf("expected 2 merged tools, got %d", len(result.Tools))
	}
	if len(result.MCPToolNames) != 1 {
		t.Fatalf("expected 1 MCP tool name, got %d", len(result.MCPToolNames))
	}
	if _, ok := result.MCPToolNames["weather.forecast"]; !ok {
		t.Fatal("expected weather.forecast in MCP tool names")
	}
}

func TestMergeToolsToolChoiceNone(t *testing.T) {
	callerTools := json.RawMessage(`[{"type":"function","function":{"name":"my_tool","parameters":{}}}]`)
	toolChoice := json.RawMessage(`"none"`)
	mcpTools := []Tool{
		{Name: "weather.forecast", ServerName: "weather", Description: "Get forecast", InputSchema: map[string]any{"type": "object"}},
	}

	result, err := MergeTools(callerTools, toolChoice, mcpTools, MergeConfig{}, nil)
	if err != nil {
		t.Fatalf("MergeTools failed: %v", err)
	}

	if result.Tools != nil {
		t.Fatal("expected nil tools when tool_choice is none")
	}
	if len(result.MCPToolNames) != 0 {
		t.Fatal("expected no MCP tool names when tool_choice is none")
	}
}

func TestMergeToolsDuplicateSkip(t *testing.T) {
	callerTools := json.RawMessage(`[
		{"type":"function","function":{"name":"weather.forecast","parameters":{}}}
	]`)
	mcpTools := []Tool{
		{Name: "weather.forecast", ServerName: "weather", Description: "Get forecast", InputSchema: map[string]any{"type": "object"}},
	}

	result, err := MergeTools(callerTools, nil, mcpTools, MergeConfig{ToolConflictStrategy: "skip"}, nil)
	if err != nil {
		t.Fatalf("MergeTools failed: %v", err)
	}

	// Should have only 1 tool (caller's), MCP tool dropped
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool after collision skip, got %d", len(result.Tools))
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
}

func TestMergeToolsDuplicateError(t *testing.T) {
	callerTools := json.RawMessage(`[
		{"type":"function","function":{"name":"weather.forecast","parameters":{}}}
	]`)
	mcpTools := []Tool{
		{Name: "weather.forecast", ServerName: "weather", Description: "Get forecast", InputSchema: map[string]any{"type": "object"}},
	}

	_, err := MergeTools(callerTools, nil, mcpTools, MergeConfig{ToolConflictStrategy: "error"}, nil)
	if err == nil {
		t.Fatal("expected error on duplicate with 'error' strategy")
	}
}

func TestMergeToolsNoCallerToolsNoAutoInject(t *testing.T) {
	mcpTools := []Tool{
		{Name: "weather.forecast", ServerName: "weather", Description: "Get forecast", InputSchema: map[string]any{"type": "object"}},
	}

	result, err := MergeTools(nil, nil, mcpTools, MergeConfig{AutoInjectTools: false}, nil)
	if err != nil {
		t.Fatalf("MergeTools failed: %v", err)
	}

	// No auto-inject + no caller tools = no injection
	if result.Tools != nil {
		t.Fatal("expected nil tools when no caller tools and auto_inject is off")
	}
}

func TestMergeToolsNoCallerToolsWithAutoInject(t *testing.T) {
	mcpTools := []Tool{
		{Name: "weather.forecast", ServerName: "weather", Description: "Get forecast", InputSchema: map[string]any{"type": "object"}},
	}

	result, err := MergeTools(nil, nil, mcpTools, MergeConfig{AutoInjectTools: true}, nil)
	if err != nil {
		t.Fatalf("MergeTools failed: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool with auto_inject, got %d", len(result.Tools))
	}
}

func TestMergeToolsEmptyAllowedToolsMeansAll(t *testing.T) {
	// This tests the auth layer (ToolAllowed), not merge directly.
	// But merge should receive the pre-filtered tools, so just test with all tools.
	mcpTools := []Tool{
		{Name: "weather.forecast", ServerName: "weather"},
		{Name: "github.create_issue", ServerName: "github"},
	}

	result, err := MergeTools(nil, nil, mcpTools, MergeConfig{AutoInjectTools: true}, nil)
	if err != nil {
		t.Fatalf("MergeTools failed: %v", err)
	}

	if len(result.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result.Tools))
	}
}

func TestIsToolChoiceNone(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{`"none"`, true},
		{`"required"`, false},
		{`"auto"`, false},
		{``, false},
		{`{"type":"function","function":{"name":"x"}}`, false},
	}
	for _, tt := range tests {
		var raw json.RawMessage
		if tt.input != "" {
			raw = json.RawMessage(tt.input)
		}
		got := isToolChoiceNone(raw)
		if got != tt.expected {
			t.Errorf("isToolChoiceNone(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestIsToolChoiceRequired(t *testing.T) {
	if !IsToolChoiceRequired(json.RawMessage(`"required"`)) {
		t.Error("expected required to be true")
	}
	if IsToolChoiceRequired(json.RawMessage(`"auto"`)) {
		t.Error("expected auto to be false")
	}
	if IsToolChoiceRequired(nil) {
		t.Error("expected nil to be false")
	}
}
