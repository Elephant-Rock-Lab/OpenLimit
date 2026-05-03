package mcp

import "sync"

// Registry aggregates tools from all connected MCP servers and provides
// lookup by name or server.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool // keyed by prefixed name (e.g., "weather.get_forecast")
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// AllTools returns all tools from all servers.
func (r *Registry) AllTools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

// ToolsByServer returns all tools from a specific server.
func (r *Registry) ToolsByServer(serverName string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, 0)
	for _, t := range r.tools {
		if t.ServerName == serverName {
			result = append(result, t)
		}
	}
	return result
}

// ToolByName looks up a tool by its prefixed name.
func (r *Registry) ToolByName(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// ReplaceServerTools replaces all tools for a given server.
// This is called after a successful tool list refresh.
func (r *Registry) ReplaceServerTools(serverName string, tools []Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove old tools for this server
	for k, t := range r.tools {
		if t.ServerName == serverName {
			delete(r.tools, k)
		}
	}

	// Add new tools
	for _, t := range tools {
		r.tools[t.Name] = t
	}
}

// RemoveServerTools removes all tools for a disconnected server.
func (r *Registry) RemoveServerTools(serverName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, t := range r.tools {
		if t.ServerName == serverName {
			delete(r.tools, k)
		}
	}
}

// ToolCount returns the total number of tools in the registry.
func (r *Registry) ToolCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
