package auth

import (
	"context"
	"path"
	"strings"
	"time"

	"openlimit/internal/store"
)

type contextKey string

const authKey contextKey = "auth"

// Context holds the authenticated identity extracted from a virtual key.
type Context struct {
	ProjectID        string
	VirtualKeyID     string
	KeyPrefix        string
	Name             string
	AllowedModels    []string
	AllowedProviders []string
	AllowedTools     []string
	RPMLimit         int
	TPMLimit         int
	BudgetLimitUSD   float64
	BudgetPeriod     string
	ExpiresAt        *time.Time
}

// WithContext stores the auth context in the request context.
func WithContext(ctx context.Context, ac *Context) context.Context {
	return context.WithValue(ctx, authKey, ac)
}

// FromContext retrieves the auth context from the request context.
func FromContext(ctx context.Context) *Context {
	if ac, ok := ctx.Value(authKey).(*Context); ok {
		return ac
	}
	return nil
}

// IsAuthenticated returns true if the context has auth info.
func IsAuthenticated(ctx context.Context) bool {
	return FromContext(ctx) != nil
}

// ModelAllowed checks if the given model is allowed by the virtual key.
// An empty allowed list means all models are permitted.
func (ac *Context) ModelAllowed(model string) bool {
	if len(ac.AllowedModels) == 0 {
		return true
	}
	for _, m := range ac.AllowedModels {
		if strings.EqualFold(m, model) {
			return true
		}
	}
	return false
}

// ProviderAllowed checks if the given provider is allowed by the virtual key.
// An empty allowed list means all providers are permitted.
func (ac *Context) ProviderAllowed(provider string) bool {
	if len(ac.AllowedProviders) == 0 {
		return true
	}
	for _, p := range ac.AllowedProviders {
		if strings.EqualFold(p, provider) {
			return true
		}
	}
	return false
}

// IsExpired checks if the key has expired.
func (ac *Context) IsExpired() bool {
	if ac.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*ac.ExpiresAt)
}

// ToolAllowed checks if the given tool name matches any allowed_tools glob pattern.
// An empty allowed list means all tools are permitted.
func (ac *Context) ToolAllowed(toolName string) bool {
	if len(ac.AllowedTools) == 0 {
		return true
	}
	for _, pattern := range ac.AllowedTools {
		if match, _ := path.Match(pattern, toolName); match {
			return true
		}
	}
	return false
}

// FromVirtualKey converts a store.VirtualKey to an auth.Context.
func FromVirtualKey(vk *store.VirtualKey) *Context {
	return &Context{
		ProjectID:        vk.ProjectID,
		VirtualKeyID:     vk.ID,
		KeyPrefix:        vk.KeyPrefix,
		Name:             vk.Name,
		AllowedModels:    vk.AllowedModels,
		AllowedProviders: vk.AllowedProviders,
		AllowedTools:     vk.AllowedTools,
		RPMLimit:         vk.RPMLimit,
		TPMLimit:         vk.TPMLimit,
		BudgetLimitUSD:   vk.BudgetLimitUSD,
		BudgetPeriod:     vk.BudgetPeriod,
		ExpiresAt:        vk.ExpiresAt,
	}
}
