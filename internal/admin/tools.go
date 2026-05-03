package admin

import (
	"encoding/json"
	"net/http"
)

// ToolsHandler returns an http.Handler that lists MCP tools and server status.
// It requires admin bearer token authentication (applied by the wrapping middleware).
func ToolsHandler(serverStatus func() []ToolServerInfo) http.HandlerFunc {
	type toolInfo struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Server      string         `json:"server"`
		InputSchema map[string]any `json:"inputSchema,omitempty"`
	}

	type toolsResponse struct {
		Tools   []toolInfo       `json:"tools"`
		Servers []ToolServerInfo `json:"servers"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAdminError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}

		serverFilter := r.URL.Query().Get("server")

		statuses := serverStatus()

		var tools []toolInfo
		var servers []ToolServerInfo

		for _, s := range statuses {
			if serverFilter != "" && s.Name != serverFilter {
				continue
			}
			servers = append(servers, s)
			for _, t := range s.Tools {
				tools = append(tools, toolInfo{
					Name:        t.Name,
					Description: t.Description,
					Server:      s.Name,
					InputSchema: t.InputSchema,
				})
			}
		}

		resp := toolsResponse{
			Tools:   tools,
			Servers: servers,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// ToolServerInfo is a simplified server status for the admin endpoint.
type ToolServerInfo struct {
	Name      string       `json:"name"`
	Status    string       `json:"status"`
	ToolCount int          `json:"tools"`
	Tools     []ToolDetail `json:"-"`
}

// ToolDetail is a simplified tool for the admin endpoint.
type ToolDetail struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}
