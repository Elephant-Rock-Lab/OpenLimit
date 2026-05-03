package semantic

import (
	"sync"
	"time"
)

// CircuitBreaker prevents cascading failures when the embedding service is down.
// After `threshold` failures within `window`, it opens for `cooldown` duration.
type CircuitBreaker struct {
	mu        sync.Mutex
	failures  int
	lastFail  time.Time
	threshold int
	window    time.Duration
	cooldown  time.Duration
	openedAt  time.Time
}

// NewCircuitBreaker creates a circuit breaker with default settings:
// 3 failures in 30s → open for 60s.
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		threshold: 3,
		window:    30 * time.Second,
		cooldown:  60 * time.Second,
	}
}

// Allow returns true if a request is permitted (circuit is closed or half-open).
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.failures < cb.threshold {
		return true
	}

	// Check if cooldown has elapsed (half-open)
	if time.Since(cb.openedAt) > cb.cooldown {
		cb.failures = 0
		return true
	}

	return false
}

// RecordSuccess resets the failure count.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
}

// RecordFailure increments the failure count and potentially opens the circuit.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	// Reset if outside the window
	if now.Sub(cb.lastFail) > cb.window {
		cb.failures = 1
	} else {
		cb.failures++
	}
	cb.lastFail = now

	if cb.failures >= cb.threshold {
		cb.openedAt = now
	}
}

// State returns the current circuit state for diagnostics.
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.failures < cb.threshold {
		return "closed"
	}
	if time.Since(cb.openedAt) > cb.cooldown {
		return "half-open"
	}
	return "open"
}
