package admin

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"openlimit/internal/config"
)

// ---------------------------------------------------------------------------
// Shared test helpers (used by rbac_test.go and handler_test.go)
// ---------------------------------------------------------------------------

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	url := testDBURLShared()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("cannot ping postgres: %v", err)
	}
	return db
}

func testDBURLShared() string {
	for _, env := range []string{"TEST_DATABASE_URL", "DATABASE_URL"} {
		if u := os.Getenv(env); u != "" {
			return u
		}
	}
	return "postgres://openlimit:openlimit@localhost:5432/openlimit_test?sslmode=disable"
}

func readBody(t *testing.T, buf *bytes.Buffer) string {
	t.Helper()
	return buf.String()
}

// newTestHandler creates a Handler wired to a real test database.
func newTestHandler(t *testing.T) (*Handler, *sql.DB) {
	t.Helper()
	db := setupDB(t)
	cfg := config.Default()
	h := NewHandler(db, cfg, nil, nil)
	return h, db
}

// ---------------------------------------------------------------------------
// TEST-7D-03-01: Quickstart with no body → 201, response has project.id
//   and key.key, key.name is "quickstart"
// ---------------------------------------------------------------------------

func TestQuickstart_NoBody_Returns201(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Wrap with BearerAuth middleware
	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	req := httptest.NewRequest(http.MethodPost, "/admin/quickstart", bytes.NewReader(nil))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	proj, ok := resp["project"].(map[string]any)
	if !ok {
		t.Fatal("response missing project object")
	}
	if proj["id"] == nil || proj["id"] == "" {
		t.Fatal("project.id is empty")
	}

	key, ok := resp["key"].(map[string]any)
	if !ok {
		t.Fatal("response missing key object")
	}
	if key["key"] == nil || key["key"] == "" {
		t.Fatal("key.key is empty")
	}
	if key["name"] != "quickstart" {
		t.Fatalf("expected key.name='quickstart', got %v", key["name"])
	}

	// Verify key starts with "gw-" and is long enough
	rawKey, _ := key["key"].(string)
	if len(rawKey) < 10 {
		t.Fatalf("key too short: %s", rawKey)
	}
}

// ---------------------------------------------------------------------------
// TEST-7D-03-02: Quickstart with {"rpm_limit":10} → 201, key.rpm_limit is 10
// ---------------------------------------------------------------------------

func TestQuickstart_WithRPMLimit_ReturnsRPMLimit(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	req := httptest.NewRequest(http.MethodPost, "/admin/quickstart", bytes.NewBufferString(`{"rpm_limit":10}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	key, ok := resp["key"].(map[string]any)
	if !ok {
		t.Fatal("response missing key object")
	}

	// JSON numbers decode as float64
	rpmLimit, _ := key["rpm_limit"].(float64)
	if rpmLimit != 10 {
		t.Fatalf("expected key.rpm_limit=10, got %v", key["rpm_limit"])
	}
}

// ---------------------------------------------------------------------------
// TEST-20-01-01: PUT /admin/keys/{id} updates name and limits
// ---------------------------------------------------------------------------

func TestUpdateKey_UpdatesNameAndLimits(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// 1. Create a project
	projReq := httptest.NewRequest(http.MethodPost, "/admin/projects", bytes.NewBufferString(`{"name":"test-update-key-"}`))
	projReq.Header.Set("Authorization", "Bearer test-admin-token")
	projW := httptest.NewRecorder()
	authed.ServeHTTP(projW, projReq)
	if projW.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d; body: %s", projW.Code, projW.Body.String())
	}
	var projResp map[string]any
	if err := json.Unmarshal(projW.Body.Bytes(), &projResp); err != nil {
		t.Fatalf("parse project: %v", err)
	}
	projectID, _ := projResp["id"].(string)

	// 2. Create a key
	keyBody := fmt.Sprintf(`{"project_id":"%s","name":"original","rpm_limit":10,"tpm_limit":100}`, projectID)
	keyReq := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewBufferString(keyBody))
	keyReq.Header.Set("Authorization", "Bearer test-admin-token")
	keyW := httptest.NewRecorder()
	authed.ServeHTTP(keyW, keyReq)
	if keyW.Code != http.StatusCreated {
		t.Fatalf("create key: expected 201, got %d; body: %s", keyW.Code, keyW.Body.String())
	}
	var keyResp map[string]any
	if err := json.Unmarshal(keyW.Body.Bytes(), &keyResp); err != nil {
		t.Fatalf("parse key: %v", err)
	}
	keyID, _ := keyResp["id"].(string)

	// 3. PUT update the key
	updateBody := `{"name":"updated-name","rpm_limit":50,"tpm_limit":500}`
	putReq := httptest.NewRequest(http.MethodPut, "/admin/keys/"+keyID, bytes.NewBufferString(updateBody))
	putReq.Header.Set("Authorization", "Bearer test-admin-token")
	putW := httptest.NewRecorder()
	authed.ServeHTTP(putW, putReq)

	if putW.Code != http.StatusOK {
		t.Fatalf("PUT update: expected 200, got %d; body: %s", putW.Code, putW.Body.String())
	}

	var updated map[string]any
	if err := json.Unmarshal(putW.Body.Bytes(), &updated); err != nil {
		t.Fatalf("parse updated key: %v", err)
	}

	if updated["name"] != "updated-name" {
		t.Errorf("name = %v, want 'updated-name'", updated["name"])
	}
	rpmLimit, _ := updated["rpm_limit"].(float64)
	if rpmLimit != 50 {
		t.Errorf("rpm_limit = %v, want 50", updated["rpm_limit"])
	}
	tpmLimit, _ := updated["tpm_limit"].(float64)
	if tpmLimit != 500 {
		t.Errorf("tpm_limit = %v, want 500", updated["tpm_limit"])
	}
}

// ---------------------------------------------------------------------------
// TEST-20-01-02: PUT /admin/keys/{id} returns 404 for missing key
// ---------------------------------------------------------------------------

func TestUpdateKey_Returns404ForMissingKey(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	updateBody := `{"name":"does-not-matter"}`
	putReq := httptest.NewRequest(http.MethodPut, "/admin/keys/nonexistent-id", bytes.NewBufferString(updateBody))
	putReq.Header.Set("Authorization", "Bearer test-admin-token")
	putW := httptest.NewRecorder()
	authed.ServeHTTP(putW, putReq)

	if putW.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", putW.Code, putW.Body.String())
	}

	var errResp map[string]any
	if err := json.Unmarshal(putW.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	errObj, _ := errResp["error"].(map[string]any)
	if errObj["type"] != "not_found" {
		t.Errorf("error.type = %v, want 'not_found'", errObj["type"])
	}
}

// ---------------------------------------------------------------------------
// TEST-20-01-03: PATCH /admin/keys/{id} partial update works
// ---------------------------------------------------------------------------

func TestPatchKey_PartialUpdateWorks(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// 1. Create project + key
	projReq := httptest.NewRequest(http.MethodPost, "/admin/projects", bytes.NewBufferString(`{"name":"test-patch-key-"}`))
	projReq.Header.Set("Authorization", "Bearer test-admin-token")
	projW := httptest.NewRecorder()
	authed.ServeHTTP(projW, projReq)
	var projResp map[string]any
	json.Unmarshal(projW.Body.Bytes(), &projResp)
	projectID, _ := projResp["id"].(string)

	keyBody := fmt.Sprintf(`{"project_id":"%s","name":"original","rpm_limit":10,"tpm_limit":100}`, projectID)
	keyReq := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewBufferString(keyBody))
	keyReq.Header.Set("Authorization", "Bearer test-admin-token")
	keyW := httptest.NewRecorder()
	authed.ServeHTTP(keyW, keyReq)
	var keyResp map[string]any
	json.Unmarshal(keyW.Body.Bytes(), &keyResp)
	keyID, _ := keyResp["id"].(string)

	// 2. PATCH only name
	patchBody := `{"name":"patched-name"}`
	patchReq := httptest.NewRequest(http.MethodPatch, "/admin/keys/"+keyID, bytes.NewBufferString(patchBody))
	patchReq.Header.Set("Authorization", "Bearer test-admin-token")
	patchW := httptest.NewRecorder()
	authed.ServeHTTP(patchW, patchReq)

	if patchW.Code != http.StatusOK {
		t.Fatalf("PATCH: expected 200, got %d; body: %s", patchW.Code, patchW.Body.String())
	}

	var patched map[string]any
	if err := json.Unmarshal(patchW.Body.Bytes(), &patched); err != nil {
		t.Fatalf("parse patched key: %v", err)
	}

	if patched["name"] != "patched-name" {
		t.Errorf("name = %v, want 'patched-name'", patched["name"])
	}
	// rpm_limit should still be 10 (unchanged)
	rpmLimit, _ := patched["rpm_limit"].(float64)
	if rpmLimit != 10 {
		t.Errorf("rpm_limit = %v, want 10 (unchanged)", patched["rpm_limit"])
	}
	tpmLimit, _ := patched["tpm_limit"].(float64)
	if tpmLimit != 100 {
		t.Errorf("tpm_limit = %v, want 100 (unchanged)", patched["tpm_limit"])
	}
}

// ---------------------------------------------------------------------------
// TEST-20-01-04: PATCH rejects attempts to modify key_hash
// ---------------------------------------------------------------------------

func TestPatchKey_RejectsProtectedField(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// Create project + key
	projReq := httptest.NewRequest(http.MethodPost, "/admin/projects", bytes.NewBufferString(`{"name":"test-protected-field-"}`))
	projReq.Header.Set("Authorization", "Bearer test-admin-token")
	projW := httptest.NewRecorder()
	authed.ServeHTTP(projW, projReq)
	var projResp map[string]any
	json.Unmarshal(projW.Body.Bytes(), &projResp)
	projectID, _ := projResp["id"].(string)

	keyBody := fmt.Sprintf(`{"project_id":"%s","name":"original"}`, projectID)
	keyReq := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewBufferString(keyBody))
	keyReq.Header.Set("Authorization", "Bearer test-admin-token")
	keyW := httptest.NewRecorder()
	authed.ServeHTTP(keyW, keyReq)
	var keyResp map[string]any
	json.Unmarshal(keyW.Body.Bytes(), &keyResp)
	keyID, _ := keyResp["id"].(string)

	// PATCH with key_hash (protected)
	patchBody := `{"key_hash":"$2a$10$malicioushash","name":"hacked"}`
	patchReq := httptest.NewRequest(http.MethodPatch, "/admin/keys/"+keyID, bytes.NewBufferString(patchBody))
	patchReq.Header.Set("Authorization", "Bearer test-admin-token")
	patchW := httptest.NewRecorder()
	authed.ServeHTTP(patchW, patchReq)

	if patchW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", patchW.Code, patchW.Body.String())
	}

	var errResp map[string]any
	if err := json.Unmarshal(patchW.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	errObj, _ := errResp["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if msg != "field 'key_hash' is read-only" {
		t.Errorf("error.message = %q, want field 'key_hash' is read-only", msg)
	}
}

// ---------------------------------------------------------------------------
// TEST-20-01-05: PUT returns 400 for empty body
// ---------------------------------------------------------------------------

func TestUpdateKey_Returns400ForEmptyBody(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// Create project + key to get a real ID
	projReq := httptest.NewRequest(http.MethodPost, "/admin/projects", bytes.NewBufferString(`{"name":"test-empty-body-"}`))
	projReq.Header.Set("Authorization", "Bearer test-admin-token")
	projW := httptest.NewRecorder()
	authed.ServeHTTP(projW, projReq)
	var projResp map[string]any
	json.Unmarshal(projW.Body.Bytes(), &projResp)
	projectID, _ := projResp["id"].(string)

	keyBody := fmt.Sprintf(`{"project_id":"%s","name":"original"}`, projectID)
	keyReq := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewBufferString(keyBody))
	keyReq.Header.Set("Authorization", "Bearer test-admin-token")
	keyW := httptest.NewRecorder()
	authed.ServeHTTP(keyW, keyReq)
	var keyResp map[string]any
	json.Unmarshal(keyW.Body.Bytes(), &keyResp)
	keyID, _ := keyResp["id"].(string)

	// PUT with empty JSON object
	putReq := httptest.NewRequest(http.MethodPut, "/admin/keys/"+keyID, bytes.NewBufferString(`{}`))
	putReq.Header.Set("Authorization", "Bearer test-admin-token")
	putW := httptest.NewRecorder()
	authed.ServeHTTP(putW, putReq)

	if putW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", putW.Code, putW.Body.String())
	}

	var errResp map[string]any
	if err := json.Unmarshal(putW.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	errObj, _ := errResp["error"].(map[string]any)
	if errObj["type"] != "invalid_request" {
		t.Errorf("error.type = %v, want 'invalid_request'", errObj["type"])
	}
}

// ---------------------------------------------------------------------------
// TEST-7D-03-03: Quickstart without admin auth → 401
// ---------------------------------------------------------------------------

func TestQuickstart_NoAuth_Returns401(t *testing.T) {
	// No DB needed — BearerAuth middleware rejects before handler runs
	inner := http.NewServeMux()
	inner.HandleFunc("POST /admin/quickstart", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without auth")
	})

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, inner))

	req := httptest.NewRequest(http.MethodPost, "/admin/quickstart", nil)
	// No Authorization header
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}
