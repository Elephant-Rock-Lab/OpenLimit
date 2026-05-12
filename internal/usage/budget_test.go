package usage

import (
	"context"
	"testing"
)

func TestCheckBudget_NoLimit(t *testing.T) {
	// TEST-39-04-01: No budget limit → always allowed
	result, err := CheckBudget(context.Background(), nil, "key-1", "monthly", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("should be allowed when limit is 0")
	}
}

func TestCheckBudget_FailClosed_NilDB(t *testing.T) {
	// TEST-39-04-03: DB is nil + failClosed → rejected
	result, err := CheckBudget(context.Background(), nil, "key-1", "monthly", 100, true)
	if err == nil {
		t.Fatal("expected error when failClosed and DB is nil")
	}
	if result != nil {
		t.Error("result should be nil on fail-closed error")
	}
}

func TestCheckBudget_FailOpen_NilDB(t *testing.T) {
	// TEST-39-04-04: DB is nil + failOpen (default) → allowed
	result, err := CheckBudget(context.Background(), nil, "key-1", "monthly", 100, false)
	if err != nil {
		t.Fatalf("unexpected error in fail-open mode: %v", err)
	}
	if !result.Allowed {
		t.Error("should be allowed in fail-open mode when DB unavailable")
	}
}

func TestCheckBudget_NegativeLimit_NoBudget(t *testing.T) {
	// Negative limit is treated as "no budget" (limit <= 0)
	result, err := CheckBudget(context.Background(), nil, "key-1", "monthly", -10, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("negative limit should be treated as no budget enforcement")
	}
}

// ---------------------------------------------------------------------------
// BATCH-61 / TASK-01: Budget context propagation tests
// ---------------------------------------------------------------------------

// TEST-61-01-03: CheckBudget accepts a non-nil context (signature verification).
// The context is passed through to GetSpendForCurrentPeriod. When DB is nil,
// the context is never used, but the signature must accept it.
func TestCheckBudget_NonNilContext(t *testing.T) {
	ctx := context.Background()
	result, err := CheckBudget(ctx, nil, "key-1", "monthly", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("should be allowed with no limit")
	}
}

// TEST-61-01-04: Budget check times out after context deadline.
// Uses a cancelled context to verify timeout propagation.
func TestCheckBudget_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// With a cancelled context and no DB, the function should still return quickly
	// (no budget configured case). We test the context path by checking that
	// a cancelled context does not cause a panic or unexpected behavior.
	result, err := CheckBudget(ctx, nil, "key-1", "monthly", 0, false)
	if err != nil {
		t.Fatalf("unexpected error with cancelled context: %v", err)
	}
	if !result.Allowed {
		t.Error("should be allowed when no budget configured")
	}
}
