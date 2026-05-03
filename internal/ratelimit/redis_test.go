package ratelimit

import (
	"context"
	"log/slog"
	"testing"
	"time"

	rediscli "openlimit/internal/redis"

	"github.com/alicebob/miniredis/v2"
)

func newTestRedisLimiter(t *testing.T, rpm, tpm int) (*RedisLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := rediscli.NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	return NewRedisLimiter(rc, "testkey", rpm, tpm), mr
}

func TestRedisLimiter_CheckRPM_Allowed(t *testing.T) {
	rl, _ := newTestRedisLimiter(t, 10, 0)
	allowed, limit, remaining, _ := rl.CheckRPM("testkey")
	if !allowed {
		t.Fatal("expected allowed")
	}
	if limit != 10 {
		t.Fatalf("expected limit 10, got %d", limit)
	}
	if remaining != 9 {
		t.Fatalf("expected remaining 9, got %d", remaining)
	}
}

func TestRedisLimiter_CheckRPM_RejectsAtLimit(t *testing.T) {
	rl, _ := newTestRedisLimiter(t, 3, 0)
	for i := 0; i < 3; i++ {
		allowed, _, _, _ := rl.CheckRPM("testkey")
		if !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	// 4th should be rejected
	allowed, _, remaining, _ := rl.CheckRPM("testkey")
	if allowed {
		t.Fatal("expected 4th request to be rejected")
	}
	if remaining != 0 {
		t.Fatalf("expected remaining 0, got %d", remaining)
	}
}

func TestRedisLimiter_CheckRPM_Unlimited(t *testing.T) {
	rl, _ := newTestRedisLimiter(t, 0, 0)
	allowed, limit, _, _ := rl.CheckRPM("testkey")
	if !allowed {
		t.Fatal("expected allowed for unlimited")
	}
	if limit != 0 {
		t.Fatalf("expected limit 0 for unlimited, got %d", limit)
	}
}

func TestRedisLimiter_CheckTPM_Allowed(t *testing.T) {
	rl, _ := newTestRedisLimiter(t, 0, 1000)
	allowed, limit, remaining, _ := rl.CheckTPM(context.Background(), 100)
	if !allowed {
		t.Fatal("expected allowed")
	}
	if limit != 1000 {
		t.Fatalf("expected limit 1000, got %d", limit)
	}
	if remaining != 900 {
		t.Fatalf("expected remaining 900, got %d", remaining)
	}
}

func TestRedisLimiter_CheckTPM_RejectsAtLimit(t *testing.T) {
	rl, _ := newTestRedisLimiter(t, 0, 100)
	// Use 80 tokens
	allowed, _, _, _ := rl.CheckTPM(context.Background(), 80)
	if !allowed {
		t.Fatal("first TPM check should be allowed")
	}
	// Try 30 more — should fail (only 20 left)
	allowed, _, remaining, _ := rl.CheckTPM(context.Background(), 30)
	if allowed {
		t.Fatal("expected rejection for exceeding TPM limit")
	}
	if remaining > 20 {
		t.Fatalf("expected remaining <= 20, got %d", remaining)
	}
}

func TestRedisLimiter_CheckTPM_Unlimited(t *testing.T) {
	rl, _ := newTestRedisLimiter(t, 0, 0)
	allowed, limit, _, _ := rl.CheckTPM(context.Background(), 99999)
	if !allowed {
		t.Fatal("expected allowed for unlimited TPM")
	}
	if limit != 0 {
		t.Fatalf("expected limit 0 for unlimited, got %d", limit)
	}
}

func TestRedisLimiter_ImplementsInterface(t *testing.T) {
	// Verify RedisLimiter satisfies RateLimiter interface
	var _ RateLimiter = (*RedisLimiter)(nil)
	// Verify Limiter satisfies RateLimiter interface
	var _ RateLimiter = (*Limiter)(nil)
}

func TestRedisLimiter_SlidingWindow(t *testing.T) {
	// This test verifies that entries within the same window accumulate.
	// Full sliding window expiry requires a real Redis instance since
	// miniredis FastForward doesn't affect Lua script time.Now().
	rl, _ := newTestRedisLimiter(t, 2, 0)

	// Use both requests
	rl.CheckRPM("testkey")
	rl.CheckRPM("testkey")

	// 3rd rejected
	allowed, _, _, _ := rl.CheckRPM("testkey")
	if allowed {
		t.Fatal("expected rejection at limit")
	}
}

func TestRedisLimiter_SeparateKeys(t *testing.T) {
	rl1, mr := newTestRedisLimiter(t, 1, 0)
	_ = mr

	rl2 := NewRedisLimiter(rl1.rc, "otherkey", 1, 0)

	// Use key1's limit
	allowed, _, _, _ := rl1.CheckRPM("testkey")
	if !allowed {
		t.Fatal("key1 first request should be allowed")
	}

	// key1 exhausted
	allowed, _, _, _ = rl1.CheckRPM("testkey")
	if allowed {
		t.Fatal("key1 second request should be rejected")
	}

	// key2 should still work
	allowed, _, _, _ = rl2.CheckRPM("otherkey")
	if !allowed {
		t.Fatal("key2 first request should be allowed")
	}
}
