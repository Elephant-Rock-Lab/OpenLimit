package tracing

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewTracer_Disabled(t *testing.T) {
	tracer, err := NewTracer(false, "localhost:4317", "test", 0.1, slog.Default())
	if err != nil {
		t.Fatalf("NewTracer with enabled=false returned error: %v", err)
	}
	if tracer == nil {
		t.Fatal("expected non-nil tracer even when disabled")
	}
	// StartChatSpan should be no-op when disabled
	_, end := tracer.StartChatSpan(nil, "gpt-4", false)
	if end != nil {
		end()
	}
}

func TestTracer_HTTPMiddleware_NoOp(t *testing.T) {
	tracer := &Tracer{} // zero-value = no-op
	handler := tracer.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
