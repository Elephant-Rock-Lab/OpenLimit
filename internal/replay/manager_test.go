package replay

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"openlimit/internal/config"
	"openlimit/internal/providers"
	"openlimit/internal/schema/openai"
)

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

func mockCompleteFn(delay time.Duration, err error) CompleteChatFunc {
	return func(ctx context.Context, req openai.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openai.ChatCompletionResponse, error) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if err != nil {
			return nil, err
		}
		return &openai.ChatCompletionResponse{
			Usage: &openai.Usage{PromptTokens: 100, CompletionTokens: 50},
		}, nil
	}
}

func mockKeyFn(providerName string) (providers.ProviderKey, error) {
	return providers.ProviderKey{ID: "test-key", Value: "sk-test"}, nil
}

func testConfig(enabled bool, routes []config.ReplayRoute) config.ReplayConfig {
	return config.ReplayConfig{Enabled: enabled, Routes: routes}
}

func basicRoute(sampleRate float64) config.ReplayRoute {
	return config.ReplayRoute{
		Model:          "gpt-4o",
		ShadowProvider: "deepseek",
		ShadowModel:    "deepseek-chat",
		SampleRate:     sampleRate,
	}
}

// ---------------------------------------------------------------------------
// TEST-55-01-01: NewManager returns nil when disabled
// ---------------------------------------------------------------------------
func TestNewManager_DisabledReturnsNil(t *testing.T) {
	cfg := testConfig(false, nil)
	mgr := NewManager(cfg, mockCompleteFn(0, nil), mockKeyFn, nil)
	if mgr != nil {
		t.Fatal("expected nil manager when replay is disabled")
	}
}

// ---------------------------------------------------------------------------
// TEST-55-01-02: Replay fires async (primary returns immediately)
// ---------------------------------------------------------------------------
func TestReplay_AsyncNonBlocking(t *testing.T) {
	cfg := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})
	mgr := NewManager(cfg, mockCompleteFn(200*time.Millisecond, nil), mockKeyFn, nil)
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	defer mgr.Close()

	req := openai.ChatCompletionRequest{Model: "gpt-4o", Messages: []openai.ChatMessage{{Role: "user"}}}
	start := time.Now()
	mgr.Replay(req, "openai", 100*time.Millisecond)
	elapsed := time.Since(start)

	// Replay should return immediately (fire-and-forget), not wait 200ms
	if elapsed > 50*time.Millisecond {
		t.Fatalf("Replay() blocked for %v, expected fire-and-forget", elapsed)
	}

	// Wait for the async goroutine to complete
	time.Sleep(300 * time.Millisecond)

	results := mgr.Recent(1)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// TEST-55-01-03: Sample rate 0.0 replays nothing, rate 1.0 replays all
// ---------------------------------------------------------------------------
func TestReplay_SampleRateFiltering(t *testing.T) {
	// rate 0.0 should replay nothing
	cfg0 := testConfig(true, []config.ReplayRoute{basicRoute(0.0)})
	mgr0 := NewManager(cfg0, mockCompleteFn(0, nil), mockKeyFn, nil)
	defer mgr0.Close()

	req := openai.ChatCompletionRequest{Model: "gpt-4o", Messages: []openai.ChatMessage{{Role: "user"}}}
	for i := 0; i < 100; i++ {
		mgr0.Replay(req, "openai", 100*time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if len(mgr0.Recent(1000)) != 0 {
		t.Errorf("sample_rate=0.0 should replay nothing, got %d", len(mgr0.Recent(1000)))
	}

	// rate 1.0 should replay all
	cfg1 := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})
	mgr1 := NewManager(cfg1, mockCompleteFn(0, nil), mockKeyFn, nil)
	defer mgr1.Close()

	for i := 0; i < 10; i++ {
		mgr1.Replay(req, "openai", 100*time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if len(mgr1.Recent(1000)) != 10 {
		t.Errorf("sample_rate=1.0 should replay all 10, got %d", len(mgr1.Recent(1000)))
	}
}

// ---------------------------------------------------------------------------
// TEST-55-01-04: Ring buffer: 1001 stores → 1000 results
// ---------------------------------------------------------------------------
func TestReplay_RingBufferEviction(t *testing.T) {
	cfg := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})
	mgr := NewManager(cfg, mockCompleteFn(0, nil), mockKeyFn, nil)
	defer mgr.Close()

	// Store 1001 results synchronously via store() (same-package access)
	for i := 0; i < 1001; i++ {
		mgr.store(ReplayResult{
			ID:               genID(),
			PrimaryLatencyMS: int64(i + 1),
		})
	}

	results := mgr.Recent(1001)
	if len(results) != 1000 {
		t.Fatalf("expected 1000 results after 1001 inserts, got %d", len(results))
	}

	// Verify eviction happened: results are a subset (first entry is not 1ms)
	if results[0].PrimaryLatencyMS == 1 {
		t.Error("expected first result to be evicted, but latency=1ms still present")
	}
	// Last entry should be 1001
	if results[len(results)-1].PrimaryLatencyMS != 1001 {
		t.Errorf("expected last result latency=1001ms, got %d", results[len(results)-1].PrimaryLatencyMS)
	}
}

// ---------------------------------------------------------------------------
// TEST-55-01-05: Result has latency and status fields
// ---------------------------------------------------------------------------
func TestReplay_ResultHasLatencyAndStatus(t *testing.T) {
	cfg := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})
	mgr := NewManager(cfg, mockCompleteFn(10*time.Millisecond, nil), mockKeyFn, nil)
	defer mgr.Close()

	req := openai.ChatCompletionRequest{Model: "gpt-4o", Messages: []openai.ChatMessage{{Role: "user"}}}
	mgr.Replay(req, "openai", 150*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	results := mgr.Recent(1)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.PrimaryLatencyMS != 150 {
		t.Errorf("expected primary_latency_ms=150, got %d", r.PrimaryLatencyMS)
	}
	if r.ShadowLatencyMS <= 0 {
		t.Errorf("expected shadow_latency_ms > 0, got %d", r.ShadowLatencyMS)
	}
	if r.ShadowStatus != 200 {
		t.Errorf("expected shadow_status=200, got %d", r.ShadowStatus)
	}
	if r.ShadowTokensIn != 100 {
		t.Errorf("expected shadow_tokens_in=100, got %d", r.ShadowTokensIn)
	}
	if r.ShadowTokensOut != 50 {
		t.Errorf("expected shadow_tokens_out=50, got %d", r.ShadowTokensOut)
	}
	if r.PrimaryProvider != "openai" {
		t.Errorf("expected primary_provider=openai, got %s", r.PrimaryProvider)
	}
	if r.ShadowProvider != "deepseek" {
		t.Errorf("expected shadow_provider=deepseek, got %s", r.ShadowProvider)
	}
	if r.ShadowModel != "deepseek-chat" {
		t.Errorf("expected shadow_model=deepseek-chat, got %s", r.ShadowModel)
	}
	if r.ID == "" || r.ID[:3] != "rp_" {
		t.Errorf("expected id with rp_ prefix, got %s", r.ID)
	}
}

// ---------------------------------------------------------------------------
// TEST-55-01-06: Replay error doesn't propagate
// ---------------------------------------------------------------------------
func TestReplay_ErrorDoesNotPropagate(t *testing.T) {
	cfg := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})
	mgr := NewManager(cfg, mockCompleteFn(0, errors.New("shadow provider down")), mockKeyFn, nil)
	defer mgr.Close()

	req := openai.ChatCompletionRequest{Model: "gpt-4o", Messages: []openai.ChatMessage{{Role: "user"}}}
	// This should NOT panic or block
	mgr.Replay(req, "openai", 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	results := mgr.Recent(1)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].ShadowError == "" {
		t.Error("expected shadow_error to be populated, got empty")
	}
	if results[0].ShadowError != "shadow provider down" {
		t.Errorf("expected error message 'shadow provider down', got %q", results[0].ShadowError)
	}
}

// ---------------------------------------------------------------------------
// TEST-55-01-07: ReplayConfig parsed from YAML
// ---------------------------------------------------------------------------
func TestReplayConfig_ParsedFromYAML(t *testing.T) {
	yamlData := `
enabled: true
routes:
  - model: gpt-4o
    shadow_provider: deepseek
    shadow_model: deepseek-chat
    sample_rate: 0.1
  - model: claude-sonnet-4-20250514
    shadow_provider: openai
    shadow_model: gpt-4o
    sample_rate: 1.0
`
	var cfg config.ReplayConfig
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	if !cfg.Enabled {
		t.Error("expected enabled=true")
	}
	if len(cfg.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(cfg.Routes))
	}
	if cfg.Routes[0].Model != "gpt-4o" {
		t.Errorf("expected model=gpt-4o, got %s", cfg.Routes[0].Model)
	}
	if cfg.Routes[0].ShadowProvider != "deepseek" {
		t.Errorf("expected shadow_provider=deepseek, got %s", cfg.Routes[0].ShadowProvider)
	}
	if cfg.Routes[0].SampleRate != 0.1 {
		t.Errorf("expected sample_rate=0.1, got %f", cfg.Routes[0].SampleRate)
	}
	if cfg.Routes[1].ShadowModel != "gpt-4o" {
		t.Errorf("expected shadow_model=gpt-4o, got %s", cfg.Routes[1].ShadowModel)
	}
}

// ---------------------------------------------------------------------------
// TEST-55-01-08: Context timeout respected (shadow request times out at 30s)
// ---------------------------------------------------------------------------
func TestReplay_ContextTimeout(t *testing.T) {
	cfg := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})

	// Mock blocks until context cancelled
	completeFn := func(ctx context.Context, req openai.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openai.ChatCompletionResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	mgr := NewManager(cfg, completeFn, mockKeyFn, nil)
	mgr.timeout = 200 * time.Millisecond // short timeout for test speed

	req := openai.ChatCompletionRequest{Model: "gpt-4o", Messages: []openai.ChatMessage{{Role: "user"}}}
	mgr.Replay(req, "openai", 50*time.Millisecond)

	// Wait for goroutine to finish (200ms timeout + overhead)
	time.Sleep(500 * time.Millisecond)

	results := mgr.Recent(10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ShadowError == "" {
		t.Error("expected shadow error due to context timeout, got empty")
	}
	mgr.Close()
}

// ---------------------------------------------------------------------------
// TEST-55-01-09: Recent(n) returns last n results
// ---------------------------------------------------------------------------
func TestReplay_RecentReturnsLastN(t *testing.T) {
	cfg := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})
	mgr := NewManager(cfg, mockCompleteFn(0, nil), mockKeyFn, nil)
	defer mgr.Close()

	// Store 20 results synchronously for deterministic ordering
	for i := 0; i < 20; i++ {
		mgr.store(ReplayResult{
			ID:               genID(),
			PrimaryLatencyMS: int64(i + 1),
		})
	}

	recent := mgr.Recent(5)
	if len(recent) != 5 {
		t.Fatalf("expected 5 results, got %d", len(recent))
	}
	// Should be the last 5 entries (highest latency values)
	if recent[4].PrimaryLatencyMS != 20 {
		t.Errorf("expected last recent latency=20, got %d", recent[4].PrimaryLatencyMS)
	}
	// Verify ascending order
	for i := 1; i < len(recent); i++ {
		if recent[i].PrimaryLatencyMS <= recent[i-1].PrimaryLatencyMS {
			t.Errorf("results not in insertion order: [%d]=%d <= [%d]=%d", i-1, recent[i-1].PrimaryLatencyMS, i, recent[i].PrimaryLatencyMS)
		}
	}
}

// ---------------------------------------------------------------------------
// TEST-55-01-10: Summary computes avg latency and error rate
// ---------------------------------------------------------------------------
func TestReplay_SummaryStatistics(t *testing.T) {
	cfg := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})

	var callCount int32
	completeFn := func(ctx context.Context, req openai.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openai.ChatCompletionResponse, error) {
		n := atomic.AddInt32(&callCount, 1)
		if n <= 2 {
			// First 2 succeed
			return &openai.ChatCompletionResponse{Usage: &openai.Usage{PromptTokens: 100, CompletionTokens: 50}}, nil
		}
		// Rest fail
		return nil, errors.New("timeout")
	}

	mgr := NewManager(cfg, completeFn, mockKeyFn, nil)
	defer mgr.Close()

	req := openai.ChatCompletionRequest{Model: "gpt-4o", Messages: []openai.ChatMessage{{Role: "user"}}}
	// 5 replays: 2 success, 3 errors
	for i := 0; i < 5; i++ {
		mgr.Replay(req, "openai", 100*time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	mgr.wg.Wait()

	summary := mgr.Summary(100)
	if summary.Total != 5 {
		t.Errorf("expected total=5, got %d", summary.Total)
	}
	if summary.AvgPrimaryMS != 100.0 {
		t.Errorf("expected avg_primary_ms=100.0, got %f", summary.AvgPrimaryMS)
	}
	if summary.ShadowErrorRate != 0.6 {
		t.Errorf("expected shadow_error_rate=0.6, got %f", summary.ShadowErrorRate)
	}
	if summary.AvgShadowMS < 0 {
		t.Errorf("expected avg_shadow_ms >= 0, got %f", summary.AvgShadowMS)
	}
	if len(summary.Results) != 5 {
		t.Errorf("expected 5 results in summary, got %d", len(summary.Results))
	}
}

// ---------------------------------------------------------------------------
// BATCH-59 / TASK-03: Ring buffer optimization tests
// ---------------------------------------------------------------------------

// TEST-59-03-01: Store 2000 results in cap=100 ring; Recent(100) returns last 100.
func TestRingBuffer_Store2000_ReturnsLast100(t *testing.T) {
	cfg := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})
	mgr := NewManager(cfg, mockCompleteFn(0, nil), mockKeyFn, nil)
	defer mgr.Close()

	// Store 2000 results synchronously
	for i := 0; i < 2000; i++ {
		mgr.store(ReplayResult{
			ID:               genID(),
			PrimaryLatencyMS: int64(i + 1),
		})
	}

	recent := mgr.Recent(100)
	if len(recent) != 100 {
		t.Fatalf("expected 100 results, got %d", len(recent))
	}

	// First result should be #1901 (2000 - 100 + 1)
	if recent[0].PrimaryLatencyMS != 1901 {
		t.Errorf("expected first result latency=1901, got %d", recent[0].PrimaryLatencyMS)
	}
	// Last result should be #2000
	if recent[99].PrimaryLatencyMS != 2000 {
		t.Errorf("expected last result latency=2000, got %d", recent[99].PrimaryLatencyMS)
	}

	// Verify ascending order
	for i := 1; i < len(recent); i++ {
		if recent[i].PrimaryLatencyMS <= recent[i-1].PrimaryLatencyMS {
			t.Errorf("results not in order: [%d]=%d <= [%d]=%d", i-1, recent[i-1].PrimaryLatencyMS, i, recent[i].PrimaryLatencyMS)
		}
	}
}

// TEST-59-03-02: Eviction caps at capacity — store cap+100 items, count stays at cap.
func TestRingBuffer_EvictionCapsAtCapacity(t *testing.T) {
	cfg := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})
	mgr := NewManager(cfg, mockCompleteFn(0, nil), mockKeyFn, nil)
	defer mgr.Close()

	// Store cap+100 items
	total := mgr.cap + 100
	for i := 0; i < total; i++ {
		mgr.store(ReplayResult{
			ID:               genID(),
			PrimaryLatencyMS: int64(i + 1),
		})
	}

	// mgr.count should be exactly cap
	mgr.mu.Lock()
	count := mgr.count
	mgr.mu.Unlock()

	if count != mgr.cap {
		t.Errorf("expected count=%d (cap), got %d", mgr.cap, count)
	}

	// Recent(cap) should return exactly cap items
	recent := mgr.Recent(mgr.cap)
	if len(recent) != mgr.cap {
		t.Errorf("expected %d results from Recent(cap), got %d", mgr.cap, len(recent))
	}
}

// TEST-59-03-03: Concurrent store and recent don't race.
func TestRingBuffer_ConcurrentStoreAndRecent(t *testing.T) {
	cfg := testConfig(true, []config.ReplayRoute{basicRoute(1.0)})
	mgr := NewManager(cfg, mockCompleteFn(0, nil), mockKeyFn, nil)
	defer mgr.Close()

	var wg sync.WaitGroup

	// Concurrent writers
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				mgr.store(ReplayResult{
					ID:               genID(),
					PrimaryLatencyMS: int64(id*1000 + i),
				})
			}
		}(g)
	}

	// Concurrent readers
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = mgr.Recent(50)
			}
		}()
	}

	wg.Wait()

	// Should have cap items (ring buffer capacity)
	recent := mgr.Recent(1000)
	if len(recent) != mgr.cap {
		t.Errorf("expected %d results after concurrent access, got %d", mgr.cap, len(recent))
	}
}
