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

	for i := 0; i < 100; i++ {
		allowed, _, _, _ := l.CheckRPM("key1")
		if !allowed {
			t.Fatalf("request %d should be allowed with 0 RPM (unlimited)", i+1)
		}
	}
}

func TestLimiterCheckTPM(t *testing.T) {
	l := NewLimiter(0, 100) // 100 tokens per minute

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
