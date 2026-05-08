package plugins

import (
	"fmt"
	"sync"
)

// registry holds all registered plugins, keyed by name.
var (
	mu      sync.RWMutex
	plugins = map[string]Plugin{}
)

// Register adds a plugin to the global registry.
// Panics if a plugin with the same name is already registered.
func Register(p Plugin) {
	mu.Lock()
	defer mu.Unlock()
	name := p.Name()
	if _, exists := plugins[name]; exists {
		panic(fmt.Sprintf("plugin %q already registered", name))
	}
	plugins[name] = p
}

// Lookup returns a plugin by name. Returns nil if not found.
func Lookup(name string) Plugin {
	mu.RLock()
	defer mu.RUnlock()
	return plugins[name]
}

// LookupGuardrail returns a GuardrailPlugin by name.
func LookupGuardrail(name string) (GuardrailPlugin, bool) {
	p := Lookup(name)
	if p == nil {
		return nil, false
	}
	gp, ok := p.(GuardrailPlugin)
	return gp, ok
}

// LookupMiddleware returns a MiddlewarePlugin by name.
func LookupMiddleware(name string) (MiddlewarePlugin, bool) {
	p := Lookup(name)
	if p == nil {
		return nil, false
	}
	mp, ok := p.(MiddlewarePlugin)
	return mp, ok
}

// List returns all registered plugins.
func List() []Plugin {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]Plugin, 0, len(plugins))
	for _, p := range plugins {
		result = append(result, p)
	}
	return result
}

// ListByType returns all plugins of a given type.
func ListByType(pluginType string) []Plugin {
	mu.RLock()
	defer mu.RUnlock()
	var result []Plugin
	for _, p := range plugins {
		if p.Type() == pluginType {
			result = append(result, p)
		}
	}
	return result
}

// Reset clears the registry (for testing).
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	plugins = map[string]Plugin{}
}
