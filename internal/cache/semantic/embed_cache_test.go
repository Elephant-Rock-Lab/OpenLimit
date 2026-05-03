package semantic

import (
	"testing"
	"time"
)

func TestEmbeddingCache_Basic(t *testing.T) {
	cache := NewEmbeddingCache(100, time.Hour)

	// Miss
	if got := cache.Get("hello"); got != nil {
		t.Error("expected nil on miss")
	}

	// Set and get
	vec := []float32{0.1, 0.2, 0.3}
	cache.Set("hello", vec)

	got := cache.Get("hello")
	if got == nil {
		t.Fatal("expected cache hit")
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 dimensions, got %d", len(got))
	}
	if got[0] != 0.1 || got[1] != 0.2 || got[2] != 0.3 {
		t.Errorf("unexpected values: %v", got)
	}
}

func TestEmbeddingCache_TTLExpiration(t *testing.T) {
	cache := NewEmbeddingCache(100, 10*time.Millisecond)

	vec := []float32{0.5}
	cache.Set("test", vec)

	// Immediate get should work
	if got := cache.Get("test"); got == nil {
		t.Fatal("expected cache hit")
	}

	// Wait for TTL
	time.Sleep(20 * time.Millisecond)
	if got := cache.Get("test"); got != nil {
		t.Error("expected nil after TTL expiration")
	}
}

func TestEmbeddingCache_Eviction(t *testing.T) {
	cache := NewEmbeddingCache(3, time.Hour)

	for i := 0; i < 5; i++ {
		vec := []float32{float32(i)}
		cache.Set(string(rune('a'+i)), vec)
	}

	// First two should be evicted
	if cache.Get("a") != nil {
		t.Error("expected 'a' to be evicted")
	}
	if cache.Get("b") != nil {
		t.Error("expected 'b' to be evicted")
	}

	// Last three should remain
	if cache.Get("c") == nil {
		t.Error("expected 'c' to exist")
	}
	if cache.Get("d") == nil {
		t.Error("expected 'd' to exist")
	}
	if cache.Get("e") == nil {
		t.Error("expected 'e' to exist")
	}
}

func TestEmbeddingCache_Update(t *testing.T) {
	cache := NewEmbeddingCache(10, time.Hour)

	cache.Set("key", []float32{1.0})
	cache.Set("key", []float32{2.0})

	got := cache.Get("key")
	if got == nil || got[0] != 2.0 {
		t.Errorf("expected updated value 2.0, got %v", got)
	}
}

func TestEmbeddingCache_Defaults(t *testing.T) {
	cache := NewEmbeddingCache(0, 0)
	if cache.max != 5000 {
		t.Errorf("expected default max 5000, got %d", cache.max)
	}
	if cache.ttl != time.Hour {
		t.Errorf("expected default TTL 1h, got %v", cache.ttl)
	}
}
