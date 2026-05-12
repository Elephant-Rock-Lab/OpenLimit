package circuit

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	rediscli "openlimit/internal/redis"
)

// State represents the circuit breaker state.
type State string

const (
	Closed   State = "closed"
	Open     State = "open"
	HalfOpen State = "half-open"
)

// Breaker prevents cascading failures to an upstream provider.
// When Redis is available, state is shared across gateway instances.
// When Redis is unavailable, it degrades to local-only state.
type Breaker struct {
	rc        *rediscli.Client
	key       string
	threshold int
	window    time.Duration
	cooldown  time.Duration
	logger    *slog.Logger

	// Local fallback (used when rc is nil or unhealthy)
	mu       sync.Mutex
	failures int
	lastFail time.Time
	openedAt time.Time
}

// Option configures a Breaker.
type Option func(*Breaker)

// WithThreshold sets the failure threshold (default: 5).
func WithThreshold(n int) Option {
	return func(b *Breaker) { b.threshold = n }
}

// WithWindow sets the failure counting window (default: 30s).
func WithWindow(d time.Duration) Option {
	return func(b *Breaker) { b.window = d }
}

// WithCooldown sets how long the breaker stays open (default: 60s).
func WithCooldown(d time.Duration) Option {
	return func(b *Breaker) { b.cooldown = d }
}

// NewBreaker creates a circuit breaker for a provider+model combination.
// If rc is nil, the breaker operates in local-only mode.
func NewBreaker(rc *rediscli.Client, provider, model string, logger *slog.Logger, opts ...Option) *Breaker {
	b := &Breaker{
		rc:        rc,
		key:       fmt.Sprintf("circuit:%s:%s", provider, model),
		threshold: 5,
		window:    30 * time.Second,
		cooldown:  60 * time.Second,
		logger:    logger,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Allow returns true if requests should be permitted.
func (b *Breaker) Allow() bool {
	if b.rc == nil || !b.rc.Healthy() {
		return b.localAllow()
	}
	return b.redisAllow()
}

// RecordSuccess resets the failure count.
func (b *Breaker) RecordSuccess() {
	if b.rc == nil || !b.rc.Healthy() {
		b.localRecordSuccess()
		return
	}
	b.redisRecordSuccess()
}

// RecordFailure increments the failure count and potentially opens the circuit.
func (b *Breaker) RecordFailure() {
	if b.rc == nil || !b.rc.Healthy() {
		b.localRecordFailure()
		return
	}
	b.redisRecordFailure()
}

// State returns the current circuit state for diagnostics and metrics.
func (b *Breaker) State() State {
	if b.rc == nil || !b.rc.Healthy() {
		return b.localState()
	}
	return b.redisState()
}

// --- Local fallback ---

func (b *Breaker) localAllow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.failures < b.threshold {
		return true
	}
	if time.Since(b.openedAt) > b.cooldown {
		b.failures = 0
		return true
	}
	return false
}

func (b *Breaker) localRecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
}

func (b *Breaker) localRecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	if now.Sub(b.lastFail) > b.window {
		b.failures = 1
	} else {
		b.failures++
	}
	b.lastFail = now
	if b.failures >= b.threshold {
		b.openedAt = now
	}
}

func (b *Breaker) localState() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures < b.threshold {
		return Closed
	}
	if time.Since(b.openedAt) > b.cooldown {
		return HalfOpen
	}
	return Open
}

// --- Redis-backed state ---

func (b *Breaker) redisAllow() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	m, err := b.rc.HGetAll(ctx, b.key)
	if err != nil {
		return true
	}

	failures := atoi64(m["failures"])
	if int(failures) < b.threshold {
		return true
	}

	openedAt := atoi64(m["opened_at"])
	if openedAt > 0 && time.Since(time.Unix(openedAt, 0)) > b.cooldown {
		return true
	}

	return false
}

func (b *Breaker) redisRecordSuccess() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	b.rc.HSet(ctx, b.key, "failures", 0)
	b.rc.Set(ctx, b.key+":ttl", "1", 2*b.window)
}

func (b *Breaker) redisRecordFailure() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	now := time.Now()

	// Atomically increment the failure counter using HINCRBY.
	// This eliminates the TOCTOU race from the previous HGETALL + HSET pattern.
	newCount, err := b.rc.HIncrBy(ctx, b.key, "failures", 1)
	if err != nil {
		return
	}

	fields := []interface{}{
		"last_fail", now.Unix(),
	}

	if int(newCount) >= b.threshold {
		fields = append(fields, "opened_at", now.Unix())
	}

	b.rc.HSet(ctx, b.key, fields...)
	b.rc.Set(ctx, b.key+":ttl", "1", 2*b.window)
}

func (b *Breaker) redisState() State {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	m, err := b.rc.HGetAll(ctx, b.key)
	if err != nil {
		return Closed
	}

	failures := atoi64(m["failures"])
	if int(failures) < b.threshold {
		return Closed
	}

	openedAt := atoi64(m["opened_at"])
	if openedAt > 0 && time.Since(time.Unix(openedAt, 0)) > b.cooldown {
		return HalfOpen
	}

	return Open
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
