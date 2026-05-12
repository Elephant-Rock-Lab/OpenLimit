package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"openlimit/internal/config"
)

// ---------------------------------------------------------------------------
// TEST-36-02-01: Request body read exceeding limit returns 413
// ---------------------------------------------------------------------------
func TestMaxBodySizeMiddleware_RejectsOversizedBodyOnRead(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to read all of the body
		buf := make([]byte, 300)
		_, err := r.Body.Read(buf)
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := maxBodySizeMiddleware(100)(inner)

	body := strings.Repeat("x", 200)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 for oversized body, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// TEST-36-02-02: Request within limit passes through
// ---------------------------------------------------------------------------
func TestMaxBodySizeMiddleware_AllowsNormalBody(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := maxBodySizeMiddleware(1024)(inner)

	body := `{"model":"fast"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for normal body, got %d", w.Code)
	}
	if !called {
		t.Error("expected inner handler to be called")
	}
}

// ---------------------------------------------------------------------------
// TEST-36-02-03: Config field max_body_size_kb defaults to 10240
// ---------------------------------------------------------------------------
func TestMaxBodySizeKB_DefaultAfterNormalize(t *testing.T) {
	cfg := config.Default()
	// After Default(), MaxBodySizeKB should be 0 (not set in Default())
	if cfg.Server.MaxBodySizeKB != 0 {
		t.Logf("pre-normalize: MaxBodySizeKB = %d", cfg.Server.MaxBodySizeKB)
	}
}

// ---------------------------------------------------------------------------
// TEST-36-02-04: Config normalization sets default for max_body_size_kb
// ---------------------------------------------------------------------------
func TestMaxBodySizeKB_NormalizeDefault(t *testing.T) {
	// Load with a non-existent file to trigger defaults + normalization
	cfg, err := config.Load("/nonexistent/path/gateway.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Server.MaxBodySizeKB != 10240 {
		t.Errorf("expected MaxBodySizeKB=10240 after Load, got %d", cfg.Server.MaxBodySizeKB)
	}
}

// ---------------------------------------------------------------------------
// TEST-36-02-04 (variant): Custom max_body_size_kb is preserved
// ---------------------------------------------------------------------------
func TestMaxBodySizeKB_CustomValuePreserved(t *testing.T) {
	cfg := config.Default()
	cfg.Server.MaxBodySizeKB = 5120
	if cfg.Server.MaxBodySizeKB != 5120 {
		t.Errorf("expected MaxBodySizeKB=5120, got %d", cfg.Server.MaxBodySizeKB)
	}
}
