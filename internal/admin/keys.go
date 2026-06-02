package admin

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"

	"golang.org/x/crypto/bcrypt"

	"openlimit/internal/audit"
	"openlimit/internal/requestid"
	"openlimit/internal/store"
)

type createKeyRequest struct {
	ProjectID        string   `json:"project_id"`
	Name             string   `json:"name"`
	AllowedModels    []string `json:"allowed_models"`
	AllowedProviders []string `json:"allowed_providers"`
	AllowedTools     []string `json:"allowed_tools"`
	RPMLimit         int      `json:"rpm_limit"`
	TPMLimit         int      `json:"tpm_limit"`
	BudgetLimitUSD   float64  `json:"budget_limit_usd"`
	BudgetPeriod     string   `json:"budget_period"`
	AllowMCPServer   bool     `json:"allow_mcp_server"`
	MCPToolName      string   `json:"mcp_tool_name"`
}

type createKeyResponse struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	KeyPrefix string `json:"key_prefix"`
	Name      string `json:"name"`
	ProjectID string `json:"project_id"`
}

func (h *Handler) createKey(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "key:create")
	if actor == nil {
		return
	}

	var req createKeyRequest
	if err := readJSON(r, &req); err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.ProjectID == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "project_id is required")
		return
	}
	if req.Name == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.BudgetPeriod == "" {
		req.BudgetPeriod = "monthly"
	}
	if req.BudgetLimitUSD < 0 {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "budget_limit_usd must be non-negative")
		return
	}

	// Generate random key: gw- + 32 hex chars
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

	prefix := rawKey[:8] // "gw-xxxx"

	vk := &store.VirtualKey{
		ProjectID:        req.ProjectID,
		KeyPrefix:        prefix,
		KeyHash:          string(hash),
		Name:             req.Name,
		AllowedModels:    req.AllowedModels,
		AllowedProviders: req.AllowedProviders,
		AllowedTools:     req.AllowedTools,
		RPMLimit:         req.RPMLimit,
		TPMLimit:         req.TPMLimit,
		BudgetLimitUSD:   req.BudgetLimitUSD,
		BudgetPeriod:     req.BudgetPeriod,
		AllowMCPServer:   req.AllowMCPServer,
		MCPToolName:      req.MCPToolName,
	}

	if err := store.CreateVirtualKey(r.Context(), h.db, vk); err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to create key")
		return
	}

	writeAdminJSON(w, http.StatusCreated, createKeyResponse{
		ID:        vk.ID,
		Key:       rawKey,
		KeyPrefix: prefix,
		Name:      vk.Name,
		ProjectID: vk.ProjectID,
	})

	h.audit.Record(audit.Event{
		EventType: audit.EventKeyCreate,
		Actor:     actorFromRequest(r),
		Action:    "create",
		Resource:  "key:" + vk.ID,
		Outcome:   "success",
		RequestID: requestid.FromContext(r.Context()),
		Metadata:  map[string]any{"project_id": vk.ProjectID, "name": vk.Name},
	})

	// Notify MCP server clients that tools may have changed
	if h.OnKeysChanged != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Default().Error("panic in OnKeysChanged", "error", r)
				}
			}()
			h.OnKeysChanged()
		}()
	}
}

func (h *Handler) listKeys(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "key:list")
	if actor == nil {
		return
	}

	projectID := r.URL.Query().Get("project_id")

	// Parse pagination parameters
	offset := 0
	limit := 100 // default matches pre-pagination behavior
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}

	// SQL-level pagination with COUNT(*) for total
	totalCount, err := store.CountVirtualKeys(r.Context(), h.db, projectID)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to count keys")
		return
	}

	keys, err := store.ListVirtualKeys(r.Context(), h.db, projectID, offset, limit)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to list keys")
		return
	}
	if keys == nil {
		keys = []store.VirtualKey{}
	}

	w.Header().Set("X-Total-Count", strconv.Itoa(totalCount))
	writeAdminJSON(w, http.StatusOK, keys)
}

func (h *Handler) revokeKey(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "key:revoke")
	if actor == nil {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "key id is required")
		return
	}

	revoked, err := store.RevokeVirtualKey(r.Context(), h.db, id)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to revoke key")
		return
	}
	if !revoked {
		writeAdminError(w, r, http.StatusNotFound, "not_found", "key not found or already revoked")
		return
	}

	h.audit.Record(audit.Event{
		EventType: audit.EventKeyRevoke,
		Actor:     actorFromRequest(r),
		Action:    "revoke",
		Resource:  "key:" + id,
		Outcome:   "success",
		RequestID: requestid.FromContext(r.Context()),
	})

	w.WriteHeader(http.StatusNoContent)

	// Notify MCP server clients that tools may have changed
	if h.OnKeysChanged != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Default().Error("panic in OnKeysChanged", "error", r)
				}
			}()
			h.OnKeysChanged()
		}()
	}
}

// protectedKeyFields may never be modified via the update API.
var protectedKeyFields = map[string]bool{
	"key_hash":   true,
	"key_prefix": true,
	"id":         true,
	"project_id": true,
	"created_at": true,
}

// updateKey handles PUT /admin/keys/{id} — full replacement of updatable fields.
func (h *Handler) updateKey(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "key:update")
	if actor == nil {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "key id is required")
		return
	}

	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if len(body) == 0 {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "request body is empty")
		return
	}

	// Reject attempts to modify protected fields.
	for field := range body {
		if protectedKeyFields[field] {
			writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "field '"+field+"' is read-only")
			return
		}
	}

	if err := store.UpdateVirtualKey(r.Context(), h.db, id, body); err != nil {
		if err == store.ErrNotFound {
			writeAdminError(w, r, http.StatusNotFound, "not_found", "key not found")
			return
		}
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to update key")
		return
	}

	// BATCH-30 BN-02: Direct SELECT by ID instead of ListVirtualKeys scan.
	updated, err := store.GetVirtualKeyByID(r.Context(), h.db, id)
	if err != nil {
		writeAdminJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}

	writeAdminJSON(w, http.StatusOK, updated)

	h.audit.Record(audit.Event{
		EventType: audit.EventKeyUpdate,
		Actor:     actorFromRequest(r),
		Action:    "update",
		Resource:  "key:" + id,
		Outcome:   "success",
		RequestID: requestid.FromContext(r.Context()),
		Metadata:  map[string]any{"fields": body},
	})

	if h.OnKeysChanged != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Default().Error("panic in OnKeysChanged", "error", r)
				}
			}()
			h.OnKeysChanged()
		}()
	}
}

// patchKey handles PATCH /admin/keys/{id} — partial update of updatable fields.
func (h *Handler) patchKey(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "key:update")
	if actor == nil {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "key id is required")
		return
	}

	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if len(body) == 0 {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "request body is empty")
		return
	}

	// Reject attempts to modify protected fields.
	for field := range body {
		if protectedKeyFields[field] {
			writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "field '"+field+"' is read-only")
			return
		}
	}

	if err := store.UpdateVirtualKey(r.Context(), h.db, id, body); err != nil {
		if err == store.ErrNotFound {
			writeAdminError(w, r, http.StatusNotFound, "not_found", "key not found")
			return
		}
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to update key")
		return
	}

	// BATCH-30 BN-02: Direct SELECT by ID instead of ListVirtualKeys scan.
	updated, err := store.GetVirtualKeyByID(r.Context(), h.db, id)
	if err != nil {
		writeAdminJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}

	writeAdminJSON(w, http.StatusOK, updated)

	h.audit.Record(audit.Event{
		EventType: audit.EventKeyUpdate,
		Actor:     actorFromRequest(r),
		Action:    "patch",
		Resource:  "key:" + id,
		Outcome:   "success",
		RequestID: requestid.FromContext(r.Context()),
		Metadata:  map[string]any{"fields": body},
	})

	if h.OnKeysChanged != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Default().Error("panic in OnKeysChanged", "error", r)
				}
			}()
			h.OnKeysChanged()
		}()
	}
}

// generateKey creates a random virtual key in the format "gw-" + 32 hex chars.
func generateKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "gw-" + hex.EncodeToString(buf), nil
}

func (h *Handler) rotateKey(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "key:update")
	if actor == nil {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "key id is required")
		return
	}

	// Generate new key
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

	vk, raw, err := store.RotateVirtualKey(r.Context(), h.db, id, rawKey, hash)
	if err != nil {
		if err == store.ErrNotFound {
			writeAdminError(w, r, http.StatusNotFound, "not_found", "key not found")
			return
		}
		if err.Error() == "key is revoked" {
			writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "key is revoked")
			return
		}
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to rotate key")
		return
	}

	writeAdminJSON(w, http.StatusOK, createKeyResponse{
		ID:        vk.ID,
		Key:       raw,
		KeyPrefix: vk.KeyPrefix,
		Name:      vk.Name,
		ProjectID: vk.ProjectID,
	})

	h.audit.Record(audit.Event{
		EventType: audit.EventKeyRotate,
		Actor:     actorFromRequest(r),
		Action:    "rotate",
		Resource:  "key:" + id,
		Outcome:   "success",
		RequestID: requestid.FromContext(r.Context()),
	})

	if h.OnKeysChanged != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Default().Error("panic in OnKeysChanged", "error", r)
				}
			}()
			h.OnKeysChanged()
		}()
	}
}
