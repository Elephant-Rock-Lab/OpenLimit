package mcp

import (
	"fmt"
	"strings"
	"testing"

	"openlimit/internal/store"
)

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Weather Agent", "weather_agent"},
		{"GitHub Commits", "github_commits"},
		{"My-Cool-Tool", "my_cool_tool"},
		{"tool.with.dots", "toolwithdots"},
		{"UPPER CASE", "upper_case"},
		{"  spaces  ", "spaces"},
		{"a" + strings.Repeat("b", 100), "a" + strings.Repeat("b", 63)},
		{"", "unnamed_tool"},
		{"!!!", "unnamed_tool"},
		{"test_123", "test_123"},
		{"Agent V2 (Beta)", "agent_v2_beta"},
		{"日本語", "unnamed_tool"}, // non-ASCII stripped
	}

	for _, tt := range tests {
		got := sanitizeToolName(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeToolName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}

	// Verify max length
	longName := ""
	for i := 0; i < 100; i++ {
		longName += "x"
	}
	got := sanitizeToolName(longName)
	if len(got) > 64 {
		t.Errorf("expected max 64 chars, got %d", len(got))
	}
}

func TestMakeToolName(t *testing.T) {
	tests := []struct {
		name     string
		key      store.VirtualKey
		expected string
	}{
		{
			name:     "mcp_tool_name override",
			key:      store.VirtualKey{Name: "My Agent", MCPToolName: "custom_name"},
			expected: "custom_name",
		},
		{
			name:     "sanitized key name",
			key:      store.VirtualKey{Name: "Weather Agent"},
			expected: "weather_agent",
		},
		{
			name:     "empty name falls back to vk_<id>",
			key:      store.VirtualKey{ID: "abc12345xyz"},
			expected: "vk_abc12345",
		},
		{
			name:     "short id",
			key:      store.VirtualKey{ID: "ab"},
			expected: "vk_ab",
		},
		{
			name:     "mcp_tool_name takes priority over name",
			key:      store.VirtualKey{Name: "Old Name", MCPToolName: "New Name"},
			expected: "new_name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := makeToolName(&tt.key)
			if got != tt.expected {
				t.Errorf("makeToolName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDescribeKey(t *testing.T) {
	key := store.VirtualKey{
		Name:           "test-key",
		AllowedModels:  []string{"gpt-4o", "claude-3"},
		BudgetLimitUSD: 50.0,
		BudgetPeriod:   "monthly",
		RPMLimit:       100,
	}

	desc := describeKey(&key)
	if desc == "" {
		t.Fatal("expected non-empty description")
	}
	// Should contain key info
	if !contains(desc, "test-key") {
		t.Error("expected description to contain key name")
	}
	if !contains(desc, "gpt-4o") {
		t.Error("expected description to contain model")
	}
	if !contains(desc, "50.00") {
		t.Error("expected description to contain budget")
	}
}

func TestDescribeKeyNoRestrictions(t *testing.T) {
	key := store.VirtualKey{
		Name: "minimal",
	}
	desc := describeKey(&key)
	if !contains(desc, "all") {
		t.Error("expected 'all' models when no restrictions")
	}
}

func TestChatCompletionInputSchema(t *testing.T) {
	schema := chatCompletionInputSchema()

	if schema["type"] != "object" {
		t.Error("expected type 'object'")
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}

	requiredFields := []string{"model", "messages"}
	for _, f := range requiredFields {
		if _, ok := props[f]; !ok {
			t.Errorf("expected property %q", f)
		}
	}

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("expected required array")
	}
	if len(required) != 2 {
		t.Errorf("expected 2 required fields, got %d", len(required))
	}
}

func TestDeduplication(t *testing.T) {
	// Simulate what NewDBToolLister does with duplicate names
	keys := []store.VirtualKey{
		{Name: "Chat Agent", AllowMCPServer: true},
		{Name: "Chat Agent", AllowMCPServer: true},
		{Name: "Chat Agent", AllowMCPServer: true},
	}

	nameCount := make(map[string]int)
	var names []string

	for i := range keys {
		name := makeToolName(&keys[i])
		if count, exists := nameCount[name]; exists {
			nameCount[name] = count + 1
			name = fmt.Sprintf("%s_%d", name, count+1)
		} else {
			nameCount[name] = 1
		}
		names = append(names, name)
	}

	if names[0] != "chat_agent" {
		t.Errorf("first name = %q, want 'chat_agent'", names[0])
	}
	if names[1] != "chat_agent_2" {
		t.Errorf("second name = %q, want 'chat_agent_2'", names[1])
	}
	if names[2] != "chat_agent_3" {
		t.Errorf("third name = %q, want 'chat_agent_3'", names[2])
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
