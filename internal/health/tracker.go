package health

import (
	"sync"
	"time"
)

// ModelHealth tracks the health state for a single provider:region:model triple.
type ModelHealth struct {
	Provider            string
	Model               string
	Region              string
	LastSuccess         time.Time
	LastFailure         time.Time
	ConsecutiveFailures int
}

// Tracker records per-model health state and reports whether a given
// provider:region:model triple is considered healthy based on a time window
// and consecutive-failure count.
//
// A model is healthy iff LastSuccess is within the window AND
// ConsecutiveFailures == 0 (AUTH-03, AC-03-05).
type Tracker struct {
	mu     sync.RWMutex
	models map[string]*ModelHealth // key: "provider:region:model"
	window time.Duration           // default 30s (AC-03-04)
}

// NewTracker creates a Tracker with the given health window.
// The window determines how recent a successful response must be
// for a model to be considered healthy.
func NewTracker(window time.Duration) *Tracker {
	if window <= 0 {
		window = 30 * time.Second
	}
	t := &Tracker{
		models: make(map[string]*ModelHealth),
		window: window,
	}
	go t.evictLoop()
	return t
}

// healthKey returns the map key for a provider:region:model triple.
func healthKey(provider, model, region string) string {
	return provider + ":" + region + ":" + model
}

// getOrCreate returns the ModelHealth entry for the given triple,
// creating it if it does not yet exist.
func (t *Tracker) getOrCreate(provider, model, region string) *ModelHealth {
	key := healthKey(provider, model, region)
	h, ok := t.models[key]
	if !ok {
		h = &ModelHealth{
			Provider: provider,
			Model:    model,
			Region:   region,
		}
		t.models[key] = h
	}
	return h
}

// RecordSuccess records a successful provider call for the given triple.
// It sets LastSuccess to now and resets ConsecutiveFailures to 0 (AC-03-05).
func (t *Tracker) RecordSuccess(provider, model, region string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	h := t.getOrCreate(provider, model, region)
	h.LastSuccess = time.Now()
	h.ConsecutiveFailures = 0
}

// RecordFailure records a failed provider call for the given triple.
// It increments ConsecutiveFailures and sets LastFailure to now.
func (t *Tracker) RecordFailure(provider, model, region string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	h := t.getOrCreate(provider, model, region)
	h.ConsecutiveFailures++
	h.LastFailure = time.Now()
}

// IsHealthy returns true iff the model's last success is within the health
// window AND ConsecutiveFailures == 0. Models that have never been recorded
// are considered healthy (no evidence of failure).
func (t *Tracker) IsHealthy(provider, model, region string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := healthKey(provider, model, region)
	h, ok := t.models[key]
	if !ok {
		// Never recorded — assume healthy (no evidence of failure).
		return true
	}

	if h.ConsecutiveFailures > 0 {
		return false
	}

	if h.LastSuccess.IsZero() {
		// Never succeeded but also no failures — healthy.
		return true
	}

	return time.Since(h.LastSuccess) <= t.window
}

// GetAll returns a snapshot of all recorded ModelHealth entries.
func (t *Tracker) GetAll() []ModelHealth {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]ModelHealth, 0, len(t.models))
	for _, h := range t.models {
		result = append(result, *h)
	}
	return result
}

// evictLoop periodically removes stale entries with zero consecutive failures
// that haven't been updated in over 1 hour.
func (t *Tracker) evictLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		t.mu.Lock()
		now := time.Now()
		for key, h := range t.models {
			if h.ConsecutiveFailures == 0 {
				lastActivity := h.LastSuccess
				if h.LastFailure.After(lastActivity) {
					lastActivity = h.LastFailure
				}
				if !lastActivity.IsZero() && now.Sub(lastActivity) > time.Hour {
					delete(t.models, key)
				}
			}
		}
		t.mu.Unlock()
	}
}
