package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"openlimit/internal/config"
	"openlimit/internal/metrics"
)

// ---------------------------------------------------------------------------
// BATCH-50 TASK-01: Guardrail Stats Endpoint Tests
// All tests use a real metrics.Collector (no mocks).
// ---------------------------------------------------------------------------

// newTestCollector creates a Collector with Prometheus disabled.
func newTestCollector() *metrics.Collector {
	return metrics.NewCollector(false)
}

// newHandlerWithMetrics creates a Handler with a real Collector wired in.
func newHandlerWithMetrics(t *testing.T) (*Handler, *metrics.Collector) {
	t.Helper()
	c := newTestCollector()
	h := NewHandler(nil, config.Default(), nil, c)
	return h, c
}

// TEST-50-01-01: Stats returns 200 with JSON structure (total_blocks field)
func TestGuardrailStats_Returns200WithJSON(t *testing.T) {
	h, _ := newHandlerWithMetrics(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/guardrails/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if _, ok := resp["total_blocks"]; !ok {
		t.Error("response missing total_blocks field")
	}
	if _, ok := resp["total_redactions"]; !ok {
		t.Error("response missing total_redactions field")
	}
	if _, ok := resp["total_passes"]; !ok {
		t.Error("response missing total_passes field")
	}
	if _, ok := resp["total_requests"]; !ok {
		t.Error("response missing total_requests field")
	}
	if _, ok := resp["block_rate_pct"]; !ok {
		t.Error("response missing block_rate_pct field")
	}
	if _, ok := resp["redact_rate_pct"]; !ok {
		t.Error("response missing redact_rate_pct field")
	}
	if _, ok := resp["stages"]; !ok {
		t.Error("response missing stages field")
	}
}

// TEST-50-01-02: Stats works with no requests (all counters zero)
func TestGuardrailStats_ZeroCounters(t *testing.T) {
	c := newTestCollector()
	stats := c.GetGuardrailStats()

	if stats.TotalBlocks != 0 {
		t.Errorf("TotalBlocks = %d, want 0", stats.TotalBlocks)
	}
	if stats.TotalRedactions != 0 {
		t.Errorf("TotalRedactions = %d, want 0", stats.TotalRedactions)
	}
	if stats.TotalPasses != 0 {
		t.Errorf("TotalPasses = %d, want 0", stats.TotalPasses)
	}
	if stats.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d, want 0", stats.TotalRequests)
	}
	if stats.BlockRatePct != 0 {
		t.Errorf("BlockRatePct = %f, want 0", stats.BlockRatePct)
	}
	if stats.RedactRatePct != 0 {
		t.Errorf("RedactRatePct = %f, want 0", stats.RedactRatePct)
	}
	if len(stats.Stages) != 0 {
		t.Errorf("Stages length = %d, want 0", len(stats.Stages))
	}
}

// TEST-50-01-03: Block increments counter
func TestGuardrailStats_BlockIncrementsCounter(t *testing.T) {
	c := metrics.NewCollector(false)

	c.RecordGuardrailBlock("pii", "input", "gpt-4")
	stats := c.GetGuardrailStats()

	if stats.TotalBlocks != 1 {
		t.Errorf("TotalBlocks = %d, want 1", stats.TotalBlocks)
	}
	if stats.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", stats.TotalRequests)
	}
}

// TEST-50-01-04: Redaction increments counter
func TestGuardrailStats_RedactionIncrementsCounter(t *testing.T) {
	c := metrics.NewCollector(false)

	c.RecordGuardrailRedaction("pii", "input", "gpt-4")
	stats := c.GetGuardrailStats()

	if stats.TotalRedactions != 1 {
		t.Errorf("TotalRedactions = %d, want 1", stats.TotalRedactions)
	}
	if stats.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", stats.TotalRequests)
	}
}

// TEST-50-01-05: Pass increments counter (call RecordGuardrailPass directly)
func TestGuardrailStats_PassIncrementsCounter(t *testing.T) {
	c := newTestCollector()

	c.RecordGuardrailPass()
	stats := c.GetGuardrailStats()

	if stats.TotalPasses != 1 {
		t.Errorf("TotalPasses = %d, want 1", stats.TotalPasses)
	}
	if stats.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", stats.TotalRequests)
	}
}

// TEST-50-01-06: Block rate computed correctly (10 blocks / 100 requests = 10%)
func TestGuardrailStats_BlockRateComputed(t *testing.T) {
	c := metrics.NewCollector(false)

	// 10 blocks
	for i := 0; i < 10; i++ {
		c.RecordGuardrailBlock("keyword", "input", "gpt-4")
	}
	// 90 passes
	for i := 0; i < 90; i++ {
		c.RecordGuardrailPass()
	}

	stats := c.GetGuardrailStats()

	if stats.TotalBlocks != 10 {
		t.Errorf("TotalBlocks = %d, want 10", stats.TotalBlocks)
	}
	if stats.TotalRequests != 100 {
		t.Errorf("TotalRequests = %d, want 100", stats.TotalRequests)
	}
	// block_rate_pct = 10/100 * 100 = 10.0
	if stats.BlockRatePct != 10.0 {
		t.Errorf("BlockRatePct = %f, want 10.0", stats.BlockRatePct)
	}
}

// TEST-50-01-07: Stats endpoint requires auth (returns 401)
func TestGuardrailStats_RequiresAuth(t *testing.T) {
	h, _ := newHandlerWithMetrics(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Request without auth
	req := httptest.NewRequest(http.MethodGet, "/admin/guardrails/stats", nil)
	w := httptest.NewRecorder()

	// Use BearerAuth middleware wrapping the mux
	adminToken := "test-admin-token"
	protected := BearerAuth(adminToken, nil, nil, nil, mux)
	protected.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TEST-50-01-08: Stats includes per-stage data (blocks+redactions, no passes)
func TestGuardrailStats_PerStageData(t *testing.T) {
	c := metrics.NewCollector(false)

	c.RecordGuardrailBlock("pii", "input", "gpt-4")
	c.RecordGuardrailBlock("pii", "input", "gpt-4")
	c.RecordGuardrailRedaction("pii", "input", "gpt-4")

	stats := c.GetGuardrailStats()

	if len(stats.Stages) != 1 {
		t.Fatalf("Stages length = %d, want 1", len(stats.Stages))
	}

	s := stats.Stages[0]
	if s.Name != "pii" {
		t.Errorf("Stage name = %q, want %q", s.Name, "pii")
	}
	if s.Blocks != 2 {
		t.Errorf("Stage blocks = %d, want 2", s.Blocks)
	}
	if s.Redactions != 1 {
		t.Errorf("Stage redactions = %d, want 1", s.Redactions)
	}
	if s.Direction != "input" {
		t.Errorf("Stage direction = %q, want %q", s.Direction, "input")
	}
}

// TEST-50-01-09: Direction tracked separately
func TestGuardrailStats_DirectionTrackedSeparately(t *testing.T) {
	c := metrics.NewCollector(false)

	c.RecordGuardrailBlock("pii", "input", "gpt-4")
	c.RecordGuardrailBlock("pii", "output", "gpt-4")

	stats := c.GetGuardrailStats()

	if len(stats.Stages) != 2 {
		t.Fatalf("Stages length = %d, want 2", len(stats.Stages))
	}

	// Stages should be sorted by name, then direction
	s1 := stats.Stages[0]
	s2 := stats.Stages[1]
	if s1.Direction != "input" {
		t.Errorf("first stage direction = %q, want input", s1.Direction)
	}
	if s2.Direction != "output" {
		t.Errorf("second stage direction = %q, want output", s2.Direction)
	}
}

// TEST-50-01-10: Multiple stages accumulate independently
func TestGuardrailStats_MultipleStagesAccumulate(t *testing.T) {
	c := metrics.NewCollector(false)

	c.RecordGuardrailBlock("pii", "input", "gpt-4")
	c.RecordGuardrailBlock("keyword", "input", "gpt-4")
	c.RecordGuardrailRedaction("pii", "input", "gpt-4")
	c.RecordGuardrailRedaction("keyword", "output", "gpt-4")
	c.RecordGuardrailRedaction("keyword", "output", "gpt-4")

	stats := c.GetGuardrailStats()

	if stats.TotalBlocks != 2 {
		t.Errorf("TotalBlocks = %d, want 2", stats.TotalBlocks)
	}
	if stats.TotalRedactions != 3 {
		t.Errorf("TotalRedactions = %d, want 3", stats.TotalRedactions)
	}

	if len(stats.Stages) != 3 {
		t.Fatalf("Stages length = %d, want 3", len(stats.Stages))
	}

	// Build a map for easier verification
	stageMap := map[string]metrics.GuardrailStageStats{}
	for _, s := range stats.Stages {
		stageMap[s.Name+"|"+s.Direction] = s
	}

	if s, ok := stageMap["pii|input"]; !ok {
		t.Error("missing pii|input stage")
	} else {
		if s.Blocks != 1 {
			t.Errorf("pii|input blocks = %d, want 1", s.Blocks)
		}
		if s.Redactions != 1 {
			t.Errorf("pii|input redactions = %d, want 1", s.Redactions)
		}
	}

	if s, ok := stageMap["keyword|input"]; !ok {
		t.Error("missing keyword|input stage")
	} else {
		if s.Blocks != 1 {
			t.Errorf("keyword|input blocks = %d, want 1", s.Blocks)
		}
		if s.Redactions != 0 {
			t.Errorf("keyword|input redactions = %d, want 0", s.Redactions)
		}
	}

	if s, ok := stageMap["keyword|output"]; !ok {
		t.Error("missing keyword|output stage")
	} else {
		if s.Blocks != 0 {
			t.Errorf("keyword|output blocks = %d, want 0", s.Blocks)
		}
		if s.Redactions != 2 {
			t.Errorf("keyword|output redactions = %d, want 2", s.Redactions)
		}
	}
}
