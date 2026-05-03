package admin

import (
	"context"
	"net/http"

	oidcPkg "openlimit/internal/oidc"

	"openlimit/internal/audit"
	"openlimit/internal/requestid"
	"openlimit/internal/store"
)

// ActorIdentity represents the authenticated admin user.
type ActorIdentity struct {
	Email  string `json:"email"`
	Role   string `json:"role"`
	Source string `json:"source"` // "oidc" or "token"
}

// actorIdentity extracts the admin identity from the request context.
// When OIDC is configured, the identity comes from OIDC claims.
// Otherwise, it falls back to bearer token (backward compat — always admin role).
func (h *Handler) actorIdentity(r *http.Request) *ActorIdentity {
	// Check for OIDC identity (injected by OIDC middleware)
	if ac := oidcFromContext(r.Context()); ac != nil {
		return &ActorIdentity{
			Email:  ac.Email,
			Role:   ac.Role,
			Source: "oidc",
		}
	}
	// Fallback: bearer token auth (backward compat)
	return &ActorIdentity{
		Email:  "admin",
		Role:   store.RoleAdmin,
		Source: "token",
	}
}

// requireRole checks if the actor has permission for the given action.
// If the actor lacks permission, it writes a 403 response, emits an audit
// event, records an RBAC metric, and returns nil.
//
// Caller must return immediately if nil is returned.
func (h *Handler) requireRole(w http.ResponseWriter, r *http.Request, action string) *ActorIdentity {
	actor := h.actorIdentity(r)

	if !h.rbacEnabled {
		return actor // RBAC off — all authenticated users are admin
	}

	if !store.RoleAllowed(actor.Role, action) {
		h.recordRBACCheck(actor.Role, action, "deny")
		if h.audit != nil {
			h.audit.Record(audit.Event{
				EventType: audit.EventAuthDenied,
				Actor:     actor.Email,
				Action:    action,
				Resource:  r.URL.Path,
				Outcome:   "denied",
				RequestID: requestid.FromContext(r.Context()),
				Metadata:  map[string]any{"role": actor.Role, "source": actor.Source},
			})
		}
		writeAdminError(w, r, http.StatusForbidden, "forbidden", "insufficient permissions")
		return nil
	}

	h.recordRBACCheck(actor.Role, action, "allow")
	return actor
}

// recordRBACCheck records an RBAC check metric if the metrics collector is available.
func (h *Handler) recordRBACCheck(role, action, result string) {
	if h.metrics != nil {
		h.metrics.RecordRBACCheck(role, action, result)
	}
}

// --- User management endpoints ---

type createUserRequest struct {
	Subject string `json:"subject"`
	Email   string `json:"email"`
	Role    string `json:"role"`
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := readJSON(r, &req); err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.Subject == "" && req.Email == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "subject or email is required")
		return
	}
	if req.Role == "" {
		req.Role = store.RoleViewer
	}
	if !store.ValidateRole(req.Role) {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "role must be admin, editor, or viewer")
		return
	}

	u, err := store.CreateUser(r.Context(), h.db, req.Subject, req.Email, req.Role)
	if err != nil {
		writeAdminError(w, r, http.StatusConflict, "conflict", "user with this subject or email already exists")
		return
	}

	if h.audit != nil {
		h.audit.Record(audit.Event{
			EventType: audit.EventUserCreate,
			Actor:     actorFromRequest(r),
			Action:    "create",
			Resource:  "user:" + u.ID,
			Outcome:   "success",
			RequestID: requestid.FromContext(r.Context()),
			Metadata:  map[string]any{"subject": req.Subject, "email": req.Email, "role": req.Role},
		})
	}

	writeAdminJSON(w, http.StatusCreated, u)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := store.ListUsers(r.Context(), h.db)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to list users")
		return
	}
	if users == nil {
		users = []store.AdminUser{}
	}
	writeAdminJSON(w, http.StatusOK, users)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "user id is required")
		return
	}

	if err := store.SoftDeleteUser(r.Context(), h.db, id); err != nil {
		if err == store.ErrNotFound {
			writeAdminError(w, r, http.StatusNotFound, "not_found", "user not found")
			return
		}
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to delete user")
		return
	}

	if h.audit != nil {
		h.audit.Record(audit.Event{
			EventType: audit.EventUserDelete,
			Actor:     actorFromRequest(r),
			Action:    "delete",
			Resource:  "user:" + id,
			Outcome:   "success",
			RequestID: requestid.FromContext(r.Context()),
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

type updateRoleRequest struct {
	Role string `json:"role"`
}

func (h *Handler) updateUserRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "user id is required")
		return
	}

	var req updateRoleRequest
	if err := readJSON(r, &req); err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if !store.ValidateRole(req.Role) {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "role must be admin, editor, or viewer")
		return
	}

	if err := store.UpdateUserRole(r.Context(), h.db, id, req.Role); err != nil {
		if err == store.ErrNotFound {
			writeAdminError(w, r, http.StatusNotFound, "not_found", "user not found")
			return
		}
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to update role")
		return
	}

	if h.audit != nil {
		h.audit.Record(audit.Event{
			EventType: audit.EventUserUpdate,
			Actor:     actorFromRequest(r),
			Action:    "update_role",
			Resource:  "user:" + id,
			Outcome:   "success",
			RequestID: requestid.FromContext(r.Context()),
			Metadata:  map[string]any{"new_role": req.Role},
		})
	}

	writeAdminJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// --- OIDC context integration ---

// oidcIdentity mirrors oidc.Context for in-package use.
type oidcIdentity struct {
	Email string
	Role  string
}

// oidcContextKey is the context key for OIDC identity.
type oidcContextKey struct{}

// oidcFromContext extracts OIDC identity from the request context.
// Checks for the real oidc.Context from the oidc package, then falls back
// to the test-only oidcIdentity.
// Returns nil when OIDC is not in use.
func oidcFromContext(ctx context.Context) *oidcIdentity {
	// Check for real OIDC context
	if oc := oidcPkg.FromContext(ctx); oc != nil {
		return &oidcIdentity{Email: oc.Email, Role: oc.Role}
	}
	// Check for test-only OIDC identity
	if v := ctx.Value(oidcContextKey{}); v != nil {
		if id, ok := v.(*oidcIdentity); ok {
			return id
		}
	}
	return nil
}
