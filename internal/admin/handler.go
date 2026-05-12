package admin

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"time"

	"openlimit/internal/metrics"

	"golang.org/x/crypto/bcrypt"

	"openlimit/internal/audit"
	"openlimit/internal/config"
	"openlimit/internal/providers"
	"openlimit/internal/requestid"
	"openlimit/internal/store"

	oidcPkg "openlimit/internal/oidc"
	openaischema "openlimit/internal/schema/openai"
)

// Handler serves admin API endpoints for projects, virtual keys, and usage.
type Handler struct {
	db            *sql.DB
	cfg           config.Config
	audit         *audit.Logger
	metrics       metricsRecorder
	rbacEnabled   bool
	OnKeysChanged func() // called after key create/revoke if MCP server is enabled
}

// metricsRecorder records RBAC and other admin metrics.
type metricsRecorder interface {
	RecordRBACCheck(role, action, result string)
	GetGuardrailStats() metrics.GuardrailStatsSnapshot
}

// NewHandler creates a new admin handler.
func NewHandler(db *sql.DB, cfg config.Config, auditLog *audit.Logger, mc metricsRecorder) *Handler {
	return &Handler{db: db, cfg: cfg, audit: auditLog, metrics: mc, rbacEnabled: cfg.Admin.RBACEnabled}
}

// RegisterRoutes registers admin routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /admin/dashboard", DashboardHandler())
	mux.HandleFunc("POST /admin/prompts", h.createPrompt)
	mux.HandleFunc("GET /admin/prompts", h.listPrompts)
	mux.HandleFunc("PUT /admin/prompts/{id}", h.updatePrompt)
	mux.HandleFunc("DELETE /admin/prompts/{id}", h.deletePrompt)
	mux.HandleFunc("POST /admin/projects", h.createProject)
	mux.HandleFunc("GET /admin/projects", h.listProjects)
	mux.HandleFunc("DELETE /admin/projects/{id}", h.deleteProject)
	mux.HandleFunc("POST /admin/keys", h.createKey)
	mux.HandleFunc("GET /admin/keys", h.listKeys)
	mux.HandleFunc("PUT /admin/keys/{id}", h.updateKey)
	mux.HandleFunc("PATCH /admin/keys/{id}", h.patchKey)
	mux.HandleFunc("DELETE /admin/keys/{id}", h.revokeKey)
	mux.HandleFunc("POST /admin/keys/{id}/rotate", h.rotateKey)
	mux.HandleFunc("GET /admin/usage", h.usage)
	mux.HandleFunc("GET /admin/usage/summary", h.usageSummary)
	mux.HandleFunc("GET /admin/usage/spend", h.spend)
	mux.HandleFunc("GET /admin/audit", h.queryAuditLogs)
	mux.HandleFunc("POST /admin/quickstart", h.handleQuickstart)
	mux.HandleFunc("GET /admin/providers/registry", h.listRegistryProviders)

	mux.HandleFunc("GET /admin/roles", h.listRoles)

	// BATCH-49: Guardrail catalog + test endpoints
	mux.HandleFunc("GET /admin/guardrails/catalog", h.listGuardrailCatalog)
	mux.HandleFunc("POST /admin/guardrails/test", h.testGuardrail)

	// BATCH-50: Guardrail stats endpoint
	mux.HandleFunc("GET /admin/guardrails/stats", h.getGuardrailStats)

	// RBAC user management (only when RBAC is enabled)
	if h.rbacEnabled {
		mux.HandleFunc("POST /admin/users", h.createUser)
		mux.HandleFunc("GET /admin/users", h.listUsers)
		mux.HandleFunc("DELETE /admin/users/{id}", h.deleteUser)
		mux.HandleFunc("PUT /admin/users/{id}/role", h.updateUserRole)
	}
}

// BearerAuth returns middleware that validates admin authentication.
// When OIDC is configured, it tries JWT validation first, then falls back to
// the static bearer token. This ensures backward compatibility and bootstrapping.
func BearerAuth(token string, auditLog *audit.Logger, oidcProvider *oidcPkg.Provider, oidcLookup oidcPkg.UserLookupFunc, next http.Handler) http.Handler {
	// If no auth is configured at all, pass through
	if token == "" && oidcProvider == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeAdminError(w, r, http.StatusUnauthorized, "unauthorized", "missing authorization header")
			return
		}
		rawToken := strings.TrimPrefix(auth, "Bearer ")

		// Try OIDC first (when provider is configured)
		if oidcProvider != nil {
			oc, err := oidcProvider.ValidateToken(r.Context(), rawToken, oidcLookup)
			if err == nil {
				// OIDC validation succeeded — inject identity into context
				ctx := oidcPkg.WithContext(r.Context(), oc)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			// OIDC validation failed — continue to static token check
		}

		// Static bearer token fallback
		if token != "" && subtle.ConstantTimeCompare([]byte(rawToken), []byte(token)) == 1 {
			next.ServeHTTP(w, r)
			return
		}

		// All auth methods failed
		if auditLog != nil {
			auditLog.Record(audit.Event{
				EventType: audit.EventAuthFailure,
				Actor:     maskToken(auth),
				Action:    "admin_access",
				Resource:  r.URL.Path,
				Outcome:   "denied",
				RequestID: requestid.FromContext(r.Context()),
				Metadata:  map[string]any{"remote_addr": r.RemoteAddr},
			})
		}
		writeAdminError(w, r, http.StatusUnauthorized, "unauthorized", "invalid or missing admin token")
	})
}

func writeAdminJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAdminError(w http.ResponseWriter, r *http.Request, status int, typ string, message string) {
	requestID := ""
	if r != nil {
		requestID = requestid.FromContext(r.Context())
	}
	writeAdminJSON(w, status, openaischema.ErrorResponse{Error: openaischema.ErrorBody{
		Message:   message,
		Type:      typ,
		RequestID: requestID,
	}})
}

func readJSON(r *http.Request, dest any) error {
	if r.Body == nil {
		return fmt.Errorf("request body is empty")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dest)
}

func actorFromRequest(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		if len(token) > 8 {
			return "token:" + token[:8] + "..."
		}
		return "token"
	}
	return "admin"
}

func (h *Handler) handleQuickstart(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "key:create")
	if actor == nil {
		return
	}

	var req struct {
		Name           string  `json:"name"`
		RPMLimit       int     `json:"rpm_limit"`
		BudgetLimitUSD float64 `json:"budget_limit_usd"`
	}
	if err := readJSON(r, &req); err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	keyName := req.Name
	if keyName == "" {
		keyName = "quickstart"
	}

	// 1. Create project
	project, err := store.CreateProject(r.Context(), h.db, "quickstart-"+time.Now().Format("2006-01-02"))
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to create project")
		return
	}

	// 2. Generate key
	rawKey, err := generateKey()
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to generate key")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to hash key")
		return
	}

	prefix := rawKey[:8]
	vk := &store.VirtualKey{
		ProjectID:      project.ID,
		KeyPrefix:      prefix,
		KeyHash:        string(hash),
		Name:           keyName,
		RPMLimit:       req.RPMLimit,
		BudgetLimitUSD: req.BudgetLimitUSD,
		BudgetPeriod:   "monthly",
	}
	if err := store.CreateVirtualKey(r.Context(), h.db, vk); err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to create key")
		return
	}

	// 3. Return both
	writeAdminJSON(w, http.StatusCreated, map[string]any{
		"project": project,
		"key": map[string]any{
			"id":               vk.ID,
			"key":              rawKey,
			"key_prefix":       prefix,
			"name":             vk.Name,
			"rpm_limit":        vk.RPMLimit,
			"budget_limit_usd": vk.BudgetLimitUSD,
		},
	})
}

func maskToken(auth string) string {
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		if len(token) > 8 {
			return "token:" + token[:4] + "****"
		}
		return "token:****"
	}
	if len(auth) > 8 {
		return auth[:4] + "****"
	}
	return "****"
}

// listRegistryProviders returns all providers from the embedded registry.
func (h *Handler) listRegistryProviders(w http.ResponseWriter, r *http.Request) {
	type providerEntry struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		BaseURL   string `json:"base_url"`
		Available bool   `json:"available"` // true if configured in user's config
	}

	// Check which providers the user has configured
	configured := map[string]bool{}
	for name, pc := range h.cfg.Providers {
		configured[name] = pc.BaseURL != ""
	}

	entries := make([]providerEntry, 0, len(providers.DefaultRegistry))
	for name, def := range providers.DefaultRegistry {
		entries = append(entries, providerEntry{
			Name:      name,
			Type:      def.BaseType,
			BaseURL:   def.BaseURL,
			Available: configured[name],
		})
	}

	writeAdminJSON(w, http.StatusOK, map[string]any{
		"providers": entries,
		"total":     len(entries),
	})
}
