package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// BATCH-40 TASK-04: statusRecorder Hijack tests
// ---------------------------------------------------------------------------

func TestStatusRecorderImplementsHijacker(t *testing.T) {
	// TEST-40-04-01: statusRecorder implements http.Hijacker
	inner := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}

	var _ http.Hijacker = rec // compile-time check
	_ = rec
}

func TestStatusRecorderHijackDelegates(t *testing.T) {
	// TEST-40-04-02: Hijack delegates to underlying ResponseWriter
	// httptest.NewRecorder does NOT implement Hijacker, so we test the error path
	inner := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}

	_, _, err := rec.Hijack()
	if err == nil {
		t.Error("expected error when underlying ResponseWriter doesn't support Hijack")
	}
}
