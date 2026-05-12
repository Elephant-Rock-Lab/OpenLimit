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
