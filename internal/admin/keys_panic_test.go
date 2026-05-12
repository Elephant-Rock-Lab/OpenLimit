package admin

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"openlimit/internal/config"
)

// ---------------------------------------------------------------------------
// BATCH-58 / TASK-01: OnKeysChanged panic guard tests
// ---------------------------------------------------------------------------

// TEST-58-01-01: OnKeysChanged panic does not crash the gateway.
// A panicking OnKeysChanged callback must not prevent the HTTP response
// from being sent. All 5 call sites (create, revoke, update, patch, rotate)
// use the same wrapper, so testing createKey is sufficient.
func TestOnKeysChanged_Panic_DoesNotCrashGateway(t *testing.T) {
	cfg := config.Default()
	cfg.Admin.BearerToken = "test-admin-token"

	// Create a handler with a panicking OnKeysChanged
	db := setupDB(t)
	defer db.Close()

	h := NewHandler(db, cfg, nil, nil)
	h.OnKeysChanged = func() { panic("boom") }

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// Create a project first
	projReq := httptest.NewRequest(http.MethodPost, "/admin/projects", bytes.NewBufferString(`{"name":"panic-test-"}`))
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

	// Create a key — the OnKeysChanged panic should be recovered
	keyReq := httptest.NewRequest(http.MethodPost, "/admin/keys",
		bytes.NewBufferString(`{"project_id":"`+projectID+`","name":"panic-key"}`))
	keyReq.Header.Set("Authorization", "Bearer test-admin-token")
	keyW := httptest.NewRecorder()
	authed.ServeHTTP(keyW, keyReq)

	// Handler must still return 201 despite the panic
	if keyW.Code != http.StatusCreated {
		t.Fatalf("expected 201 despite panic, got %d; body: %s", keyW.Code, keyW.Body.String())
	}
}

// TEST-58-01-02: OnKeysChanged panic is logged at Error level.
func TestOnKeysChanged_Panic_LoggedAsError(t *testing.T) {
	// Capture log output by checking that the panic value appears in logs.
	// We use a custom slog handler to capture output.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	slog.SetDefault(logger)
	defer slog.SetDefault(slog.Default())

	cfg := config.Default()
	cfg.Admin.BearerToken = "test-admin-token"

	db := setupDB(t)
	defer db.Close()

	h := NewHandler(db, cfg, nil, nil)
	h.OnKeysChanged = func() { panic("test-panic-value") }

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	// Create project + key
	projReq := httptest.NewRequest(http.MethodPost, "/admin/projects", bytes.NewBufferString(`{"name":"panic-log-"}`))
	projReq.Header.Set("Authorization", "Bearer test-admin-token")
	projW := httptest.NewRecorder()
	authed.ServeHTTP(projW, projReq)

	var projResp map[string]any
	if err := json.Unmarshal(projW.Body.Bytes(), &projResp); err != nil {
		t.Fatalf("parse project: %v", err)
	}
	projectID, _ := projResp["id"].(string)

	keyReq := httptest.NewRequest(http.MethodPost, "/admin/keys",
		bytes.NewBufferString(`{"project_id":"`+projectID+`","name":"panic-log-key"}`))
	keyReq.Header.Set("Authorization", "Bearer test-admin-token")
	keyW := httptest.NewRecorder()
	authed.ServeHTTP(keyW, keyReq)

	// The key creation should succeed
	if keyW.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", keyW.Code, keyW.Body.String())
	}

	// The panic recovery should have logged "panic in OnKeysChanged"
	logOutput := buf.String()
	if !strings.Contains(logOutput, "panic in OnKeysChanged") {
		t.Errorf("expected log to contain 'panic in OnKeysChanged', got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "test-panic-value") {
		t.Errorf("expected log to contain panic value 'test-panic-value', got: %s", logOutput)
	}
}
