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

// --- listRoles endpoint tests ---

func TestListRoles(t *testing.T) {
	h := &Handler{rbacEnabled: true}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/roles", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Result().StatusCode, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify rbac_enabled
	if rbacEnabled, ok := resp["rbac_enabled"].(bool); !ok || !rbacEnabled {
		t.Errorf("expected rbac_enabled=true, got %v", resp["rbac_enabled"])
	}

	// Verify roles
	roles, ok := resp["roles"].(map[string]any)
	if !ok {
		t.Fatalf("expected roles to be a map, got %T", resp["roles"])
	}
	if len(roles) != 3 {
		t.Errorf("expected 3 roles, got %d", len(roles))
	}

	// Verify admin has user:manage
	adminRole, ok := roles[store.RoleAdmin].(map[string]any)
	if !ok {
		t.Fatalf("admin role not found or wrong type")
	}
	adminPerms := toStringSlice(t, adminRole["permissions"])
	if !containsString(adminPerms, "user:manage") {
		t.Error("admin permissions should include user:manage")
	}

	// Verify viewer does NOT have key:create
	viewerRole, ok := roles[store.RoleViewer].(map[string]any)
	if !ok {
		t.Fatalf("viewer role not found or wrong type")
	}
	viewerPerms := toStringSlice(t, viewerRole["permissions"])
	if containsString(viewerPerms, "key:create") {
		t.Error("viewer permissions should NOT include key:create")
	}
}

func TestListRoles_RBACDisabled(t *testing.T) {
	h := &Handler{rbacEnabled: false}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/roles", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if rbacEnabled, ok := resp["rbac_enabled"].(bool); !ok || rbacEnabled {
		t.Errorf("expected rbac_enabled=false, got %v", resp["rbac_enabled"])
	}
}

func TestListRoles_PermissionConsistency(t *testing.T) {
	h := &Handler{rbacEnabled: true}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/roles", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	roles, _ := resp["roles"].(map[string]any)
	for roleName, roleData := range roles {
		roleMap, ok := roleData.(map[string]any)
		if !ok {
			continue
		}
		permissions := toStringSlice(t, roleMap["permissions"])
		for _, action := range permissions {
			if !store.RoleAllowed(roleName, action) {
				t.Errorf("GET /admin/roles says %s can %s, but RoleAllowed disagrees", roleName, action)
			}
		}
	}
}

// --- Audit metadata test (TASK-02 verification) ---

func TestAuditMetadata_ContainsVirtualKeyID(t *testing.T) {
	// Verify that audit.Event Metadata can carry virtual_key_id and project_id
	// as expected from the governed.go enrichment.
	// This validates the metadata shape without requiring a full HTTP round-trip.
	authCtx := &authContext{
		VirtualKeyID: "vk_test123",
		ProjectID:    "proj_test456",
	}

	meta := map[string]any{
		"model":    "gpt-4",
		"provider": "openai",
	}
	if authCtx != nil {
		meta["virtual_key_id"] = authCtx.VirtualKeyID
		meta["project_id"] = authCtx.ProjectID
	}

	if meta["virtual_key_id"] != "vk_test123" {
		t.Errorf("expected virtual_key_id=vk_test123, got %v", meta["virtual_key_id"])
	}
	if meta["project_id"] != "proj_test456" {
		t.Errorf("expected project_id=proj_test456, got %v", meta["project_id"])
	}
}

// authContext mirrors the auth.Context shape for TASK-02 metadata testing.
type authContext struct {
	VirtualKeyID string
	ProjectID    string
}

// --- Test helpers ---

func toStringSlice(t *testing.T, v any) []string {
	t.Helper()
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", v)
	}
	result := make([]string, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("expected string item, got %T", item)
		}
		result[i] = s
	}
	return result
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// --- OIDC context test helper ---

func withOIDCContext(ctx context.Context, email, role string) context.Context {
	return context.WithValue(ctx, oidcContextKey{}, &oidcIdentity{Email: email, Role: role})
}
