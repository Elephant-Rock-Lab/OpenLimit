// Package plugins defines the extension interfaces for OpenLimit gateway plugins.
//
// Plugins are registered at init time via [Register] and looked up by name.
// Three plugin types are supported:
//   - GuardrailPlugin: custom guardrail stages (input/output processing)
//   - MiddlewarePlugin: HTTP middleware (request/response modification)
//   - ProviderPlugin: custom provider adapters
package plugins

import "net/http"

// Plugin is the base interface all plugins must implement.
type Plugin interface {
	// Name returns the unique plugin identifier.
	Name() string
	// Type returns the plugin type ("guardrail", "middleware", "provider").
	Type() string
	// Init initializes the plugin with its configuration.
	Init(config map[string]any) error
}

// GuardrailPlugin extends Plugin with guardrail-specific methods.
// GuardrailPlugins are loaded into the guardrail pipeline as custom stages.
type GuardrailPlugin interface {
	Plugin
	// ProcessInput processes an input message before it's sent to the provider.
	// Returns the (possibly modified) message, or an error to block the request.
	ProcessInput(ctx GuardrailContext) (GuardrailContext, error)
	// ProcessOutput processes an output message after it's received from the provider.
	// Returns the (possibly modified) message, or an error to block the response.
	ProcessOutput(ctx GuardrailContext) (GuardrailContext, error)
}

// MiddlewarePlugin extends Plugin with HTTP middleware.
// MiddlewarePlugins wrap HTTP handlers for request/response modification.
type MiddlewarePlugin interface {
	Plugin
	// Middleware returns an HTTP middleware function.
	Middleware() func(http.Handler) http.Handler
}

// ProviderPlugin extends Plugin with provider adapter creation.
type ProviderPlugin interface {
	Plugin
	// CreateAdapter creates a new provider adapter instance.
	CreateAdapter(config map[string]any) (ProviderAdapter, error)
}

// ProviderAdapter is the interface for custom provider adapters.
type ProviderAdapter interface {
	// Name returns the provider name.
	Name() string
}

// GuardrailContext carries data through the guardrail pipeline.
type GuardrailContext struct {
	// Message is the text content being processed.
	Message string
	// Metadata carries arbitrary key-value data.
	Metadata map[string]any
	// Blocked indicates the request should be blocked.
	Blocked bool
	// BlockReason describes why the request was blocked.
	BlockReason string
	// Modified indicates the content was changed.
	Modified bool
}

// NewGuardrailContext creates a GuardrailContext with the given message.
func NewGuardrailContext(message string) GuardrailContext {
	return GuardrailContext{
		Message:  message,
		Metadata: map[string]any{},
	}
}
