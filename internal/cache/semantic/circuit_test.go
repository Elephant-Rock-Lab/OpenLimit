package semantic

import (
	"testing"
	"time"
)

func TestCircuitBreaker_Closed(t *testing.T) {
	cb := NewCircuitBreaker()
	if !cb.Allow() {
		t.Error("circuit should be closed initially")
	}
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	cb := NewCircuitBreaker()

	// Record threshold failures
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.Allow() {
		t.Error("circuit should be open after 3 failures")
	}
	if cb.State() != "open" {
		t.Errorf("expected state 'open', got %q", cb.State())
	}
}

func TestCircuitBreaker_SuccessResets(t *testing.T) {
	cb := NewCircuitBreaker()

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // Resets
	cb.RecordFailure()

	if !cb.Allow() {
		t.Error("circuit should be closed after success reset (only 1 failure)")
	}
}

func TestCircuitBreaker_CooldownExpires(t *testing.T) {
	cb := NewCircuitBreaker()
	// Override for testing
	cb.cooldown = 50 * time.Millisecond

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.Allow() {
		t.Error("circuit should be open")
	}

	time.Sleep(60 * time.Millisecond)

	if !cb.Allow() {
		t.Error("circuit should be half-open after cooldown")
	}
}
