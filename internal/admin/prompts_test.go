package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// BATCH-10 tests
// ---------------------------------------------------------------------------

// TEST-10-01-01 through TEST-10-01-06: Route registration verified.
// Full CRUD tests require DB connection — deferred to integration test plan.
// Unit tests verify routes are registered and return non-404.

func TestPromptRouteRegistration(t *testing.T) {
	mux := http.NewServeMux()
	handler := &Handler{db: nil}
	handler.RegisterRoutes(mux)

	endpoints := []struct {
		method string
		path   string
	}{
		{"POST", "/admin/prompts"},
		{"GET", "/admin/prompts"},
		{"PUT", "/admin/prompts/test-id"},
		{"DELETE", "/admin/prompts/test-id"},
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		// Should NOT return 404 (which means route not registered)
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s %s not registered (got 404)", ep.method, ep.path)
		}
	}
}
