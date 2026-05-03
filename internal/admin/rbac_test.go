package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"openlimit/internal/config"
	"openlimit/internal/store"
)

// --- requireRole tests ---

func TestRequireRole_AdminAllowed(t *testing.T) {
	h := &Handler{rbacEnabled: true}
	req := httptest.NewRequest(http.MethodPost, "/admin/projects", nil)
	w := httptest.NewRecorder()

	actor := h.requireRole(w, req, "project:create")
	if actor == nil {
		t.Fatal("expected actor for admin role")
	}
	if actor.Role != store.RoleAdmin {
		t.Errorf("role = %q, want %q", actor.Role, store.RoleAdmin)
	}
}

func TestRequireRole_ViewerDeniedForKeyCreate(t *testing.T) {
	h := &Handler{
		rbacEnabled: true,
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/keys", nil)
	// Simulate viewer via OIDC context
	ctx := withOIDCContext(req.Context(), "viewer@example.com", store.RoleViewer)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	actor := h.requireRole(w, req, "key:create")
	if actor != nil {
		t.Fatal("expected nil actor for viewer on key:create")
	}
	if w.Result().StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Result().StatusCode)
	}
}

func TestRequireRole_EditorAllowedForKeyCreate(t *testing.T) {
	h := &Handler{rbacEnabled: true}
	req := httptest.NewRequest(http.MethodPost, "/admin/keys", nil)
	ctx := withOIDCContext(req.Context(), "editor@example.com", store.RoleEditor)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	actor := h.requireRole(w, req, "key:create")
	if actor == nil {
		t.Fatal("expected actor for editor on key:create")
	}
}

func TestRequireRole_EditorDeniedForProjectDelete(t *testing.T) {
	h := &Handler{rbacEnabled: true}
	req := httptest.NewRequest(http.MethodDelete, "/admin/projects/123", nil)
	ctx := withOIDCContext(req.Context(), "editor@example.com", store.RoleEditor)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	actor := h.requireRole(w, req, "project:delete")
	if actor != nil {
		t.Fatal("expected nil for editor on project:delete")
	}
	if w.Result().StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Result().StatusCode)
	}
}

func TestRequireRole_RBACDisabled(t *testing.T) {
	h := &Handler{rbacEnabled: false}
	req := httptest.NewRequest(http.MethodPost, "/admin/projects", nil)
	w := httptest.NewRecorder()

	actor := h.requireRole(w, req, "project:create")
	if actor == nil {
		t.Fatal("expected actor when RBAC disabled")
	}
	if actor.Role != store.RoleAdmin {
		t.Errorf("role = %q, want admin when RBAC disabled", actor.Role)
	}
}

// --- Viewer sweep: verify all endpoints with viewer role ---

func TestViewerSweep(t *testing.T) {
	db := setupDB(t)

	cfg := config.Config{Admin: config.AdminConfig{RBACEnabled: true}}
	h := NewHandler(db, cfg, nil, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	readEndpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/admin/projects"},
		{"GET", "/admin/keys"},
		{"GET", "/admin/usage"},
		{"GET", "/admin/usage/summary"},
		{"GET", "/admin/audit"},
	}

	writeEndpoints := []struct {
		method string
		path   string
		body   string
	}{
		{"POST", "/admin/projects", `{"name":"sweep-test"}`},
		{"DELETE", "/admin/projects/nonexistent", ""},
		{"POST", "/admin/keys", `{"project_id":"nonexistent","name":"sweep-key"}`},
		{"DELETE", "/admin/keys/nonexistent", ""},
	}

	// Read endpoints should return 200 (or 200 with empty array)
	for _, ep := range readEndpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		ctx := withOIDCContext(req.Context(), "viewer@example.com", store.RoleViewer)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Result().StatusCode == http.StatusForbidden {
			t.Errorf("viewer should access %s %s, got 403", ep.method, ep.path)
		}
	}

	// Write endpoints should return 403
	for _, ep := range writeEndpoints {
		var body *bytes.Buffer
		if ep.body != "" {
			body = bytes.NewBufferString(ep.body)
		}
		req := httptest.NewRequest(ep.method, ep.path, body)
		if ep.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		ctx := withOIDCContext(req.Context(), "viewer@example.com", store.RoleViewer)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Result().StatusCode != http.StatusForbidden {
			t.Errorf("viewer should be denied on %s %s, got %d", ep.method, ep.path, w.Result().StatusCode)
		}
	}
}

// --- User CRUD tests ---

func TestUserCRUD(t *testing.T) {
	db := setupDB(t)
	db.Exec("DELETE FROM admin_users")

	cfg := config.Config{Admin: config.AdminConfig{RBACEnabled: true}}
	h := NewHandler(db, cfg, nil, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Create user
	body := `{"subject":"oidc-123","email":"crud@example.com","role":"editor"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusCreated {
		t.Fatalf("create user: expected 201, got %d: %s", w.Result().StatusCode, readBody(t, w.Body))
	}

	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	userID := created["id"].(string)

	// List users
	req = httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("list users: expected 200, got %d", w.Result().StatusCode)
	}

	// Update role
	body = `{"role":"admin"}`
	req = httptest.NewRequest(http.MethodPut, "/admin/users/"+userID+"/role", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("update role: expected 200, got %d: %s", w.Result().StatusCode, readBody(t, w.Body))
	}

	// Delete user
	req = httptest.NewRequest(http.MethodDelete, "/admin/users/"+userID, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("delete user: expected 204, got %d", w.Result().StatusCode)
	}

	// Verify user is gone
	req = httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var users []map[string]any
	json.NewDecoder(w.Body).Decode(&users)
	for _, u := range users {
		if u["id"] == userID {
			t.Fatal("user should be deleted but still appears in list")
		}
	}
}

// --- OIDC context test helper ---

func withOIDCContext(ctx context.Context, email, role string) context.Context {
	return context.WithValue(ctx, oidcContextKey{}, &oidcIdentity{Email: email, Role: role})
}
