package routing

import (
	"fmt"
	"testing"
	"time"

	"openlimit/internal/config"
)

func TestPlan_NoRegions(t *testing.T) {
	models := map[string]config.ModelConfig{
		"gpt-4": {
			Routes: []config.ModelRoute{
				{Provider: "openai", Model: "gpt-4", Weight: 1},
			},
		},
	}
	providers := map[string]config.ProviderConfig{
		"openai": {Type: "openai"},
	}
	r := New(models, providers, config.RoutingConfig{}, nil)

	plan, err := r.Plan("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(plan.Targets))
	}
	if plan.Targets[0].Region != "" {
		t.Errorf("expected empty region, got %q", plan.Targets[0].Region)
	}
	if plan.Targets[0].BaseURL != "" {
		t.Errorf("expected empty BaseURL, got %q", plan.Targets[0].BaseURL)
	}
}

func TestPlan_WithRegions_PriorityStrategy(t *testing.T) {
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
	if len(plan.Targets) < 1 {
		t.Fatal("expected at least 1 target")
	}
	// Primary should be us-east (priority 1)
	if plan.Targets[0].Region != "us-east" {
		t.Errorf("expected primary region us-east, got %q", plan.Targets[0].Region)
	}
	if plan.Targets[0].BaseURL != "https://us.api.openai.com/v1" {
		t.Errorf("expected us-east base URL, got %q", plan.Targets[0].BaseURL)
	}
}

func TestPlan_WithRegions_LocalPreference(t *testing.T) {
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
				{Name: "eu-west", BaseURL: "https://eu.api.openai.com/v1", Priority: 1},
			},
		},
	}
	routingCfg := config.RoutingConfig{
		Region: "eu-west",
	}
	r := New(models, providerCfgs, routingCfg, nil)

	plan, err := r.Plan("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// With equal priority and local region = eu-west, should prefer eu-west
	if plan.Targets[0].Region != "eu-west" {
		t.Errorf("expected local region eu-west, got %q", plan.Targets[0].Region)
	}
}

func TestPlan_LatencyStrategy_ColdStart(t *testing.T) {
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
	routingCfg := config.RoutingConfig{
		RegionStrategy: "latency",
	}
	// Mock LatencyReader that returns no data (cold start)
	r := New(models, providerCfgs, routingCfg, &noopLatencyReader{})

	plan, err := r.Plan("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// Cold start should fall back to priority, so us-east (priority 1) should be chosen
	if plan.Targets[0].Region != "us-east" {
		t.Errorf("cold start should fall back to priority, expected us-east, got %q", plan.Targets[0].Region)
	}
}

func TestPlan_LatencyStrategy_WithData(t *testing.T) {
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
	routingCfg := config.RoutingConfig{
		RegionStrategy: "latency",
	}
	// Mock LatencyReader where eu-west is faster
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
	// Should pick eu-west (lower latency) despite higher priority number
	if plan.Targets[0].Region != "eu-west" {
		t.Errorf("expected eu-west (lower latency), got %q", plan.Targets[0].Region)
	}
}

func TestPlan_UnknownStrategy(t *testing.T) {
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
				{Name: "us-east", BaseURL: "https://us.api.openai.com/v1", Priority: 2},
				{Name: "eu-west", BaseURL: "https://eu.api.openai.com/v1", Priority: 1},
			},
		},
	}
	routingCfg := config.RoutingConfig{
		RegionStrategy: "unknown",
	}
	r := New(models, providerCfgs, routingCfg, nil)

	plan, err := r.Plan("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// Unknown strategy should default to priority — eu-west (priority 1) wins
	if plan.Targets[0].Region != "eu-west" {
		t.Errorf("expected eu-west (priority 1), got %q", plan.Targets[0].Region)
	}
}

func TestLatencyCache_Refresh(t *testing.T) {
	reader := &mockLatencyReader{
		latencies: map[string]time.Duration{
			"openai:gpt-4:us-east": 200 * time.Millisecond,
		},
	}
	cache := NewLatencyCache(reader, []combo{
		{provider: "openai", model: "gpt-4", region: "us-east"},
		{provider: "openai", model: "gpt-4", region: "eu-west"},
	}, 10*time.Second)

	// Initially no data
	d, ok := cache.Get("openai", "gpt-4", "us-east")
	if !ok {
		t.Fatal("expected data for us-east")
	}
	if d != 200*time.Millisecond {
		t.Errorf("expected 200ms, got %v", d)
	}

	// eu-west has no data
	_, ok = cache.Get("openai", "gpt-4", "eu-west")
	if ok {
		t.Error("expected no data for eu-west")
	}
}

// --- Mock implementations ---

type noopLatencyReader struct{}

func (n *noopLatencyReader) RegionLatency(provider, model, region string) (time.Duration, bool) {
	return 0, false
}

func TestRouterConcurrentPlan(t *testing.T) {
	models := map[string]config.ModelConfig{
		"gpt-4": {
			Routes: []config.ModelRoute{
				{Provider: "openai", Model: "gpt-4", Weight: 2},
				{Provider: "anthropic", Model: "claude-3", Weight: 1},
			},
		},
	}
	providers := map[string]config.ProviderConfig{
		"openai":    {Type: "openai"},
		"anthropic": {Type: "anthropic"},
	}
	r := New(models, providers, config.RoutingConfig{}, nil)

	const goroutines = 50
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			plan, err := r.Plan("gpt-4")
			if err != nil {
				errCh <- err
				return
			}
			if len(plan.Targets) == 0 {
				errCh <- fmt.Errorf("empty targets")
				return
			}
			errCh <- nil
		}()
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
}

// --- Mock implementations ---

type mockLatencyReader struct {
	latencies map[string]time.Duration
}

func (m *mockLatencyReader) RegionLatency(provider, model, region string) (time.Duration, bool) {
	key := provider + ":" + model + ":" + region
	d, ok := m.latencies[key]
	return d, ok
}
