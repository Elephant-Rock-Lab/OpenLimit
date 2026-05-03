package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"fmt"
	"openlimit/internal/config"
	"openlimit/internal/store"
	"openlimit/internal/usage"
)

// Middleware authenticates requests using virtual API keys.
// When auth is disabled, all requests pass through.
type Middleware struct {
	cfg   config.AuthConfig
	db    *sql.DB
	cache *KeyCache
}

// NewMiddleware creates a new auth middleware.
func NewMiddleware(cfg config.AuthConfig, db *sql.DB) *Middleware {
	m := &Middleware{
		cfg: cfg,
		db:  db,
	}
	if cfg.Enabled && db != nil {
		m.cache = NewKeyCache(cfg.KeyCacheSize, ttlFromSec(cfg.KeyCacheTTLSec))
	}
	return m
}

// Wrap returns an http.Handler that authenticates requests before passing to next.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.cfg.Enabled {
		return next
	}
	if m.db == nil {
		// Auth is enabled but no database — reject everything
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeAuthError(w, "authentication is enabled but database is not configured")
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearerToken(r)
		if token == "" {
			writeAuthError(w, "missing or invalid authorization header")
			return
		}

		authCtx, err := m.authenticate(r, token)
		if err != nil {
			writeAuthError(w, err.Error())
			return
		}

		// Validate model access
		model := r.URL.Query().Get("model")
		if model != "" && !authCtx.ModelAllowed(model) {
			writeForbidden(w, "model not allowed for this key")
			return
		}

		ctx := WithContext(r.Context(), authCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Middleware) authenticate(r *http.Request, token string) (*Context, error) {
	// Check in-memory cache (keyed by the raw token — acceptable for in-process cache)
	if m.cache != nil {
		if authCtx, ok := m.cache.Get(token); ok {
			if authCtx.IsExpired() {
				m.cache.Invalidate(token)
				return nil, &authError{message: "virtual key has expired"}
			}
			return authCtx, nil
		}
	}

	// DB lookup via bcrypt comparison
	vk, err := store.LookupVirtualKeyByToken(r.Context(), m.db, token)
	if err != nil {
		return nil, &authError{message: "invalid virtual key"}
	}

	authCtx := FromVirtualKey(vk)

	if authCtx.IsExpired() {
		return nil, &authError{message: "virtual key has expired"}
	}

	// Budget enforcement
	if authCtx.BudgetLimitUSD > 0 {
		spend, err := usage.GetSpendForCurrentPeriod(r.Context(), m.db, authCtx.VirtualKeyID, authCtx.BudgetPeriod)
		if err == nil && spend >= authCtx.BudgetLimitUSD {
			return nil, &authError{message: fmt.Sprintf("budget exceeded: $%.2f %s limit reached", authCtx.BudgetLimitUSD, authCtx.BudgetPeriod)}
		}
	}

	// Cache the result
	if m.cache != nil {
		m.cache.Set(token, authCtx)
	}

	return authCtx, nil
}

// InvalidateCache removes a cached entry.
func (m *Middleware) InvalidateCache(token string) {
	if m.cache != nil {
		m.cache.Invalidate(token)
	}
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	if !strings.HasPrefix(token, "gw-") {
		return ""
	}
	return token
}

func writeAuthError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": message, "type": "auth_error"},
	})
}

func writeForbidden(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": message, "type": "auth_error"},
	})
}

type authError struct {
	message string
}

func (e *authError) Error() string {
	return e.message
}

func ttlFromSec(sec int) time.Duration {
	if sec <= 0 {
		return 60 * time.Second
	}
	return time.Duration(sec) * time.Second
}
