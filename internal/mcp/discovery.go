package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"openlimit/internal/store"
)

// NewDBToolLister returns a ToolLister function that queries virtual keys
// from the database and converts them to MCP tool definitions.
func NewDBToolLister(db *sql.DB, logger *slog.Logger) ToolLister {
	if logger == nil {
		logger = slog.Default()
	}
	l := logger.With("component", "mcp_discovery")

	return func() ([]ToolDefinition, error) {
		ctx := context.Background()
		keys, err := store.ListVirtualKeys(ctx, db, "")
		if err != nil {
			l.Error("failed to list keys for tool discovery", "error", err)
			return nil, fmt.Errorf("list keys: %w", err)
		}

		var tools []ToolDefinition
		nameCount := make(map[string]int)

		for i := range keys {
			key := &keys[i]
			if !key.AllowMCPServer || key.RevokedAt != nil {
				continue
			}

			name := makeToolName(key)

			// Deduplicate: if name already seen, append counter
			if count, exists := nameCount[name]; exists {
				nameCount[name] = count + 1
				name = fmt.Sprintf("%s_%d", name, count+1)
			} else {
				nameCount[name] = 1
			}

			desc := describeKey(key)

			tools = append(tools, ToolDefinition{
				Name:        name,
				Description: desc,
				InputSchema: chatCompletionInputSchema(),
			})
		}

		l.Debug("tool discovery completed", "tool_count", len(tools))
		return tools, nil
	}
}

// makeToolName determines the MCP tool name for a virtual key.
// Priority: mcp_tool_name (if set) > sanitized key name > vk_<id[:8]>.
func makeToolName(key *store.VirtualKey) string {
	if key.MCPToolName != "" {
		return sanitizeToolName(key.MCPToolName)
	}
	if key.Name != "" {
		return sanitizeToolName(key.Name)
	}
	shortID := key.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return "vk_" + shortID
}

var nonAlnumUnderscore = regexp.MustCompile(`[^a-z0-9_]`)

// sanitizeToolName converts a string to a valid MCP tool name:
// lowercase, spaces/hyphens become underscores, strip invalid chars, max 64 chars.
func sanitizeToolName(name string) string {
	s := strings.TrimSpace(strings.ToLower(name))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	s = nonAlnumUnderscore.ReplaceAllString(s, "")
	s = strings.Trim(s, "_")
	if len(s) > 64 {
		s = s[:64]
		s = strings.TrimRight(s, "_")
	}
	if s == "" {
		return "unnamed_tool"
	}
	return s
}

// describeKey generates a human-readable description for a virtual key tool.
func describeKey(key *store.VirtualKey) string {
	models := "all"
	if len(key.AllowedModels) > 0 {
		models = strings.Join(key.AllowedModels, ", ")
	}

	desc := fmt.Sprintf("Virtual key '%s' — models: %s", key.Name, models)

	if key.BudgetLimitUSD > 0 {
		desc += fmt.Sprintf(", budget: $%.2f/%s", key.BudgetLimitUSD, key.BudgetPeriod)
	}
	if key.RPMLimit > 0 {
		desc += fmt.Sprintf(", RPM: %d", key.RPMLimit)
	}
	if key.TPMLimit > 0 {
		desc += fmt.Sprintf(", TPM: %d", key.TPMLimit)
	}

	return desc
}

// chatCompletionInputSchema returns the JSON Schema for a chat completion request.
// This is the input schema for all virtual key tools.
func chatCompletionInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"model": map[string]any{
				"type":        "string",
				"description": "Model name (must be allowed for this virtual key)",
			},
			"messages": map[string]any{
				"type":        "array",
				"description": "Chat messages in OpenAI format",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"role":    map[string]any{"type": "string", "enum": []string{"system", "user", "assistant", "tool"}},
						"content": map[string]any{"type": "string"},
					},
					"required": []string{"role", "content"},
				},
			},
			"temperature": map[string]any{
				"type":        "number",
				"description": "Sampling temperature (0-2)",
			},
			"max_tokens": map[string]any{
				"type":        "integer",
				"description": "Maximum tokens to generate",
			},
			"stream": map[string]any{
				"type":        "boolean",
				"description": "Stream response (buffered to single result in MCP tool)",
				"default":     false,
			},
		},
		"required": []string{"model", "messages"},
	}
}
