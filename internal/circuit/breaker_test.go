package circuit

import (
	"context"
	"log/slog"
	"testing"
	"time"

	rediscli "openlimit/internal/redis"

	"github.com/alicebob/miniredis/v2"
)

func newTestBreaker(t *testing.T, threshold int) (*Breaker, *rediscli.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := rediscli.NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	b := NewBreaker(rc, "openai", "gpt-4o", slog.Default(),
		WithThreshold(threshold),
		WithWindow(30*time.Second),
		WithCooldown(60*time.Second),
	)
	return b, rc, mr
}

func newTestLocalBreaker(threshold int) *Breaker {
	return NewBreaker(nil, "openai", "gpt-4o", slog.Default(),
		WithThreshold(threshold),
		WithWindow(30*time.Second),
		WithCooldown(60*time.Second),
	)
}

// --- Redis-backed tests ---

func TestRedisBreaker_StartsClosed(t *testing.T) {
	b, _, _ := newTestBreaker(t, 3)
	if b.State() != Closed {
		t.Fatal("expected closed initially")
	}
	if !b.Allow() {
		t.Fatal("expected allow when closed")
	}
}

func TestRedisBreaker_OpensAfterThreshold(t *testing.T) {
	b, _, _ := newTestBreaker(t, 3)
	for i := 0; i < 3; i++ {
		b.RecordFailure()
	}
	if b.State() != Open {
		t.Fatalf("expected open after %d failures, got %s", 3, b.State())
	}
	if b.Allow() {
		t.Fatal("expected reject when open")
	}
}

func TestRedisBreaker_SuccessResets(t *testing.T) {
	b, _, _ := newTestBreaker(t, 3)
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess()
	if b.State() != Closed {
		t.Fatal("expected closed after success")
	}
	// Failures should be reset, need 3 more to open
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != Closed {
		t.Fatal("expected still closed with only 2 failures after reset")
	}
}

func TestRedisBreaker_CooldownTransition(t *testing.T) {
	// This test verifies that cooldown logic works by directly manipulating
	// the Redis hash. miniredis FastForward doesn't affect Go's time.Now(),
	// so we can't use it to test real-time cooldown. Instead, we set an
	// old opened_at timestamp directly.
	b, rc, _ := newTestBreaker(t, 2)
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != Open {
		t.Fatal("expected open")
	}

	// Manually set opened_at to 61 seconds ago
	oldTime := time.Now().Add(-61 * time.Second).Unix()
	ctx := context.Background()
	rc.HSet(ctx, b.key, "opened_at", oldTime)

	if b.State() != HalfOpen {
		t.Fatalf("expected half-open after cooldown, got %s", b.State())
	}
	if !b.Allow() {
		t.Fatal("expected allow in half-open")
	}
}

func TestRedisBreaker_SeparateProviders(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := rediscli.NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)

	b1 := NewBreaker(rc, "openai", "gpt-4o", slog.Default(), WithThreshold(2))
	b2 := NewBreaker(rc, "anthropic", "claude-3", slog.Default(), WithThreshold(2))

	b1.RecordFailure()
	b1.RecordFailure()
	if b1.State() != Open {
		t.Fatal("openai should be open")
	}
	if b2.State() != Closed {
		t.Fatal("anthropic should be closed — separate breaker")
	}
}

// --- Local fallback tests ---

func TestLocalBreaker_StartsClosed(t *testing.T) {
	b := newTestLocalBreaker(3)
	if b.State() != Closed {
		t.Fatal("expected closed")
	}
	if !b.Allow() {
		t.Fatal("expected allow when closed")
	}
}

func TestLocalBreaker_OpensAfterThreshold(t *testing.T) {
	b := newTestLocalBreaker(3)
	for i := 0; i < 3; i++ {
		b.RecordFailure()
	}
	if b.State() != Open {
		t.Fatalf("expected open, got %s", b.State())
	}
	if b.Allow() {
		t.Fatal("expected reject when open")
	}
}

func TestLocalBreaker_SuccessResets(t *testing.T) {
	b := newTestLocalBreaker(3)
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess()
	if b.State() != Closed {
		t.Fatal("expected closed after success reset")
	}
}

func TestLocalBreaker_WindowExpiry(t *testing.T) {
	b := newTestLocalBreaker(2)
	b.RecordFailure()
	// Manually set lastFail to be outside the window
	b.mu.Lock()
	b.lastFail = time.Now().Add(-31 * time.Second)
	b.mu.Unlock()

	// This failure should reset the count (outside window) then increment to 1
	b.RecordFailure()
	if b.State() != Closed {
		t.Fatal("expected closed — only 1 failure in window after expiry")
	}
}

func TestBreaker_RedisDegradedFallsToLocal(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := rediscli.NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)

	b := NewBreaker(rc, "openai", "gpt-4o", slog.Default(), WithThreshold(2))

	// Kill Redis
	mr.Close()

	// Give health check time to detect (or force it)
	rc.SetHealthy(false)

	// Should still work in local mode
	if !b.Allow() {
		t.Fatal("expected allow in local mode")
	}
	b.RecordFailure()
	b.RecordFailure()
	if b.Allow() {
		t.Fatal("expected reject in local mode after threshold")
	}
}
