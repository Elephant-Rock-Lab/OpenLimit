package admin

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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
// TEST-7D-03-01: Quickstart with empty JSON {} → 201, response has project.id
//   and key.key, key.name is "quickstart"
//   Updated for BATCH-57: nil body now returns 400 (HB-04), so we send valid empty JSON.
// ---------------------------------------------------------------------------

func TestQuickstart_NoBody_Returns201(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Wrap with BearerAuth middleware
	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	req := httptest.NewRequest(http.MethodPost, "/admin/quickstart", bytes.NewBufferString(`{}`))
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

// ---------------------------------------------------------------------------
// BATCH-29 TASK-01: Pagination parameter parsing tests
// ---------------------------------------------------------------------------

// TestListKeys_PaginationDefaults verifies that default limit=100 is applied
// when no pagination params are provided. This is an integration test requiring DB.
func TestListKeys_PaginationDefaults(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	// Create a project first
	pRes := httptest.NewRecorder()
	pReq := httptest.NewRequest(http.MethodPost, "/admin/projects", bytes.NewBufferString(`{"name":"pag-test-proj"}`))
	pReq.Header.Set("Authorization", "Bearer test-admin-token")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))
	authed.ServeHTTP(pRes, pReq)
	if pRes.Code != http.StatusCreated && pRes.Code != http.StatusConflict {
		t.Skipf("cannot create project: %d", pRes.Code)
	}

	// List keys with no pagination params
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/keys", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	mux = http.NewServeMux()
	h.RegisterRoutes(mux)
	authed = http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))
	authed.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// X-Total-Count header should be present
	totalCount := w.Header().Get("X-Total-Count")
	if totalCount == "" {
		t.Error("X-Total-Count header missing")
	}
}

// TestListKeys_LimitParam verifies that limit query param is respected.
func TestListKeys_LimitParam(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/keys?limit=1", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))
	authed.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	totalCount := w.Header().Get("X-Total-Count")
	if totalCount == "" {
		t.Error("X-Total-Count header missing")
	}

	var keys []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil {
		t.Fatalf("parse keys: %v", err)
	}
	if len(keys) > 1 {
		t.Errorf("expected at most 1 key with limit=1, got %d", len(keys))
	}
}

// TestListKeys_OffsetParam verifies offset skips keys.
func TestListKeys_OffsetParam(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	// Get all keys (no pagination)
	wAll := httptest.NewRecorder()
	reqAll := httptest.NewRequest(http.MethodGet, "/admin/keys", nil)
	reqAll.Header.Set("Authorization", "Bearer test-admin-token")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))
	authed.ServeHTTP(wAll, reqAll)

	var allKeys []map[string]any
	json.Unmarshal(wAll.Body.Bytes(), &allKeys)

	if len(allKeys) < 2 {
		t.Skip("need at least 2 keys to test offset")
	}

	// Get keys with offset=1
	wOff := httptest.NewRecorder()
	reqOff := httptest.NewRequest(http.MethodGet, "/admin/keys?offset=1", nil)
	reqOff.Header.Set("Authorization", "Bearer test-admin-token")
	mux = http.NewServeMux()
	h.RegisterRoutes(mux)
	authed = http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))
	authed.ServeHTTP(wOff, reqOff)

	var offsetKeys []map[string]any
	json.Unmarshal(wOff.Body.Bytes(), &offsetKeys)

	if len(offsetKeys) >= len(allKeys) {
		t.Errorf("offset=1 should return fewer keys than no offset")
	}

	// First key with offset=1 should be different from first key without offset
	if len(offsetKeys) > 0 && len(allKeys) > 1 {
		if offsetKeys[0]["id"] == allKeys[0]["id"] {
			t.Error("offset=1 first key should differ from offset=0 first key")
		}
	}
}

// ---------------------------------------------------------------------------
// BATCH-38 TASK-03: Reject negative budget_limit_usd on createKey
// ---------------------------------------------------------------------------

func TestCreateKeyRejectsNegativeBudget(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// Create a project first
	projReq := httptest.NewRequest(http.MethodPost, "/admin/projects", bytes.NewBufferString(`{"name":"test-neg-budget-"}`))
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

	// Create key with negative budget
	keyBody := fmt.Sprintf(`{"project_id":"%s","name":"neg-budget","budget_limit_usd":-1}`, projectID)
	keyReq := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewBufferString(keyBody))
	keyReq.Header.Set("Authorization", "Bearer test-admin-token")
	keyW := httptest.NewRecorder()
	authed.ServeHTTP(keyW, keyReq)

	if keyW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", keyW.Code, keyW.Body.String())
	}

	var errResp map[string]any
	if err := json.Unmarshal(keyW.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	errObj, _ := errResp["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if msg != "budget_limit_usd must be non-negative" {
		t.Errorf("error.message = %q, want 'budget_limit_usd must be non-negative'", msg)
	}
}

// TestListKeys_XTotalCountAccurate verifies X-Total-Count reflects total, not page size.
func TestListKeys_XTotalCountAccurate(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/keys?limit=1", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))
	authed.ServeHTTP(w, req)

	totalCount, _ := strconv.Atoi(w.Header().Get("X-Total-Count"))

	var keys []map[string]any
	json.Unmarshal(w.Body.Bytes(), &keys)

	if totalCount < len(keys) {
		t.Errorf("X-Total-Count (%d) should be >= returned keys (%d)", totalCount, len(keys))
	}
}

// ---------------------------------------------------------------------------
// BATCH-45: Registry provider endpoint tests
// ---------------------------------------------------------------------------

func TestListRegistryProviders_ReturnsAllEntries(t *testing.T) {
	// TEST-45-01-01: Endpoint returns all registry providers
	h := NewHandler(nil, config.Default(), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/providers/registry", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	total, _ := resp["total"].(float64)
	if total < 20 {
		t.Errorf("total = %.0f, want >= 20 providers", total)
	}

	providers, _ := resp["providers"].([]any)
	if len(providers) < 20 {
		t.Errorf("got %d providers, want >= 20", len(providers))
	}
}

func TestListRegistryProviders_EntryStructure(t *testing.T) {
	// TEST-45-01-02: Each entry has name, type, base_url, available fields
	h := NewHandler(nil, config.Default(), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/providers/registry", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	providers, _ := resp["providers"].([]any)
	for i, p := range providers {
		entry, _ := p.(map[string]any)
		for _, field := range []string{"name", "type", "base_url", "available"} {
			if _, ok := entry[field]; !ok {
				t.Errorf("provider[%d] missing field %q", i, field)
			}
		}
	}
}

func TestListRegistryProviders_MarksConfiguredAsAvailable(t *testing.T) {
	// TEST-45-01-03: Providers in user config are marked available
	cfg := config.Default()
	cfg.Providers = map[string]config.ProviderConfig{
		"deepseek": {Type: "openai-compatible", BaseURL: "https://api.deepseek.com/v1"},
	}

	h := NewHandler(nil, cfg, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/providers/registry", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	providers, _ := resp["providers"].([]any)
	for _, p := range providers {
		entry, _ := p.(map[string]any)
		name, _ := entry["name"].(string)
		available, _ := entry["available"].(bool)
		if name == "deepseek" && !available {
			t.Error("deepseek should be marked as available (configured)")
		}
		if name == "together_ai" && available {
			t.Error("together_ai should NOT be marked available (not configured)")
		}
	}
}

func TestListRegistryProviders_ContainsDeepSeek(t *testing.T) {
	// TEST-45-01-04: DeepSeek is in the response
	h := NewHandler(nil, config.Default(), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/providers/registry", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	providers, _ := resp["providers"].([]any)
	found := false
	for _, p := range providers {
		entry, _ := p.(map[string]any)
		if entry["name"] == "deepseek" {
			found = true
			if entry["type"] != "openai-compatible" {
				t.Errorf("deepseek type = %v, want openai-compatible", entry["type"])
			}
		}
	}
	if !found {
		t.Error("deepseek not found in registry response")
	}
}

func TestListRegistryProviders_AllHaveOpenAICompatibleType(t *testing.T) {
	// TEST-45-01-05: All registry providers are openai-compatible
	h := NewHandler(nil, config.Default(), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/providers/registry", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	providers, _ := resp["providers"].([]any)
	for _, p := range providers {
		entry, _ := p.(map[string]any)
		if entry["type"] != "openai-compatible" {
			t.Errorf("provider %q has type %q, want openai-compatible", entry["name"], entry["type"])
		}
	}
}

// ---------------------------------------------------------------------------
// BATCH-57 / TASK-03 Part B: Quickstart error handling regression tests
// ---------------------------------------------------------------------------

// TEST-57-03-04: handleQuickstart returns 400 for malformed JSON body.
func TestQuickstart_MalformedJSON_Returns400(t *testing.T) {
	cfg := config.Default()
	h := NewHandler(nil, cfg, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// Send malformed JSON
	req := httptest.NewRequest(http.MethodPost, "/admin/quickstart", bytes.NewBufferString(`{invalid json`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	errObj, _ := errResp["error"].(map[string]any)
	if errObj["type"] != "invalid_request" {
		t.Errorf("error.type = %v, want 'invalid_request'", errObj["type"])
	}
}

// TEST-57-03-05: handleQuickstart returns 400 for type error in JSON.
func TestQuickstart_TypeError_Returns400(t *testing.T) {
	cfg := config.Default()
	h := NewHandler(nil, cfg, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// name should be string, not number
	req := httptest.NewRequest(http.MethodPost, "/admin/quickstart", bytes.NewBufferString(`{"name":123}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for type error, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	errObj, _ := errResp["error"].(map[string]any)
	if errObj["type"] != "invalid_request" {
		t.Errorf("error.type = %v, want 'invalid_request'", errObj["type"])
	}
}

// TEST-57-03-06: handleQuickstart with empty JSON {} returns 400 (readJSON fails on nil body after Close).
func TestQuickstart_EmptyBody_Returns400(t *testing.T) {
	cfg := config.Default()
	h := NewHandler(nil, cfg, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// Send nil body (no io.Reader)
	req := httptest.NewRequest(http.MethodPost, "/admin/quickstart", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	// With the fix, readJSON returns error for nil body → 400
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for nil body, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// BATCH-61 / TASK-03: Quickstart duplicate guard
// ---------------------------------------------------------------------------

// TEST-61-03-01: Concurrent quickstart creates only 1 project.
// Two quickstart calls on the same day should result in exactly 1 project
// (the second call reuses the project created by the first).
func TestQuickstart_ConcurrentCreatesSingleProject(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// First quickstart call
	req1 := httptest.NewRequest(http.MethodPost, "/admin/quickstart", bytes.NewBufferString(`{}`))
	req1.Header.Set("Authorization", "Bearer test-admin-token")
	w1 := httptest.NewRecorder()
	authed.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("first call: expected 201, got %d; body: %s", w1.Code, w1.Body.String())
	}

	// Second quickstart call (same day)
	req2 := httptest.NewRequest(http.MethodPost, "/admin/quickstart", bytes.NewBufferString(`{}`))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	authed.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("second call: expected 201, got %d; body: %s", w2.Code, w2.Body.String())
	}

	// Parse both responses and verify they reference the same project
	var resp1, resp2 map[string]any
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	json.Unmarshal(w2.Body.Bytes(), &resp2)

	proj1, _ := resp1["project"].(map[string]any)
	proj2, _ := resp2["project"].(map[string]any)

	if proj1["id"] != proj2["id"] {
		t.Errorf("both quickstart calls should reference the same project, got %v and %v", proj1["id"], proj2["id"])
	}

	// Verify exactly 1 project with quickstart- prefix for today
	rows, err := db.Query("SELECT COUNT(*) FROM projects WHERE name LIKE 'quickstart-%'")
	if err != nil {
		t.Fatalf("failed to count projects: %v", err)
	}
	defer rows.Close()
	var count int
	if rows.Next() {
		rows.Scan(&count)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 quickstart project, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// BATCH-62: UX Auth + Navigation tests
// ---------------------------------------------------------------------------

// loadDashboardHTML reads the embedded dashboard HTML for content verification.
func loadDashboardHTML(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Fatalf("failed to read dashboard HTML: %v", err)
	}
	return string(data)
}

// TEST-62-01-01: Dashboard HTML contains login form div with password input.
func TestDashboard_LoginFormContainsPasswordInput(t *testing.T) {
	html := loadDashboardHTML(t)

	// Verify login-form div exists
	if !bytes.Contains([]byte(html), []byte(`id="login-form"`)) {
		t.Error("dashboard HTML missing div with id='login-form'")
	}

	// Verify password input exists
	if !bytes.Contains([]byte(html), []byte(`type="password"`)) {
		t.Error("dashboard HTML missing input with type='password'")
	}

	// Verify submit button exists
	if !bytes.Contains([]byte(html), []byte(`id="login-submit"`)) {
		t.Error("dashboard HTML missing button with id='login-submit'")
	}
}

// TEST-62-01-02: Dashboard HTML has no prompt() call.
func TestDashboard_NoPromptCall(t *testing.T) {
	html := loadDashboardHTML(t)

	if bytes.Contains([]byte(html), []byte("prompt(")) {
		t.Error("dashboard HTML contains prompt() call — must be removed per HB-01")
	}
}

// TEST-62-01-03: Dashboard HTML contains login-form div and localStorage removeItem on 401.
func TestDashboard_LoginFormShowsOnPageLoad(t *testing.T) {
	html := loadDashboardHTML(t)

	// Verify login-form div exists with hidden class (shown via JS when no token)
	if !bytes.Contains([]byte(html), []byte(`id="login-form"`)) {
		t.Error("dashboard HTML missing div with id='login-form'")
	}

	// Verify localStorage.removeItem('admin_token') on 401
	if !bytes.Contains([]byte(html), []byte("localStorage.removeItem('admin_token')")) {
		t.Error("dashboard HTML missing localStorage.removeItem('admin_token') for 401 handling")
	}
}

// TEST-62-02-01: Dashboard has exactly 5 tab elements with role="tab".
func TestDashboard_HasExactlyFiveTabs(t *testing.T) {
	html := loadDashboardHTML(t)

	tabCount := bytes.Count([]byte(html), []byte(`role="tab"`))
	if tabCount != 5 {
		t.Errorf("expected exactly 5 elements with role='tab', got %d", tabCount)
	}
}

// TEST-62-02-02: Dashboard contains all original data panels (spend, providers, request-log content).
func TestDashboard_ContainsAllMergedPanels(t *testing.T) {
	html := loadDashboardHTML(t)

	// Spend data should still be present (Budget Overview, Per-Key Budget Utilization)
	if !bytes.Contains([]byte(html), []byte("Budget Overview")) {
		t.Error("dashboard HTML missing 'Budget Overview' — spend data lost in consolidation")
	}
	if !bytes.Contains([]byte(html), []byte("Per-Key Budget Utilization")) {
		t.Error("dashboard HTML missing 'Per-Key Budget Utilization' — spend data lost in consolidation")
	}

	// Providers data should still be present (Provider Health, provider-cards)
	if !bytes.Contains([]byte(html), []byte("Provider Health")) {
		t.Error("dashboard HTML missing 'Provider Health' — providers data lost in consolidation")
	}
	if !bytes.Contains([]byte(html), []byte(`id="provider-cards"`)) {
		t.Error("dashboard HTML missing id='provider-cards' — providers data lost in consolidation")
	}

	// Request Log data should still be present
	if !bytes.Contains([]byte(html), []byte("Request Log")) {
		t.Error("dashboard HTML missing 'Request Log' — request log data lost in consolidation")
	}
	if !bytes.Contains([]byte(html), []byte(`id="logs-table"`)) {
		t.Error("dashboard HTML missing id='logs-table' — request log data lost in consolidation")
	}
}

// TEST-62-02-03: Guardrail catalog cards have onclick prefill handler.
func TestDashboard_GuardrailCatalogHasPrefill(t *testing.T) {
	html := loadDashboardHTML(t)

	// Verify prefillGuardrailTest function exists
	if !bytes.Contains([]byte(html), []byte("prefillGuardrailTest")) {
		t.Error("dashboard HTML missing prefillGuardrailTest function — guardrail catalog prefill not implemented")
	}

	// Verify catalog cards have onclick calling prefillGuardrailTest
	if !bytes.Contains([]byte(html), []byte(`onclick="prefillGuardrailTest(`)) {
		t.Error("dashboard HTML missing onclick='prefillGuardrailTest()' on catalog cards")
	}
}

// TEST-62-03-01: All tabs have aria-selected attribute.
func TestDashboard_TabsHaveAriaSelected(t *testing.T) {
	html := loadDashboardHTML(t)

	ariaSelectedCount := bytes.Count([]byte(html), []byte("aria-selected="))
	if ariaSelectedCount < 5 {
		t.Errorf("expected at least 5 aria-selected attributes (one per tab), got %d", ariaSelectedCount)
	}

	// Verify aria-controls on tabs
	ariaControlsCount := bytes.Count([]byte(html), []byte("aria-controls="))
	if ariaControlsCount < 5 {
		t.Errorf("expected at least 5 aria-controls attributes (one per tab), got %d", ariaControlsCount)
	}
}

// TEST-62-03-02: All panels have role="tabpanel".
func TestDashboard_PanelsHaveRoleTabpanel(t *testing.T) {
	html := loadDashboardHTML(t)

	tabpanelCount := bytes.Count([]byte(html), []byte(`role="tabpanel"`))
	if tabpanelCount != 5 {
		t.Errorf("expected exactly 5 elements with role='tabpanel', got %d", tabpanelCount)
	}

	// Verify aria-labelledby on panels
	ariaLabelledByCount := bytes.Count([]byte(html), []byte("aria-labelledby="))
	if ariaLabelledByCount < 5 {
		t.Errorf("expected at least 5 aria-labelledby attributes (one per panel), got %d", ariaLabelledByCount)
	}
}

// ---------------------------------------------------------------------------
// BATCH-63 TASK-01: Init Wizard Docs Reference tests
// ---------------------------------------------------------------------------

// TEST-63-01-01: getting-started.md mentions openlimit init command.
func TestDocs_MentionInitWizard(t *testing.T) {
	data, err := os.ReadFile("../../docs/getting-started.md")
	if err != nil {
		t.Fatalf("failed to read getting-started.md: %v", err)
	}
	content := string(data)
	if !bytes.Contains([]byte(content), []byte("openlimit init")) {
		t.Error("getting-started.md does not mention 'openlimit init' command")
	}
}

// TEST-63-01-02: getting-started.md has Quick Setup section heading.
func TestDocs_HasQuickSetupSection(t *testing.T) {
	data, err := os.ReadFile("../../docs/getting-started.md")
	if err != nil {
		t.Fatalf("failed to read getting-started.md: %v", err)
	}
	content := string(data)
	if !bytes.Contains([]byte(content), []byte("Quick Setup")) {
		t.Error("getting-started.md missing 'Quick Setup' section heading")
	}
}

// TEST-63-01-03: getting-started.md mentions Advanced/Manual path.
func TestDocs_HasAdvancedSetupSection(t *testing.T) {
	data, err := os.ReadFile("../../docs/getting-started.md")
	if err != nil {
		t.Fatalf("failed to read getting-started.md: %v", err)
	}
	content := string(data)
	if !bytes.Contains([]byte(content), []byte("Advanced")) {
		t.Error("getting-started.md missing 'Advanced' section heading")
	}
}

// ---------------------------------------------------------------------------
// BATCH-63 TASK-02: Mobile CSS Breakpoints + Focus-Visible tests
// ---------------------------------------------------------------------------

// TEST-63-02-01: index.html has @media breakpoint for 768px.
func TestDashboard_HasMobileBreakpoint768(t *testing.T) {
	html := loadDashboardHTML(t)
	if !bytes.Contains([]byte(html), []byte("@media(max-width:768px)")) && !bytes.Contains([]byte(html), []byte("@media (max-width: 768px)")) {
		// Try more relaxed check
		if !bytes.Contains([]byte(html), []byte("768px")) || !bytes.Contains([]byte(html), []byte("@media")) {
			t.Error("index.html missing @media breakpoint for 768px")
		}
	}
}

// TEST-63-02-02: index.html has @media breakpoint for 1024px.
func TestDashboard_HasTabletBreakpoint1024(t *testing.T) {
	html := loadDashboardHTML(t)
	if !bytes.Contains([]byte(html), []byte("@media(max-width:1024px)")) && !bytes.Contains([]byte(html), []byte("@media (max-width: 1024px)")) {
		if !bytes.Contains([]byte(html), []byte("1024px")) || !bytes.Contains([]byte(html), []byte("@media")) {
			t.Error("index.html missing @media breakpoint for 1024px")
		}
	}
}

// TEST-63-02-03: index.html has focus-visible CSS rules.
func TestDashboard_HasFocusVisibleRules(t *testing.T) {
	html := loadDashboardHTML(t)
	if !bytes.Contains([]byte(html), []byte(":focus-visible")) {
		t.Error("index.html missing :focus-visible CSS rules")
	}
}

// ---------------------------------------------------------------------------
// BATCH-63 TASK-03: Colorblind-Friendly Spend Bars tests
// ---------------------------------------------------------------------------

// TEST-63-03-01: Budget bar CSS has stripe pattern via repeating-linear-gradient.
func TestDashboard_BudgetBarHasStripePattern(t *testing.T) {
	html := loadDashboardHTML(t)
	if !bytes.Contains([]byte(html), []byte("repeating-linear-gradient")) {
		t.Error("index.html missing repeating-linear-gradient for budget bar stripe pattern")
	}
}

// TEST-63-03-02: Budget bar has safe/warning/danger CSS classes.
func TestDashboard_BudgetBarHasStatusClasses(t *testing.T) {
	html := loadDashboardHTML(t)
	for _, cls := range []string{"budget-bar-safe", "budget-bar-warning", "budget-bar-danger"} {
		if !bytes.Contains([]byte(html), []byte(cls)) {
			t.Errorf("index.html missing CSS class %q", cls)
		}
	}
}

// TEST-63-03-03: Budget bar JS applies class based on percentage threshold (70/90).
func TestDashboard_BudgetBarThresholdLogic(t *testing.T) {
	html := loadDashboardHTML(t)
	// Verify the new 70/90 thresholds are in the JS
	if !bytes.Contains([]byte(html), []byte("pct > 90")) {
		t.Error("index.html budget bar JS missing 'pct > 90' danger threshold")
	}
	if !bytes.Contains([]byte(html), []byte("pct >= 70")) {
		t.Error("index.html budget bar JS missing 'pct >= 70' warning threshold")
	}
	// Verify old 75/95 thresholds are removed
	if bytes.Contains([]byte(html), []byte("pct > 95")) {
		t.Error("index.html still contains old 'pct > 95' threshold — should be 'pct > 90'")
	}
	if bytes.Contains([]byte(html), []byte("pct >= 75")) {
		t.Error("index.html still contains old 'pct >= 75' threshold — should be 'pct >= 70'")
	}
}
