package health

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/routing"
)

// TEST-7B-03-01: RecordSuccess then IsHealthy returns true within 30s window.
func TestRecordSuccess_IsHealthy_True(t *testing.T) {
	tracker := NewTracker(30 * time.Second)

	tracker.RecordSuccess("openai", "gpt-4", "us-east")

	if !tracker.IsHealthy("openai", "gpt-4", "us-east") {
		t.Error("expected IsHealthy=true after RecordSuccess within window")
	}
}

// TEST-7B-03-02: RecordFailure → IsHealthy false; RecordSuccess resets
// ConsecutiveFailures to 0; IsHealthy true again.
func TestRecordFailure_ResetsOnSuccess(t *testing.T) {
	tracker := NewTracker(30 * time.Second)

	// Record a failure — model should be unhealthy.
	tracker.RecordFailure("openai", "gpt-4", "us-east")
	if tracker.IsHealthy("openai", "gpt-4", "us-east") {
		t.Error("expected IsHealthy=false after RecordFailure")
	}

	// Verify ConsecutiveFailures was incremented.
	key := healthKey("openai", "gpt-4", "us-east")
	tracker.mu.RLock()
	h := tracker.models[key]
	tracker.mu.RUnlock()
	if h.ConsecutiveFailures != 1 {
		t.Errorf("expected ConsecutiveFailures=1, got %d", h.ConsecutiveFailures)
	}

	// Record success — should reset ConsecutiveFailures to 0 (AC-03-05).
	tracker.RecordSuccess("openai", "gpt-4", "us-east")
	if !tracker.IsHealthy("openai", "gpt-4", "us-east") {
		t.Error("expected IsHealthy=true after RecordSuccess resets ConsecutiveFailures")
	}

	tracker.mu.RLock()
	h = tracker.models[key]
	tracker.mu.RUnlock()
	if h.ConsecutiveFailures != 0 {
		t.Errorf("expected ConsecutiveFailures=0 after RecordSuccess, got %d", h.ConsecutiveFailures)
	}
}

// TEST-7B-03-03: Router.Plan reorders targets (healthy before unhealthy).
func TestRouter_Plan_ReordersHealthyFirst(t *testing.T) {
	models := map[string]config.ModelConfig{
		"gpt-4": {
			Routes: []config.ModelRoute{
				{Provider: "openai", Model: "gpt-4", Weight: 1},
			},
			Fallbacks: []config.ModelRoute{
				{Provider: "anthropic", Model: "claude-3", Weight: 1},
			},
		},
	}
	providers := map[string]config.ProviderConfig{
		"openai":    {Type: "openai"},
		"anthropic": {Type: "anthropic"},
	}

	r := routing.New(models, providers, config.RoutingConfig{}, nil)

	// Get the plan first to see the original order.
	plan, err := r.Plan("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(plan.Targets))
	}

	// Mark the primary (openai/gpt-4) as unhealthy.
	tracker := NewTracker(30 * time.Second)
	tracker.RecordFailure("openai", "gpt-4", "")

	// Wire the tracker into the router.
	r.SetHealthTracker(tracker)

	// Plan should now reorder: anthropic (healthy) before openai (unhealthy).
	plan2, err := r.Plan("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan2.Targets) != 2 {
		t.Fatalf("expected 2 targets after reorder, got %d", len(plan2.Targets))
	}

	// The healthy target (anthropic) should come first.
	if plan2.Targets[0].Provider != "anthropic" {
		t.Errorf("expected healthy target (anthropic) first, got %q", plan2.Targets[0].Provider)
	}
	if plan2.Targets[1].Provider != "openai" {
		t.Errorf("expected unhealthy target (openai) second, got %q", plan2.Targets[1].Provider)
	}
}

// TEST-7B-03-04: All targets unhealthy → Router.Plan returns all targets (best-effort).
func TestRouter_Plan_AllUnhealthy_BestEffort(t *testing.T) {
	models := map[string]config.ModelConfig{
		"gpt-4": {
			Routes: []config.ModelRoute{
				{Provider: "openai", Model: "gpt-4", Weight: 1},
			},
			Fallbacks: []config.ModelRoute{
				{Provider: "anthropic", Model: "claude-3", Weight: 1},
			},
		},
	}
	providers := map[string]config.ProviderConfig{
		"openai":    {Type: "openai"},
		"anthropic": {Type: "anthropic"},
	}

	r := routing.New(models, providers, config.RoutingConfig{}, nil)

	// Mark ALL targets as unhealthy.
	tracker := NewTracker(30 * time.Second)
	tracker.RecordFailure("openai", "gpt-4", "")
	tracker.RecordFailure("anthropic", "claude-3", "")

	r.SetHealthTracker(tracker)

	plan, err := r.Plan("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// Best-effort: all targets must still be returned.
	if len(plan.Targets) != 2 {
		t.Fatalf("expected 2 targets (best-effort), got %d", len(plan.Targets))
	}
}

// ---------------------------------------------------------------------------
// BATCH-12 tests
// ---------------------------------------------------------------------------

// TEST-12-01-01: GetAll returns all recorded health entries
func TestGetAll_ReturnsAllEntries(t *testing.T) {
	tracker := NewTracker(30 * time.Second)
	tracker.RecordSuccess("openai", "gpt-4", "us-east")
	tracker.RecordFailure("anthropic", "claude-3", "eu")

	all := tracker.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}

	found := map[string]bool{}
	for _, e := range all {
		found[e.Provider+"/"+e.Model] = true
	}
	if !found["openai/gpt-4"] {
		t.Error("expected openai/gpt-4 entry")
	}
	if !found["anthropic/claude-3"] {
		t.Error("expected anthropic/claude-3 entry")
	}
}

// TEST-12-01-02: Admin provider health endpoint returns JSON array
func TestAdminProviderHealth_ReturnsJSON(t *testing.T) {
	tracker := NewTracker(30 * time.Second)
	tracker.RecordSuccess("openai", "gpt-4", "us-east")
	tracker.RecordFailure("anthropic", "claude-3", "eu")

	handler := AdminProviderHealth(tracker)

	req := httptest.NewRequest("GET", "/admin/health/providers", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "openai") || !strings.Contains(body, "anthropic") {
		t.Errorf("expected both providers in response: %s", body)
	}
}

// TEST-12-01-03: Admin model health endpoint returns JSON array
func TestAdminModelHealth_ReturnsJSON(t *testing.T) {
	tracker := NewTracker(30 * time.Second)
	tracker.RecordSuccess("openai", "gpt-4", "us-east")

	handler := AdminModelHealth(tracker)

	req := httptest.NewRequest("GET", "/admin/health/models", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "gpt-4") {
		t.Errorf("expected model in response: %s", body)
	}
}
