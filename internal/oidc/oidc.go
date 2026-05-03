package oidc

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"

	"openlimit/internal/store"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Context holds the OIDC-authenticated identity.
type Context struct {
	Subject string // OIDC sub claim — stable unique identifier
	Email   string // email claim (may be empty for some IdPs)
	Name    string // name claim (may be empty)
	Role    string // resolved from admin_users table
}

type contextKey struct{}

// FromContext retrieves the OIDC context from the request context.
func FromContext(ctx context.Context) *Context {
	if v := ctx.Value(contextKey{}); v != nil {
		if oc, ok := v.(*Context); ok {
			return oc
		}
	}
	return nil
}

// WithContext stores the OIDC context in the request context.
func WithContext(ctx context.Context, oc *Context) context.Context {
	return context.WithValue(ctx, contextKey{}, oc)
}

// UserLookupFunc looks up a user by subject and email, returning the role.
type UserLookupFunc func(ctx context.Context, subject, email string) (role string, err error)

// Provider handles OIDC token validation and user lookup.
type Provider struct {
	verifier    *oidc.IDTokenVerifier
	provider    *oidc.Provider
	issuer      string
	defaultRole string
	logger      *slog.Logger

	mu      sync.RWMutex
	healthy bool
}

// ProviderConfig holds OIDC configuration.
type ProviderConfig struct {
	Issuer       string
	Audience     string
	DefaultRole  string
	JWKSCacheTTL int // seconds, default 3600
}

// NewProvider creates a new OIDC provider with JWKS discovery.
func NewProvider(cfg ProviderConfig, logger *slog.Logger) (*Provider, error) {
	if cfg.DefaultRole == "" {
		cfg.DefaultRole = store.RoleViewer
	}

	ctx := context.Background()

	oidcProvider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}

	verifier := oidcProvider.Verifier(&oidc.Config{
		ClientID: cfg.Audience,
	})

	p := &Provider{
		verifier:    verifier,
		provider:    oidcProvider,
		issuer:      cfg.Issuer,
		defaultRole: cfg.DefaultRole,
		logger:      logger,
		healthy:     true,
	}

	return p, nil
}

// ValidateToken validates a JWT access token and returns the OIDC context.
// It extracts claims and resolves the user's role via the lookup function.
func (p *Provider) ValidateToken(ctx context.Context, rawToken string, lookup UserLookupFunc) (*Context, error) {
	token, err := p.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, err
	}

	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		Name          string `json:"name"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := token.Claims(&claims); err != nil {
		return nil, err
	}

	// Resolve role via user lookup
	role := p.defaultRole
	if lookup != nil {
		resolved, err := lookup(ctx, claims.Sub, claims.Email)
		if err == nil && resolved != "" {
			role = resolved
		}
	}

	oc := &Context{
		Subject: claims.Sub,
		Email:   claims.Email,
		Name:    claims.Name,
		Role:    role,
	}

	return oc, nil
}

// Healthy returns true if the OIDC provider was successfully discovered.
func (p *Provider) Healthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.healthy
}

// Issuer returns the configured issuer URL.
func (p *Provider) Issuer() string {
	return p.issuer
}

// DBLookup creates a UserLookupFunc that queries the admin_users table.
// Lookup order: by subject → by email → returns defaultRole (no auto-provision in lookup).
// Auto-provisioning is handled by the caller (server wiring).
func DBLookup(db *sql.DB, defaultRole string) UserLookupFunc {
	return func(ctx context.Context, subject, email string) (string, error) {
		// Try subject first
		if subject != "" {
			u, err := store.LookupUserBySubject(ctx, db, subject)
			if err == nil && u != nil {
				return u.Role, nil
			}
		}

		// Try email
		if email != "" {
			u, err := store.LookupUserByEmail(ctx, db, email)
			if err == nil && u != nil {
				return u.Role, nil
			}
		}

		// Not found — return default role (caller decides whether to auto-provision)
		return defaultRole, nil
	}
}

// AutoProvision creates a user in the admin_users table with the default role.
// Returns the assigned role.
func AutoProvision(ctx context.Context, db *sql.DB, subject, email, defaultRole string) (string, error) {
	_, err := store.CreateUser(ctx, db, subject, email, defaultRole)
	if err != nil {
		return defaultRole, err
	}
	return defaultRole, nil
}
