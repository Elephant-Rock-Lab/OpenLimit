package ratelimit

import (
	"sync"
	"time"
)

// Limiter manages per-key rate limit buckets.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*Bucket
	rpm     int // requests per minute (0 = unlimited)
	tpm     int // tokens per minute (0 = unlimited)
}

// NewLimiter creates a new rate limiter with the given RPM and TPM limits.
// A limit of 0 means unlimited.
func NewLimiter(rpm, tpm int) *Limiter {
	return &Limiter{
		buckets: make(map[string]*Bucket),
		rpm:     rpm,
		tpm:     tpm,
	}
}

// CheckRPM checks if a request is allowed under the RPM limit.
// Returns (allowed, limit, remaining, resetAt).
func (l *Limiter) CheckRPM(keyID string) (bool, int, int, time.Time) {
	if l.rpm <= 0 {
		return true, 0, 0, time.Time{}
	}
	return l.getBucket(keyID, "rpm", l.rpm, time.Minute).Allow()
}

// CheckTPM checks if a token count is allowed under the TPM limit.
// This is called after the response is received with actual token counts.
func (l *Limiter) CheckTPM(keyID string, tokenCount int) (bool, int, int, time.Time) {
	if l.tpm <= 0 {
		return true, 0, 0, time.Time{}
	}

	b := l.getBucket(keyID, "tpm", l.tpm, time.Minute)
	if b == nil {
		return true, 0, 0, time.Time{}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill)
	if elapsed >= b.window {
		b.tokens = b.limit
		b.lastRefill = now
	}

	if b.tokens >= tokenCount {
		b.tokens -= tokenCount
		return true, b.limit, b.tokens, b.lastRefill.Add(b.window)
	}

	return false, b.limit, b.tokens, b.lastRefill.Add(b.window)
}

func (l *Limiter) getBucket(keyID, limitType string, limit int, window time.Duration) *Bucket {
	l.mu.Lock()
	defer l.mu.Unlock()

	k := keyID + ":" + limitType
	b, ok := l.buckets[k]
	if !ok {
		b = NewBucket(limit, window)
		l.buckets[k] = b
	}
	return b
}
