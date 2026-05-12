package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TEST-09-01-01: GET /admin/dashboard returns 200 with HTML content
// ---------------------------------------------------------------------------

func TestDashboard_ReturnsHTML(t *testing.T) {
	handler := DashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "<html") {
		t.Error("expected HTML content with <html tag")
	}
	if !strings.Contains(body, "OpenLimit") {
		t.Error("expected 'OpenLimit' in dashboard HTML")
	}
}

// ---------------------------------------------------------------------------
// TEST-09-01-02: Dashboard handler serves content (auth tested at integration level)
// ---------------------------------------------------------------------------

func TestDashboard_ContentTypeIsHTML(t *testing.T) {
	handler := DashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type, got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// TEST-09-01-03: embed.FS correctly serves the HTML file
// ---------------------------------------------------------------------------

func TestDashboard_EmbedFSContainsIndex(t *testing.T) {
	handler := DashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for root, got %d", w.Code)
	}
	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty body from embedded file system")
	}
}

// ---------------------------------------------------------------------------
// TEST-09-02-01: Dashboard HTML contains key UI sections
// ---------------------------------------------------------------------------
func TestDashboard_ContainsUISections(t *testing.T) {
	handler := DashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	sections := []string{"panel-overview", "panel-keys", "panel-usage", "panel-guardrails", "panel-mcp"}
	for _, s := range sections {
		if !strings.Contains(body, s) {
			t.Errorf("expected section %q in dashboard HTML", s)
		}
	}
}

// ---------------------------------------------------------------------------
// TEST-09-02-02: Dashboard HTML contains JS fetch calls to /admin/* endpoints
// ---------------------------------------------------------------------------
func TestDashboard_ContainsAdminAPICalls(t *testing.T) {
	handler := DashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	endpoints := []string{"/admin/projects", "/admin/keys", "/admin/usage", "/admin/usage/summary", "/health", "/admin/health/providers"}
	for _, ep := range endpoints {
		if !strings.Contains(body, ep) {
			t.Errorf("expected API call to %q in dashboard JS", ep)
		}
	}
}

// ---------------------------------------------------------------------------
// TEST-09-02-03: Dashboard HTML contains CSS styles
// ---------------------------------------------------------------------------
func TestDashboard_ContainsCSS(t *testing.T) {
	handler := DashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "<style>") {
		t.Error("expected <style> tag in dashboard HTML")
	}
	// Verify no external CSS/JS references
	if strings.Contains(body, "cdn.jsdelivr") || strings.Contains(body, "unpkg.com") {
		t.Error("dashboard should not reference external CDN resources")
	}
}
