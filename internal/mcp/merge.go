package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// MergeConfig controls how MCP tools are merged into a request.
type MergeConfig struct {
	AutoInjectTools      bool
	ToolConflictStrategy string // "skip" or "error"
}

// MergeResult contains the outcome of tool merging.
type MergeResult struct {
	Tools        []json.RawMessage // merged tool list (caller + MCP)
	MCPToolNames map[string]Tool   // prefixed name → MCP tool, for later lookup
	Warnings     []string
}

// MergeTools merges caller-defined tools with permitted MCP tools.
//
// Parameters:
//   - callerTools: the "tools" field from the request (may be nil/empty)
//   - toolChoice: the "tool_choice" field from the request (may be nil)
//   - mcpTools: all MCP tools the virtual key is allowed to use
//   - cfg: merge configuration
//   - logger: for warnings
func MergeTools(callerTools json.RawMessage, toolChoice json.RawMessage, mcpTools []Tool, cfg MergeConfig, logger *slog.Logger) (*MergeResult, error) {
	if logger == nil {
		logger = slog.Default()
	}

	result := &MergeResult{
		MCPToolNames: make(map[string]Tool),
	}

	// If tool_choice is "none", skip all tool injection
	if isToolChoiceNone(toolChoice) {
		result.Tools = nil
		return result, nil
	}

	// Parse caller tools
	callerToolNames := make(map[string]bool)
	var parsedCallerTools []json.RawMessage
	if len(callerTools) > 0 && string(callerTools) != "null" {
		if err := json.Unmarshal(callerTools, &parsedCallerTools); err != nil {
			return nil, fmt.Errorf("parse caller tools: %w", err)
		}
		for _, raw := range parsedCallerTools {
			var t struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if err := json.Unmarshal(raw, &t); err == nil && t.Function.Name != "" {
				callerToolNames[t.Function.Name] = true
			}
		}
	}

	// Convert MCP tools to OpenAI tool format and check for collisions
	var mcpToolJSONs []json.RawMessage
	for _, tool := range mcpTools {
		if callerToolNames[tool.Name] {
			// Collision!
			switch cfg.ToolConflictStrategy {
			case "error":
				return nil, fmt.Errorf("tool name collision: %q is defined by both caller and MCP server %q", tool.Name, tool.ServerName)
			case "skip", "":
				warning := fmt.Sprintf("MCP tool %q dropped due to name collision with caller tool (server: %s)", tool.Name, tool.ServerName)
				result.Warnings = append(result.Warnings, warning)
				logger.Warn("tool name collision, dropping MCP tool",
					"tool", tool.Name,
					"server", tool.ServerName,
				)
				continue
			}
		}

		openaiTool := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			},
		}
		toolJSON, err := json.Marshal(openaiTool)
		if err != nil {
			logger.Warn("failed to marshal MCP tool", "tool", tool.Name, "error", err)
			continue
		}
		mcpToolJSONs = append(mcpToolJSONs, toolJSON)
		result.MCPToolNames[tool.Name] = tool
	}

	// Merge: caller tools + MCP tools
	merged := make([]json.RawMessage, 0, len(parsedCallerTools)+len(mcpToolJSONs))
	merged = append(merged, parsedCallerTools...)
	merged = append(merged, mcpToolJSONs...)

	// If no caller tools and auto_inject is off and there are MCP tools,
	// don't inject (unless auto_inject is on)
	if len(parsedCallerTools) == 0 && !cfg.AutoInjectTools && len(mcpToolJSONs) > 0 {
		result.Tools = nil
		result.MCPToolNames = make(map[string]Tool) // clear so caller knows nothing was injected
		return result, nil
	}

	result.Tools = merged
	return result, nil
}

// isToolChoiceNone checks if tool_choice is explicitly set to "none".
func isToolChoiceNone(toolChoice json.RawMessage) bool {
	if len(toolChoice) == 0 {
		return false
	}
	var val string
	if err := json.Unmarshal(toolChoice, &val); err == nil && val == "none" {
		return true
	}
	return false
}

// IsToolChoiceRequired checks if tool_choice is set to "required".
func IsToolChoiceRequired(toolChoice json.RawMessage) bool {
	if len(toolChoice) == 0 {
		return false
	}
	var val string
	if err := json.Unmarshal(toolChoice, &val); err == nil && val == "required" {
		return true
	}
	return false
}
