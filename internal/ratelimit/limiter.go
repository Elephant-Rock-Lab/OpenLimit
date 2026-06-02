package ratelimit

import (
	"sync"
	"time"
)

// bucketEntry wraps a Bucket with last-access metadata for eviction.
type bucketEntry struct {
	bucket     *Bucket
	lastAccess time.Time
}

// Limiter manages per-key rate limit buckets.
type Limiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucketEntry
	rpm        int // requests per minute (0 = unlimited)
	tpm        int // tokens per minute (0 = unlimited)
	maxEntries int
	stopCh     chan struct{}
}

// NewLimiter creates a new rate limiter with the given RPM and TPM limits.
// A limit of 0 means unlimited.
func NewLimiter(rpm, tpm int) *Limiter {
	l := &Limiter{
		buckets:    make(map[string]*bucketEntry),
		rpm:        rpm,
		tpm:        tpm,
		maxEntries: 10000,
		stopCh:     make(chan struct{}),
	}
	go l.evictLoop()
	return l
}

// Close stops the background eviction goroutine.
func (l *Limiter) Close() {
	close(l.stopCh)
}

// evictLoop runs a background goroutine that evicts stale entries every 60 seconds.
func (l *Limiter) evictLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.evictStale()
		}
	}
}

// evictStale removes entries not accessed in the last 10 minutes.
func (l *Limiter) evictStale() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, entry := range l.buckets {
		if now.Sub(entry.lastAccess) > 10*time.Minute {
			delete(l.buckets, k)
		}
	}
}

// evictOldest removes the oldest-accessed entry when the map is at capacity.
func (l *Limiter) evictOldest() {
	// Caller must hold l.mu.
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, entry := range l.buckets {
		if first || entry.lastAccess.Before(oldestTime) {
			oldestKey = k
			oldestTime = entry.lastAccess
			first = false
		}
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
}

// CheckRPM checks if a request is allowed under the RPM limit.
// Returns (allowed, limit, remaining, resetAt).
func (l *Limiter) CheckRPM(keyID string) (bool, int, int, time.Time) {
	if l.rpm <= 0 {
		return true, 0, 0, time.Time{}
	}
	return l.getBucket(keyID, "rpm", l.rpm, time.Minute).bucket.Allow()
}

// CheckTPM checks if a token count is allowed under the TPM limit.
// This is called after the response is received with actual token counts.
func (l *Limiter) CheckTPM(keyID string, tokenCount int) (bool, int, int, time.Time) {
	if l.tpm <= 0 {
		return true, 0, 0, time.Time{}
	}

	entry := l.getBucket(keyID, "tpm", l.tpm, time.Minute)
	b := entry.bucket
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

func (l *Limiter) getBucket(keyID, limitType string, limit int, window time.Duration) *bucketEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	k := keyID + ":" + limitType
	entry, ok := l.buckets[k]
	if !ok {
		// Enforce maxEntries: evict oldest if at capacity
		if len(l.buckets) >= l.maxEntries {
			l.evictOldest()
		}
		entry = &bucketEntry{
			bucket:     NewBucket(limit, window),
			lastAccess: time.Now(),
		}
		l.buckets[k] = entry
	} else {
		entry.lastAccess = time.Now()
	}
	return entry
}
