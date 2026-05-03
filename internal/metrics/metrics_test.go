package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewCollector_Enabled(t *testing.T) {
	c := NewCollector(true)
	if !c.Enabled() {
		t.Fatal("expected enabled")
	}

	// Record some metrics
	c.RecordRequest("gpt-4", "openai", "200", false, 500*time.Millisecond)
	c.RecordCacheHit("gpt-4")
	c.RecordCacheMiss("gpt-3.5")
	c.RecordTokens("gpt-4", "openai", 100, 50)
	c.RecordCost("gpt-4", "openai", 0.01)
	c.RecordRateLimitRejection("gw-a1", "proj-1")
	c.RecordBudgetRejection("gw-b2", "proj-2")
	c.RecordRetry("openai", "gpt-4")
	c.RecordFallback("openai", "anthropic")
	c.ActiveRequestsInc()
	c.ActiveRequestsDec()
	c.RecordProviderCall("openai", "gpt-4", 300*time.Millisecond, "")
	c.RecordProviderCall("openai", "gpt-4", 100*time.Millisecond, "timeout")
	c.RecordGatewayError("rate_limit_exceeded", "direct")
	c.RecordGatewayError("rate_limit_exceeded", "direct")
	c.RecordGatewayError("budget_exceeded", "admin")

	// Check metrics endpoint
	handler := c.MetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	for _, metric := range []string{
		"gateway_requests_total",
		"gateway_request_duration_seconds",
		"gateway_provider_call_duration_seconds",
		"gateway_provider_errors_total",
		"gateway_tokens_total",
		"gateway_cost_dollars_total",
		"gateway_cache_hits_total",
		"gateway_cache_misses_total",
		"gateway_rate_limit_rejections_total",
		"gateway_budget_rejections_total",
		"gateway_active_requests",
		"gateway_retries_total",
		"gateway_fallbacks_total",
		"gateway_errors_total",
	} {
		if !strings.Contains(body, metric) {
			t.Errorf("expected metric %q in output", metric)
		}
	}
}

func TestNewCollector_Disabled(t *testing.T) {
	c := NewCollector(false)
	if c.Enabled() {
		t.Fatal("expected disabled")
	}

	// All methods should be no-ops (no panic)
	c.RecordRequest("gpt-4", "openai", "200", false, 500*time.Millisecond)
	c.RecordCacheHit("gpt-4")
	c.RecordCacheMiss("gpt-4")
	c.RecordTokens("gpt-4", "openai", 100, 50)
	c.RecordCost("gpt-4", "openai", 0.01)
	c.RecordRateLimitRejection("gw-a1", "proj-1")
	c.RecordBudgetRejection("gw-b2", "proj-2")
	c.RecordRetry("openai", "gpt-4")
	c.RecordFallback("openai", "anthropic")
	c.ActiveRequestsInc()
	c.ActiveRequestsDec()
	c.RecordProviderCall("openai", "gpt-4", 300*time.Millisecond, "error")
	c.RecordGatewayError("rate_limit_exceeded", "direct")

	// Metrics endpoint should return 404
	handler := c.MetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
