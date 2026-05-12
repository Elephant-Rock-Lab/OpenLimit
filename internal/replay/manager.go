package replay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/providers"
	"openlimit/internal/schema/openai"
)

// ReplayResult holds the outcome of a shadow replay request.
type ReplayResult struct {
	ID               string `json:"id"`
	Timestamp        string `json:"timestamp"`
	Model            string `json:"model"`
	PrimaryProvider  string `json:"primary_provider"`
	ShadowProvider   string `json:"shadow_provider"`
	ShadowModel      string `json:"shadow_model"`
	PrimaryLatencyMS int64  `json:"primary_latency_ms"`
	ShadowLatencyMS  int64  `json:"shadow_latency_ms"`
	ShadowStatus     int    `json:"shadow_status"`
	ShadowTokensIn   int    `json:"shadow_tokens_in"`
	ShadowTokensOut  int    `json:"shadow_tokens_out"`
	ShadowError      string `json:"shadow_error,omitempty"`
}

// ReplaySummary is the admin API response.
type ReplaySummary struct {
	Results         []ReplayResult `json:"results"`
	Total           int            `json:"total"`
	AvgPrimaryMS    float64        `json:"avg_primary_ms"`
	AvgShadowMS     float64        `json:"avg_shadow_ms"`
	ShadowErrorRate float64        `json:"shadow_error_rate"`
}

// CompleteChatFunc is the function signature for making a chat completion call.
type CompleteChatFunc func(ctx context.Context, req openai.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openai.ChatCompletionResponse, error)

// KeyFunc returns the next key for a provider.
type KeyFunc func(providerName string) (providers.ProviderKey, error)

// Manager handles shadow replay of requests.
type Manager struct {
	cfg        config.ReplayConfig
	completeFn CompleteChatFunc
	keyFn      KeyFunc
	logger     *slog.Logger
	timeout    time.Duration

	mu    sync.Mutex
	ring  []ReplayResult
	head  int // next write position
	count int // current number of items
	cap   int // maximum capacity (ring never grows beyond this)

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewManager creates a new replay manager. Returns nil if replay is disabled.
func NewManager(cfg config.ReplayConfig, completeFn CompleteChatFunc, keyFn KeyFunc, logger *slog.Logger) *Manager {
	if !cfg.Enabled {
		return nil
	}
	cap := 1000
	return &Manager{
		cfg:        cfg,
		completeFn: completeFn,
		keyFn:      keyFn,
		logger:     logger,
		timeout:    30 * time.Second,
		ring:       make([]ReplayResult, cap),
		cap:        cap,
		stopCh:     make(chan struct{}),
	}
}

// Replay fires a shadow request asynchronously (fire-and-forget).
func (m *Manager) Replay(req openai.ChatCompletionRequest, primaryProvider string, primaryLatency time.Duration) {
	if m == nil {
		return
	}
	// Find matching route
	var route *config.ReplayRoute
	for i := range m.cfg.Routes {
		if m.cfg.Routes[i].Model == req.Model {
			route = &m.cfg.Routes[i]
			break
		}
	}
	if route == nil {
		return
	}
	// Sample rate check
	if route.SampleRate < 1.0 {
		// Simple sampling: use timestamp nanoseconds
		if time.Now().UnixNano()%100 >= int64(route.SampleRate*100) {
			return
		}
	}

	shadowReq := req // copy
	shadowReq.Model = route.ShadowModel

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("replay goroutine panic", "error", r)
			}
		}()

		// Check if we should stop
		select {
		case <-m.stopCh:
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
		defer cancel()

		key, err := m.keyFn(route.ShadowProvider)
		if err != nil {
			m.store(ReplayResult{
				ID:               genID(),
				Timestamp:        time.Now().UTC().Format(time.RFC3339),
				Model:            req.Model,
				PrimaryProvider:  primaryProvider,
				ShadowProvider:   route.ShadowProvider,
				ShadowModel:      route.ShadowModel,
				PrimaryLatencyMS: primaryLatency.Milliseconds(),
				ShadowError:      "key error: " + err.Error(),
			})
			return
		}

		target := providers.Target{
			Provider: route.ShadowProvider,
			Model:    route.ShadowModel,
		}

		start := time.Now()
		resp, err := m.completeFn(ctx, shadowReq, target, key)
		elapsed := time.Since(start)

		result := ReplayResult{
			ID:               genID(),
			Timestamp:        time.Now().UTC().Format(time.RFC3339),
			Model:            req.Model,
			PrimaryProvider:  primaryProvider,
			ShadowProvider:   route.ShadowProvider,
			ShadowModel:      route.ShadowModel,
			PrimaryLatencyMS: primaryLatency.Milliseconds(),
			ShadowLatencyMS:  elapsed.Milliseconds(),
		}

		if err != nil {
			result.ShadowError = err.Error()
		} else if resp != nil {
			result.ShadowStatus = 200
			if resp.Usage != nil {
				result.ShadowTokensIn = resp.Usage.PromptTokens
				result.ShadowTokensOut = resp.Usage.CompletionTokens
			}
		}

		m.store(result)
	}()
}

func (m *Manager) store(r ReplayResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ring[m.head] = r
	m.head = (m.head + 1) % m.cap
	if m.count < m.cap {
		m.count++
	}
}

// Recent returns the last n replay results.
func (m *Manager) Recent(n int) []ReplayResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n > m.count {
		n = m.count
	}
	result := make([]ReplayResult, n)
	// Read from ring using modular arithmetic.
	// The oldest item is at (head - count + cap) % cap.
	start := (m.head - m.count + m.cap) % m.cap
	// We want the last n items, so skip (count - n) items from the start.
	offset := m.count - n
	for i := 0; i < n; i++ {
		idx := (start + offset + i) % m.cap
		result[i] = m.ring[idx]
	}
	return result
}

// Summary returns recent results with aggregate statistics.
func (m *Manager) Summary(n int) ReplaySummary {
	results := m.Recent(n)
	if len(results) == 0 {
		return ReplaySummary{Results: []ReplayResult{}}
	}

	var totalPrimary, totalShadow int64
	var errors int
	for _, r := range results {
		totalPrimary += r.PrimaryLatencyMS
		totalShadow += r.ShadowLatencyMS
		if r.ShadowError != "" {
			errors++
		}
	}
	count := float64(len(results))
	return ReplaySummary{
		Results:         results,
		Total:           len(results),
		AvgPrimaryMS:    float64(totalPrimary) / count,
		AvgShadowMS:     float64(totalShadow) / count,
		ShadowErrorRate: float64(errors) / count,
	}
}

// Close stops the replay manager and waits for in-flight goroutines.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	close(m.stopCh)
	m.wg.Wait()
}

func genID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "rp_" + hex.EncodeToString(b)
}
