package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/store"
	"openlimit/internal/usage"
)

// Middleware authenticates requests using virtual API keys.
// When auth is disabled, all requests pass through.
type Middleware struct {
	cfg              config.AuthConfig
	db               *sql.DB
	cache            *KeyCache
	budgetFailClosed bool
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
		// If this looks like a DB error (not "key not found"), try grace-period cache
		if m.cache != nil && isDBError(err) {
			if authCtx, ok := m.cache.GetWithGrace(token, 5*time.Minute); ok {
				if !authCtx.IsExpired() {
					log.Printf("[WARN] auth: DB unavailable, serving key from grace-period cache (key=%s)", token[:min(8, len(token))]+"...")
					return authCtx, nil
				}
			}
			return nil, &authError{message: "service temporarily unavailable"}
		}
		return nil, &authError{message: "invalid virtual key"}
	}

	authCtx := FromVirtualKey(vk)

	if authCtx.IsExpired() {
		return nil, &authError{message: "virtual key has expired"}
	}

	// Budget enforcement (consolidated via usage.CheckBudget)
	if authCtx.BudgetLimitUSD > 0 {
		result, budgetErr := usage.CheckBudget(r.Context(), m.db, authCtx.VirtualKeyID, authCtx.BudgetPeriod, authCtx.BudgetLimitUSD, m.budgetFailClosed)
		if budgetErr != nil {
			return nil, &authError{message: budgetErr.Error()}
		}
		if !result.Allowed {
			return nil, &authError{message: fmt.Sprintf("budget exceeded: $%.2f %s limit reached", authCtx.BudgetLimitUSD, authCtx.BudgetPeriod)}
		}
	}

	// Cache the result
	if m.cache != nil {
		m.cache.Set(token, authCtx)
	}

	return authCtx, nil
}

// isDBError returns true if the error indicates a database connectivity problem
// rather than a simple "not found" result.
func isDBError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// The "no matching virtual key found" is a normal lookup miss, not a DB error.
	if msg == "no matching virtual key found" {
		return false
	}
	return true
}

// InvalidateCache removes a cached entry.
func (m *Middleware) InvalidateCache(token string) {
	if m.cache != nil {
		m.cache.Invalidate(token)
	}
}

// SetBudgetFailClosed configures whether budget checks reject on DB errors.
func (m *Middleware) SetBudgetFailClosed(failClosed bool) {
	m.budgetFailClosed = failClosed
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
	// Reject tokens shorter than "gw-" + 8 chars (minimum key body)
	if len(token) < 11 {
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
