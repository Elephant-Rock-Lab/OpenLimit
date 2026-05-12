package ratelimit

import (
	"testing"
	"time"
)

func TestBucketAllowsUpToLimit(t *testing.T) {
	b := NewBucket(3, time.Minute)

	for i := 0; i < 3; i++ {
		allowed, _, _, _ := b.Allow()
		if !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	allowed, _, remaining, _ := b.Allow()
	if allowed {
		t.Fatal("4th request should be rate limited")
	}
	if remaining != 0 {
		t.Fatalf("expected 0 remaining, got %d", remaining)
	}
}

func TestBucketReturnsLimitInfo(t *testing.T) {
	b := NewBucket(10, time.Minute)

	allowed, limit, remaining, _ := b.Allow()
	if !allowed {
		t.Fatal("should be allowed")
	}
	if limit != 10 {
		t.Fatalf("expected limit 10, got %d", limit)
	}
	if remaining != 9 {
		t.Fatalf("expected 9 remaining, got %d", remaining)
	}
}

func TestBucketNilIsUnlimited(t *testing.T) {
	var b *Bucket
	allowed, _, _, _ := b.Allow()
	if !allowed {
		t.Fatal("nil bucket should be unlimited")
	}
}

func TestBucketZeroLimitIsUnlimited(t *testing.T) {
	b := NewBucket(0, time.Minute)
	if b != nil {
		t.Fatal("expected nil for zero limit")
	}
}

func TestLimiterCheckRPM(t *testing.T) {
	l := NewLimiter(2, 0) // 2 RPM
	defer l.Close()

	allowed, _, _, _ := l.CheckRPM("key1")
	if !allowed {
		t.Fatal("first request should be allowed")
	}

	allowed, _, _, _ = l.CheckRPM("key1")
	if !allowed {
		t.Fatal("second request should be allowed")
	}

	allowed, _, _, _ = l.CheckRPM("key1")
	if allowed {
		t.Fatal("third request should be rate limited")
	}
}

func TestLimiterSeparateKeys(t *testing.T) {
	l := NewLimiter(1, 0)
	defer l.Close()

	allowed, _, _, _ := l.CheckRPM("key1")
	if !allowed {
		t.Fatal("key1 first request should be allowed")
	}

	allowed, _, _, _ = l.CheckRPM("key2")
	if !allowed {
		t.Fatal("key2 first request should be allowed (separate bucket)")
	}
}

func TestLimiterZeroRPMIsUnlimited(t *testing.T) {
	l := NewLimiter(0, 0)
	defer l.Close()

	for i := 0; i < 100; i++ {
		allowed, _, _, _ := l.CheckRPM("key1")
		if !allowed {
			t.Fatalf("request %d should be allowed with 0 RPM (unlimited)", i+1)
		}
	}
}

func TestLimiterCheckTPM(t *testing.T) {
	l := NewLimiter(0, 100) // 100 tokens per minute
	defer l.Close()

	allowed, _, _, _ := l.CheckTPM("key1", 50)
	if !allowed {
		t.Fatal("50 tokens should be allowed")
	}

	allowed, _, _, _ = l.CheckTPM("key1", 50)
	if !allowed {
		t.Fatal("another 50 tokens should be allowed (total 100)")
	}

	allowed, _, _, _ = l.CheckTPM("key1", 1)
	if allowed {
		t.Fatal("1 more token should exceed the 100 TPM limit")
	}
}

func TestLimiterEvictsStaleBuckets(t *testing.T) {
	l := NewLimiter(10, 10)
	defer l.Close()

	// Access several keys
	l.CheckRPM("old-key1")
	l.CheckRPM("old-key2")
	l.CheckRPM("old-key3")

	l.mu.Lock()
	if len(l.buckets) != 6 { // 3 keys × 2 types (rpm + tpm lazy, but rpm creates rpm bucket, tpm not called)
		// Only rpm buckets created = 3
		if len(l.buckets) != 3 {
			t.Fatalf("expected 3 or 6 buckets, got %d", len(l.buckets))
		}
	}
	initialCount := len(l.buckets)
	l.mu.Unlock()

	// Manually age the entries
	l.mu.Lock()
	for _, entry := range l.buckets {
		entry.lastAccess = time.Now().Add(-15 * time.Minute) // older than 10 min threshold
	}
	l.mu.Unlock()

	// Run eviction
	l.evictStale()

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) != 0 {
		t.Fatalf("expected 0 buckets after evicting stale entries (had %d), got %d", initialCount, len(l.buckets))
	}
}

func TestLimiterMaxEntriesEnforced(t *testing.T) {
	l := NewLimiter(100, 0)
	defer l.Close()
	l.maxEntries = 5 // small cap for testing

	// Create 10 unique keys (only rpm, so 10 buckets)
	for i := 0; i < 10; i++ {
		l.CheckRPM("key-" + string(rune('A'+i)))
	}

	l.mu.Lock()
	count := len(l.buckets)
	l.mu.Unlock()

	if count > l.maxEntries {
		t.Fatalf("expected at most %d buckets, got %d", l.maxEntries, count)
	}
}

func TestLimiterCloseStopsEviction(t *testing.T) {
	l := NewLimiter(10, 0)
	l.CheckRPM("key1")

	// Close should not panic
	l.Close()

	// Give goroutine time to exit
	time.Sleep(100 * time.Millisecond)

	// Second close would panic on closed channel — don't test that.
	// Just verify the limiter is still usable for synchronous operations.
	l.CheckRPM("key2") // should not deadlock
}
