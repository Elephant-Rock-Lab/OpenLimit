package oidc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// MultiProvider validates tokens against multiple OIDC providers.
// It selects the correct provider based on the token's issuer claim.
type MultiProvider struct {
	providers map[string]*Provider // keyed by issuer URL
	logger    *slog.Logger
}

// MultiProviderConfig holds configuration for multiple OIDC providers.
type MultiProviderConfig struct {
	Providers []ProviderConfig
}

// NewMultiProvider creates a provider that validates against multiple OIDC issuers.
func NewMultiProvider(cfg MultiProviderConfig, logger *slog.Logger) (*MultiProvider, error) {
	providers := make(map[string]*Provider, len(cfg.Providers))
	for _, pc := range cfg.Providers {
		p, err := NewProvider(pc, logger)
		if err != nil {
			return nil, fmt.Errorf("OIDC provider %s: %w", pc.Issuer, err)
		}
		providers[normalizeIssuer(pc.Issuer)] = p
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("at least one OIDC provider is required")
	}
	return &MultiProvider{
		providers: providers,
		logger:    logger,
	}, nil
}

// ValidateToken validates a token against the matching provider.
// It extracts the issuer from the token's "iss" claim, finds the
// corresponding provider, and delegates validation.
func (mp *MultiProvider) ValidateToken(ctx context.Context, rawToken string, lookup UserLookupFunc) (*Context, error) {
	issuer, err := extractIssuer(rawToken)
	if err != nil {
		return nil, fmt.Errorf("extract issuer: %w", err)
	}

	normalized := normalizeIssuer(issuer)
	provider, ok := mp.providers[normalized]
	if !ok {
		return nil, fmt.Errorf("no OIDC provider configured for issuer %q", issuer)
	}

	return provider.ValidateToken(ctx, rawToken, lookup)
}

// Issuers returns the list of configured issuer URLs.
func (mp *MultiProvider) Issuers() []string {
	var issuers []string
	for iss := range mp.providers {
		issuers = append(issuers, iss)
	}
	return issuers
}

// Healthy reports whether all providers are healthy.
func (mp *MultiProvider) Healthy() bool {
	for _, p := range mp.providers {
		if !p.Healthy() {
			return false
		}
	}
	return true
}

// normalizeIssuer normalizes an issuer URL for matching.
func normalizeIssuer(issuer string) string {
	return strings.TrimRight(strings.ToLower(issuer), "/")
}

// extractIssuer extracts the "iss" claim from an unverified JWT.
// This is safe for routing — actual validation happens in the selected provider.
func extractIssuer(rawToken string) (string, error) {
	parts := strings.SplitN(rawToken, ".", 3)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid JWT format")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}

	var claims struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "", fmt.Errorf("parse claims: %w", err)
	}
	if claims.Issuer == "" {
		return "", fmt.Errorf("iss claim not found in token")
	}
	return claims.Issuer, nil
}

// Pool is a convenience type for thread-safe provider management.
// Supports both single-provider and multi-tenant modes.
type Pool struct {
	mu     sync.RWMutex
	single *Provider      // single provider mode
	multi  *MultiProvider // multi-tenant mode
}

// NewPool creates a new OIDC pool with either single or multi-tenant mode.
func NewPool(providers []ProviderConfig, logger *slog.Logger) (*Pool, error) {
	p := &Pool{}
	if len(providers) == 0 {
		return p, nil
	}
	if len(providers) == 1 {
		single, err := NewProvider(providers[0], logger)
		if err != nil {
			return nil, err
		}
		p.single = single
	} else {
		multi, err := NewMultiProvider(MultiProviderConfig{Providers: providers}, logger)
		if err != nil {
			return nil, err
		}
		p.multi = multi
	}
	return p, nil
}

// ValidateToken validates against the single or multi-tenant provider.
func (p *Pool) ValidateToken(ctx context.Context, rawToken string, lookup UserLookupFunc) (*Context, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.single != nil {
		return p.single.ValidateToken(ctx, rawToken, lookup)
	}
	if p.multi != nil {
		return p.multi.ValidateToken(ctx, rawToken, lookup)
	}
	return nil, fmt.Errorf("no OIDC provider configured")
}

// IsConfigured returns true if any provider is configured.
func (p *Pool) IsConfigured() bool {
	return p.single != nil || p.multi != nil
}
