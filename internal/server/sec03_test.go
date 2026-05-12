package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// BATCH-59 / TASK-01: Admin body limit tests (white-box for maxBodySizeMiddleware)
// ---------------------------------------------------------------------------

// TEST-59-01-01: Admin body limit middleware rejects body > 64KB with 413.
func TestAdminBodyLimit_RejectsOversized(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate admin handler reading the body (triggers MaxBytesReader)
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// Same wrapping as in server.go: maxBodySizeMiddleware(65536)(adminRoutes)
	handler := maxBodySizeMiddleware(65536)(inner)

	// 65KB body — exceeds the 64KB admin limit
	body := make([]byte, 65*1024)
	for i := range body {
		body[i] = 'A'
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d: %s", w.Code, w.Body.String())
	}
}

// TEST-59-01-02: Admin body limit middleware accepts body < 64KB.
func TestAdminBodyLimit_AcceptsUnderLimit(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate admin handler reading the body
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := maxBodySizeMiddleware(65536)(inner)

	// 10KB body — well under the 64KB admin limit
	body := make([]byte, 10*1024)
	for i := range body {
		body[i] = 'A'
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for 10KB body, got %d: %s", w.Code, w.Body.String())
	}
}
