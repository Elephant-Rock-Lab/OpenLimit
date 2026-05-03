package ratelimit

import (
	"sync"
	"time"
)

// Bucket is a token bucket rate limiter.
// It allows up to `limit` requests per `window` duration.
type Bucket struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	tokens     int
	lastRefill time.Time
}

// NewBucket creates a token bucket with the given limit per window.
func NewBucket(limit int, window time.Duration) *Bucket {
	if limit <= 0 {
		return nil // unlimited
	}
	return &Bucket{
		limit:      limit,
		window:     window,
		tokens:     limit,
		lastRefill: time.Now(),
	}
}

// Allow tries to consume one token. Returns true if allowed, false if rate limited.
// Also returns the current limit and remaining tokens for response headers.
func (b *Bucket) Allow() (allowed bool, limit int, remaining int, resetAt time.Time) {
	if b == nil {
		return true, 0, 0, time.Time{}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill)

	// Refill tokens based on elapsed time
	if elapsed >= b.window {
		b.tokens = b.limit
		b.lastRefill = now
	} else {
		// Partial refill: proportional tokens recovered
		refill := int(elapsed.Seconds() * float64(b.limit) / b.window.Seconds())
		if refill > 0 {
			b.tokens += refill
			if b.tokens > b.limit {
				b.tokens = b.limit
			}
			b.lastRefill = now
		}
	}

	limit = b.limit
	resetAt = b.lastRefill.Add(b.window)
	remaining = b.tokens - 1

	if b.tokens > 0 {
		b.tokens--
		remaining = b.tokens
		return true, limit, remaining, resetAt
	}

	remaining = 0
	return false, limit, 0, resetAt
}
