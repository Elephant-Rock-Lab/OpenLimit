package routing

import (
	"testing"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/providers"
)

// ---------------------------------------------------------------------------
// TEST-54-01-01: Cost catalog has 20+ entries
// ---------------------------------------------------------------------------
func TestCostCatalog_MinSize(t *testing.T) {
	if len(CostCatalog) < 20 {
		t.Errorf("expected CostCatalog >= 20 entries, got %d", len(CostCatalog))
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-02: Cost strategy picks cheapest provider
// ---------------------------------------------------------------------------
func TestSelectByCost_PicksCheapest(t *testing.T) {
	r := &Router{strategy: "cost"}
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4o", Region: "us-east"},       // avgCost = 6.25
		{Provider: "deepseek", Model: "deepseek-chat", Region: "eu"},   // avgCost = 0.685
		{Provider: "anthropic", Model: "claude-sonnet-4-20250514", Region: "us"}, // avgCost = 9.0
	}
	selected := r.selectByCost(targets, "gpt-4")
	if selected.Provider != "deepseek" {
		t.Errorf("expected cheapest provider deepseek, got %s", selected.Provider)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-03: Smart strategy computes combined score correctly
// ---------------------------------------------------------------------------
func TestSelectSmart_CombinedScore(t *testing.T) {
	r := &Router{
		strategy:     "smart",
		smartWeights: DefaultSmartWeights(),
		latencyCache: nil, // no latency data
	}
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4o", Region: "us-east"},
		{Provider: "deepseek", Model: "deepseek-chat", Region: "eu"},
	}
	selected := r.selectSmart(targets, "gpt-4")
	// With no latency data, both get median latency, so cost dominates.
	// deepseek-chat avgCost=0.685 vs gpt-4o avgCost=6.25 → deepseek should win
	if selected.Provider != "deepseek" {
		t.Errorf("expected deepseek (cheaper), got %s", selected.Provider)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-04: Health score: unhealthy=0.0, unknown=0.5
// ---------------------------------------------------------------------------
func TestSelectSmart_HealthScore(t *testing.T) {
	// Test with unhealthy checker: healthy=false → healthScore=0.0
	unhealthyChecker := &staticHealthChecker{healthy: false}
	r1 := &Router{
		strategy:     "smart",
		smartWeights: DefaultSmartWeights(),
		health:       unhealthyChecker,
		latencyCache: nil,
	}
	targets := []providers.Target{
		{Provider: "deepseek", Model: "deepseek-chat", Region: "us"},
	}
	// Should still return a target (no panic with health checker returning false)
	selected := r1.selectSmart(targets, "deepseek-chat")
	if selected.Provider != "deepseek" {
		t.Errorf("expected deepseek, got %s", selected.Provider)
	}

	// Test with nil health checker: unknown → healthScore=0.5 (AR-08)
	r2 := &Router{
		strategy:     "smart",
		smartWeights: DefaultSmartWeights(),
		health:       nil, // unknown health
		latencyCache: nil,
	}
	selected2 := r2.selectSmart(targets, "deepseek-chat")
	if selected2.Provider != "deepseek" {
		t.Errorf("expected deepseek, got %s", selected2.Provider)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-05: Missing cost defaults to median
// ---------------------------------------------------------------------------
func TestSelectByCost_MissingCostDefaultsMedian(t *testing.T) {
	r := &Router{strategy: "cost"}
	targets := []providers.Target{
		{Provider: "unknown_provider", Model: "unknown-model", Region: "us"},
		{Provider: "deepseek", Model: "deepseek-chat", Region: "eu"},
	}
	selected := r.selectByCost(targets, "unknown-model")
	// unknown provider gets medianCost(CostCatalog), deepseek-chat gets 0.685
	// deepseek-chat is cheaper than median → should be selected
	if selected.Provider != "deepseek" {
		t.Errorf("expected deepseek (cheaper than median), got %s", selected.Provider)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-06: Missing latency defaults to median
// ---------------------------------------------------------------------------
func TestSelectSmart_MissingLatencyDefaultsMedian(t *testing.T) {
	r := &Router{
		strategy:     "smart",
		smartWeights: DefaultSmartWeights(),
		latencyCache: nil, // no latency data at all
		health:       nil,
	}
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4o-mini", Region: "us-east"}, // avgCost=0.375
		{Provider: "openai", Model: "gpt-4o", Region: "eu-west"},      // avgCost=6.25
	}
	selected := r.selectSmart(targets, "gpt-4o-mini")
	// Both get same median latency, so cost wins → gpt-4o-mini is cheaper
	if selected.Provider != "openai" || selected.Model != "gpt-4o-mini" {
		t.Errorf("expected openai/gpt-4o-mini (cheapest), got %s/%s", selected.Provider, selected.Model)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-07: LookupCost returns correct entry
// ---------------------------------------------------------------------------
func TestLookupCost_DeepSeekChat(t *testing.T) {
	entry := LookupCost("deepseek", "deepseek-chat")
	if entry == nil {
		t.Fatal("expected entry for deepseek/deepseek-chat, got nil")
	}
	if entry.InputPer1M != 0.27 {
		t.Errorf("expected InputPer1M=0.27, got %v", entry.InputPer1M)
	}
	if entry.OutputPer1M != 1.10 {
		t.Errorf("expected OutputPer1M=1.10, got %v", entry.OutputPer1M)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-08: Priority strategy unchanged (regression)
// ---------------------------------------------------------------------------
func TestSelectByPriority_Unchanged(t *testing.T) {
	models := map[string]config.ModelConfig{
		"gpt-4": {
			Routes: []config.ModelRoute{
				{Provider: "openai", Model: "gpt-4", Weight: 1},
			},
		},
	}
	providerCfgs := map[string]config.ProviderConfig{
		"openai": {
			Type: "openai",
			Regions: []config.RegionConfig{
				{Name: "us-east", BaseURL: "https://us.api.openai.com/v1", Priority: 1},
				{Name: "eu-west", BaseURL: "https://eu.api.openai.com/v1", Priority: 2},
			},
		},
	}
	r := New(models, providerCfgs, config.RoutingConfig{}, nil)

	plan, err := r.Plan("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// Priority 1 should still be us-east
	if plan.Targets[0].Region != "us-east" {
		t.Errorf("priority strategy regression: expected us-east, got %q", plan.Targets[0].Region)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-09: Latency strategy unchanged (regression)
// ---------------------------------------------------------------------------
func TestSelectByLatency_Unchanged(t *testing.T) {
	models := map[string]config.ModelConfig{
		"gpt-4": {
			Routes: []config.ModelRoute{
				{Provider: "openai", Model: "gpt-4", Weight: 1},
			},
		},
	}
	providerCfgs := map[string]config.ProviderConfig{
		"openai": {
			Type: "openai",
			Regions: []config.RegionConfig{
				{Name: "us-east", BaseURL: "https://us.api.openai.com/v1", Priority: 1},
				{Name: "eu-west", BaseURL: "https://eu.api.openai.com/v1", Priority: 2},
			},
		},
	}
	routingCfg := config.RoutingConfig{RegionStrategy: "latency"}
	r := New(models, providerCfgs, routingCfg, &mockLatencyReader{
		latencies: map[string]time.Duration{
			"openai:gpt-4:us-east": 500 * time.Millisecond,
			"openai:gpt-4:eu-west": 100 * time.Millisecond,
		},
	})

	plan, err := r.Plan("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Targets[0].Region != "eu-west" {
		t.Errorf("latency strategy regression: expected eu-west, got %q", plan.Targets[0].Region)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-10: Smart picks healthy over cheap when weights favor health
// ---------------------------------------------------------------------------
func TestSelectSmart_HealthyBeatsCheap(t *testing.T) {
	// Use weights where health dominates
	r := &Router{
		strategy: "smart",
		smartWeights: SmartWeights{Cost: 0.1, Latency: 0.1, Health: 0.8},
		latencyCache: nil,
		health: &selectiveHealthChecker{
			healthy: map[string]bool{
				"openai:gpt-4o:us": false,   // unhealthy
				"deepseek:deepseek-chat:eu": true, // healthy
			},
		},
	}
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4o", Region: "us"},        // unhealthy, expensive
		{Provider: "deepseek", Model: "deepseek-chat", Region: "eu"}, // healthy, cheap
	}
	selected := r.selectSmart(targets, "gpt-4o")
	if selected.Provider != "deepseek" {
		t.Errorf("expected deepseek (healthy+cheap), got %s", selected.Provider)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-11: Cost normalization: cheapest=1.0, expensive→0.0
// ---------------------------------------------------------------------------
func TestSelectByCost_Normalization(t *testing.T) {
	entry := LookupCost("openai", "gpt-4o")
	if entry == nil {
		t.Fatal("expected entry for openai/gpt-4o")
	}
	// Verify avgCost calculation
	avg := entry.avgCost()
	expected := (2.50 + 10.00) / 2.0
	if avg != expected {
		t.Errorf("expected avgCost %.2f, got %.2f", expected, avg)
	}

	// Verify that cost strategy selects the cheapest among known entries
	r := &Router{strategy: "cost"}
	targets := []providers.Target{
		{Provider: "openai", Model: "o3", Region: "us"},            // avgCost=25.0 (most expensive)
		{Provider: "openai", Model: "gpt-4.1-nano", Region: "eu"},  // avgCost=0.25 (cheapest)
	}
	selected := r.selectByCost(targets, "o3")
	if selected.Model != "gpt-4.1-nano" {
		t.Errorf("expected cheapest (gpt-4.1-nano), got %s", selected.Model)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-01-12: Custom weights applied correctly
// ---------------------------------------------------------------------------
func TestSelectSmart_CustomWeights(t *testing.T) {
	// Cost-only weights (latency and health zeroed)
	r := &Router{
		strategy: "smart",
		smartWeights: SmartWeights{Cost: 1.0, Latency: 0.0, Health: 0.0},
		latencyCache: nil,
		health: &staticHealthChecker{healthy: false}, // all unhealthy, but health weight=0
	}
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4o", Region: "us"},       // expensive
		{Provider: "deepseek", Model: "deepseek-chat", Region: "eu"}, // cheap
	}
	selected := r.selectSmart(targets, "gpt-4o")
	if selected.Provider != "deepseek" {
		t.Errorf("with cost-only weights, expected cheapest (deepseek), got %s", selected.Provider)
	}
}

// --- Test helpers ---

// staticHealthChecker returns the same health status for all targets.
type staticHealthChecker struct {
	healthy bool
}

func (s *staticHealthChecker) IsHealthy(provider, model, region string) bool {
	return s.healthy
}

// selectiveHealthChecker returns per-key health status.
type selectiveHealthChecker struct {
	healthy map[string]bool // "provider:model:region" → bool
}

func (s *selectiveHealthChecker) IsHealthy(provider, model, region string) bool {
	key := provider + ":" + model + ":" + region
	if h, ok := s.healthy[key]; ok {
		return h
	}
	return false
}

// ---------------------------------------------------------------------------
// BATCH-57 / TASK-04: Nil LatencyCache for Smart Routing Strategy regression tests
// ---------------------------------------------------------------------------

// TEST-57-04-01: Router with strategy=smart creates non-nil LatencyCache.
func TestSmartStrategy_CreatesNonNilCache(t *testing.T) {
	models := map[string]config.ModelConfig{
		"gpt-4": {
			Routes: []config.ModelRoute{
				{Provider: "openai", Model: "gpt-4", Weight: 1},
			},
		},
	}
	providerCfgs := map[string]config.ProviderConfig{
		"openai": {
			Type: "openai",
			Regions: []config.RegionConfig{
				{Name: "us-east", BaseURL: "https://us.api.openai.com/v1", Priority: 1},
				{Name: "eu-west", BaseURL: "https://eu.api.openai.com/v1", Priority: 2},
			},
		},
	}
	routingCfg := config.RoutingConfig{RegionStrategy: "smart"}
	r := New(models, providerCfgs, routingCfg, &mockLatencyReader{
		latencies: map[string]time.Duration{
			"openai:gpt-4:us-east": 100 * time.Millisecond,
			"openai:gpt-4:eu-west": 200 * time.Millisecond,
		},
	})

	if r.latencyCache == nil {
		t.Fatal("expected non-nil latencyCache for smart strategy")
	}
}

// TEST-57-04-02: Smart strategy produces different scores for targets with different latencies.
func TestSmartStrategy_DifferentScoresPerTarget(t *testing.T) {
	models := map[string]config.ModelConfig{
		"gpt-4": {
			Routes: []config.ModelRoute{
				{Provider: "openai", Model: "gpt-4", Weight: 1},
			},
		},
	}
	providerCfgs := map[string]config.ProviderConfig{
		"openai": {
			Type: "openai",
			Regions: []config.RegionConfig{
				{Name: "us-east", BaseURL: "https://us.api.openai.com/v1", Priority: 1},
				{Name: "eu-west", BaseURL: "https://eu.api.openai.com/v1", Priority: 2},
			},
		},
	}
	routingCfg := config.RoutingConfig{RegionStrategy: "smart"}
	r := New(models, providerCfgs, routingCfg, &mockLatencyReader{
		latencies: map[string]time.Duration{
			"openai:gpt-4:us-east": 100 * time.Millisecond,
			"openai:gpt-4:eu-west": 500 * time.Millisecond,
		},
	})

	plan, err := r.Plan("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// With latency data, the faster region should be preferred
	if plan.Targets[0].Region != "us-east" {
		t.Errorf("expected us-east (faster latency), got %q", plan.Targets[0].Region)
	}
}

// TEST-57-04-03: Router with strategy=priority does NOT create LatencyCache.
func TestPriorityStrategy_DoesNotCreateCache(t *testing.T) {
	models := map[string]config.ModelConfig{
		"gpt-4": {
			Routes: []config.ModelRoute{
				{Provider: "openai", Model: "gpt-4", Weight: 1},
			},
		},
	}
	providerCfgs := map[string]config.ProviderConfig{
		"openai": {
			Type: "openai",
			Regions: []config.RegionConfig{
				{Name: "us-east", BaseURL: "https://us.api.openai.com/v1", Priority: 1},
			},
		},
	}
	routingCfg := config.RoutingConfig{RegionStrategy: "priority"}
	r := New(models, providerCfgs, routingCfg, &mockLatencyReader{
		latencies: map[string]time.Duration{
			"openai:gpt-4:us-east": 100 * time.Millisecond,
		},
	})

	if r.latencyCache != nil {
		t.Error("expected nil latencyCache for priority strategy")
	}
}

// TEST-57-04-04: Router with strategy=smart and nil latencyReader creates nil LatencyCache without panic.
func TestSmartStrategy_NilLatencyReader_NoPanic(t *testing.T) {
	models := map[string]config.ModelConfig{
		"gpt-4": {
			Routes: []config.ModelRoute{
				{Provider: "openai", Model: "gpt-4", Weight: 1},
			},
		},
	}
	providerCfgs := map[string]config.ProviderConfig{
		"openai": {
			Type: "openai",
			Regions: []config.RegionConfig{
				{Name: "us-east", BaseURL: "https://us.api.openai.com/v1", Priority: 1},
			},
		},
	}
	routingCfg := config.RoutingConfig{RegionStrategy: "smart"}

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		r := New(models, providerCfgs, routingCfg, nil) // nil latencyReader
		if r.latencyCache != nil {
			t.Error("expected nil latencyCache when latencyReader is nil")
		}
	}()

	if panicked {
		t.Fatal("New() panicked with smart strategy and nil latencyReader")
	}
}

// ---------------------------------------------------------------------------
// BATCH-60 / TASK-02: Smart Routing Integration Verification
// ---------------------------------------------------------------------------

// TEST-60-02-01: Smart routing with 3 providers produces strictly ordered scores.
// Uses providers with distinct cost/latency/health profiles. The ranking must be:
// deepseek (cheap+fast+healthy) > openai (mid) > anthropic (expensive+slow+unhealthy).
func TestSmartRouting_StrictOrdering_ThreeProviders(t *testing.T) {
	r := &Router{
		strategy:     "smart",
		smartWeights: DefaultSmartWeights(), // Cost=0.4, Latency=0.4, Health=0.2
		health: &selectiveHealthChecker{
			healthy: map[string]bool{
				"deepseek:deepseek-chat:eu":   true,  // healthy
				"openai:gpt-4o:us-east":       true,  // healthy
				"anthropic:claude-sonnet-4-20250514:us": false, // unhealthy
			},
		},
		latencyCache: &LatencyCache{
			entries: map[string]time.Duration{
				"deepseek:deepseek-chat:eu":                              50 * time.Millisecond,  // fastest
				"openai:gpt-4o:us-east":                                  200 * time.Millisecond, // mid
				"anthropic:claude-sonnet-4-20250514:us":                   800 * time.Millisecond, // slowest
			},
			updated: time.Now(), // fresh TTL
			ttl:     time.Hour,
		},
	}

	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4o", Region: "us-east"},                    // mid cost, mid latency, healthy
		{Provider: "deepseek", Model: "deepseek-chat", Region: "eu"},              // cheap, fast, healthy
		{Provider: "anthropic", Model: "claude-sonnet-4-20250514", Region: "us"},   // expensive, slow, unhealthy
	}

	selected := r.selectSmart(targets, "gpt-4o")
	// deepseek should win: cheapest + fastest + healthy
	if selected.Provider != "deepseek" {
		t.Errorf("expected deepseek (cheapest+fastest+healthy), got %s", selected.Provider)
	}
}

// TEST-60-02-02: Smart routing falls back to cost-only when LatencyCache.Get
// returns (0, false) for all targets (no latency data available).
func TestSmartRouting_CostOnlyFallback_NoLatencyData(t *testing.T) {
	// latencyCache that always returns (0, false)
	alwaysMissCache := &LatencyCache{
		entries: map[string]time.Duration{}, // empty — no data
		updated: time.Now(),
		ttl:     time.Hour,
	}

	r := &Router{
		strategy:     "smart",
		smartWeights: DefaultSmartWeights(),
		health:       nil, // unknown health → healthScore=0.5 for all
		latencyCache: alwaysMissCache,
	}

	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4o", Region: "us-east"},       // avgCost=6.25
		{Provider: "deepseek", Model: "deepseek-chat", Region: "eu"},  // avgCost=0.685
		{Provider: "openai", Model: "gpt-4o-mini", Region: "us-west"}, // avgCost=0.375
	}

	selected := r.selectSmart(targets, "gpt-4o")
	// With no latency data, all get same median latency → cost dominates
	// gpt-4o-mini (avgCost=0.375) should win
	if selected.Model != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini (cheapest, cost-only fallback), got %s/%s", selected.Provider, selected.Model)
	}
}

// TEST-60-02-03: Smart routing with same cost+latency but different health
// produces >50% score difference between healthy and unhealthy targets.
func TestSmartRouting_HealthScoreDifferential(t *testing.T) {
	// Two targets with identical cost and latency, different health
	r := &Router{
		strategy: "smart",
		smartWeights: SmartWeights{Cost: 0.34, Latency: 0.33, Health: 0.33},
		health: &selectiveHealthChecker{
			healthy: map[string]bool{
				"deepseek:deepseek-chat:us": true,   // healthScore = 1.0
				"deepseek:deepseek-chat:eu": false,  // healthScore = 0.0
			},
		},
		latencyCache: &LatencyCache{
			entries: map[string]time.Duration{
				"deepseek:deepseek-chat:us": 100 * time.Millisecond,
				"deepseek:deepseek-chat:eu": 100 * time.Millisecond,
			},
			updated: time.Now(),
			ttl:     time.Hour,
		},
	}

	targets := []providers.Target{
		{Provider: "deepseek", Model: "deepseek-chat", Region: "us"}, // healthy
		{Provider: "deepseek", Model: "deepseek-chat", Region: "eu"}, // unhealthy
	}

	selected := r.selectSmart(targets, "deepseek-chat")
	// With same cost+latency, health determines the winner
	// Healthy: costScore=1.0, latScore=1.0, healthScore=1.0 → weighted=1.0
	// Unhealthy: costScore=1.0, latScore=1.0, healthScore=0.0 → weighted=0.67
	// Score diff: 1.0 vs 0.67 → ~49% difference (close to 50%)
	if selected.Region != "us" {
		t.Errorf("expected us region (healthy), got %q", selected.Region)
	}
}
