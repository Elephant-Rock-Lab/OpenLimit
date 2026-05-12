package admin

import (
	"encoding/json"
	"net/http"
)

// MCPServerEntry holds per-server data for the MCP dashboard.
type MCPServerEntry struct {
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Tools     int      `json:"tools"`
	ToolList  []string `json:"tool_list"`
	LastError string   `json:"last_error,omitempty"`
}

// MCPServersHandler returns an http.Handler for GET /admin/mcp/servers.
// Follows the closure pattern from ToolsHandler.
func MCPServersHandler(serverStatus func() []MCPServerEntry) http.HandlerFunc {
	type response struct {
		Servers   []MCPServerEntry `json:"servers"`
		Total     int              `json:"total"`
		Connected int              `json:"connected"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAdminError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}

		entries := serverStatus()
		if entries == nil {
			entries = []MCPServerEntry{}
		}

		connected := 0
		for _, s := range entries {
			if s.Status == "connected" {
				connected++
			}
		}

		resp := response{
			Servers:   entries,
			Total:     len(entries),
			Connected: connected,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
