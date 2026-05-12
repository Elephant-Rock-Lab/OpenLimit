package admin

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TEST-36-04-01: Dashboard HTML contains esc() on usage chart provider labels
// ---------------------------------------------------------------------------
func TestDashboard_UsageChartEscapesProviderLabels(t *testing.T) {
	handler := DashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()

	// Verify that the usage chart uses esc() on the period labels
	// Line: esc(p.slice(0,10))
	if !strings.Contains(body, "esc(p.slice(0,10))") {
		t.Error("expected esc(p.slice(0,10)) in usage chart period labels — potential XSS vulnerability")
	}
}

// ---------------------------------------------------------------------------
// TEST-36-04-02: No unescaped API-sourced innerHTML in dashboard
// ---------------------------------------------------------------------------
func TestDashboard_NoUnescapedAPIInnerHTML(t *testing.T) {
	handler := DashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()

	// All innerHTML assignments that interpolate API data should use esc()
	// Check that the top models chart uses esc()
	if !strings.Contains(body, "esc(m)") {
		t.Error("expected esc(m) in top models chart — model names must be escaped")
	}

	// Check that provider card names use esc()
	if !strings.Contains(body, "esc(p.name)") {
		t.Error("expected esc(p.name) in provider cards — provider names must be escaped")
	}

	// Check that key table uses esc()
	if !strings.Contains(body, "esc(k.name)") {
		t.Error("expected esc(k.name) in key table — key names must be escaped")
	}

	// Check that project table uses esc()
	if !strings.Contains(body, "esc(p.name)") {
		t.Error("expected esc(p.name) in project table — project names must be escaped")
	}

	// Check that log table uses esc()
	if !strings.Contains(body, "esc(r.model)") {
		t.Error("expected esc(r.model) in request log table — model names must be escaped")
	}
}
