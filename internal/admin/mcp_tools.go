package admin

import (
	"net/http"
)

// MCPToolsHandler returns an HTTP handler that lists MCP-exposed tools.
// Requires admin bearer token (same auth as all /admin/ endpoints).
func MCPToolsHandler(toolLister func() ([]map[string]any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAdminError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}

		tools, err := toolLister()
		if err != nil {
			writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to list MCP tools")
			return
		}

		if tools == nil {
			tools = []map[string]any{}
		}

		writeAdminJSON(w, http.StatusOK, map[string]any{
			"tools": tools,
		})
	}
}
