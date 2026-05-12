package providers

import (
	"math"
	"testing"
)

func TestKeyRingNextWrapsSafely(t *testing.T) {
	kr := &KeyRing{
		keys: []ProviderKey{
			{ID: "key-a", Value: "val-a"},
			{ID: "key-b", Value: "val-b"},
		},
		next: math.MaxUint64 - 1,
	}

	for i := 0; i < 3; i++ {
		key, ok := kr.Next()
		if !ok {
			t.Fatalf("call %d: expected ok=true", i)
		}
		if key.ID != "key-a" && key.ID != "key-b" {
			t.Fatalf("call %d: unexpected key ID %q", i, key.ID)
		}
	}

	// Verify the counter wrapped past MaxUint64 without panicking
	if kr.next != 1 { // started at MaxUint64-1, incremented 3 times → wraps to 1
		t.Errorf("expected next=1 after wrap, got %d", kr.next)
	}
}
