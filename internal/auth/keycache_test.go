package auth

import (
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// BATCH-58 / TASK-03: KeyCache LRU Eviction tests
// ---------------------------------------------------------------------------

// TEST-58-03-01: Most-recently-accessed entry survives eviction.
// When the cache is at capacity and a new entry is inserted,
// the entry that was accessed most recently should NOT be evicted.
func TestKeyCache_MRU_SurvivesEviction(t *testing.T) {
	cache := NewKeyCache(3, 60*time.Second) // max 3 entries

	// Fill cache with 3 entries
	cache.Set("key1", &Context{VirtualKeyID: "v1"})
	cache.Set("key2", &Context{VirtualKeyID: "v2"})
	cache.Set("key3", &Context{VirtualKeyID: "v3"})

	// Access key2 to make it the most recently used
	time.Sleep(time.Millisecond) // ensure lastAccess differs
	ctx, ok := cache.Get("key2")
	if !ok || ctx.VirtualKeyID != "v2" {
		t.Fatal("expected key2 to be retrievable")
	}

	// Insert a 4th entry to trigger eviction — key2 should survive
	cache.Set("key4", &Context{VirtualKeyID: "v4"})

	// key2 should still be present (was accessed most recently)
	_, ok = cache.Get("key2")
	if !ok {
		t.Error("key2 (most recently accessed) should survive eviction")
	}

	// key4 should be present (just inserted)
	_, ok = cache.Get("key4")
	if !ok {
		t.Error("key4 (just inserted) should be present")
	}
}

// TEST-58-03-02: Oldest-accessed entry is evicted first.
// The entry with the oldest lastAccess should be removed when capacity is exceeded.
func TestKeyCache_OldestEvictedFirst(t *testing.T) {
	cache := NewKeyCache(3, 60*time.Second) // max 3 entries

	// Fill cache with 3 entries
	cache.Set("oldest", &Context{VirtualKeyID: "v_old"})
	time.Sleep(time.Millisecond) // ensure time difference
	cache.Set("middle", &Context{VirtualKeyID: "v_mid"})
	time.Sleep(time.Millisecond)
	cache.Set("newest", &Context{VirtualKeyID: "v_new"})

	// Insert a 4th entry to trigger eviction
	cache.Set("trigger", &Context{VirtualKeyID: "v_trig"})

	// "oldest" should have been evicted (it was set first and never accessed via Get)
	_, okOld := cache.Get("oldest")
	if okOld {
		t.Error("oldest entry should have been evicted")
	}

	// "middle" and "newest" should still be present
	_, okMid := cache.Get("middle")
	if !okMid {
		t.Error("middle entry should still be present")
	}
	_, okNew := cache.Get("newest")
	if !okNew {
		t.Error("newest entry should still be present")
	}
	_, okTrig := cache.Get("trigger")
	if !okTrig {
		t.Error("trigger entry should be present")
	}
}

// TEST-58-03-03: Concurrent Get and Set don't race.
// Run with -race flag to verify zero data races on the lastAccess field.
func TestKeyCache_ConcurrentGetSet_NoRace(t *testing.T) {
	cache := NewKeyCache(100, 60*time.Second)

	// Pre-populate
	for i := 0; i < 50; i++ {
		cache.Set(string(rune('a'+i)), &Context{VirtualKeyID: "v"})
	}

	var wg sync.WaitGroup
	const goroutines = 20
	const iterations = 100

	// Concurrent readers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := string(rune('a' + (idx+j)%50))
				cache.Get(key)
			}
		}(i)
	}

	// Concurrent writers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := string(rune('A' + idx%26))
				cache.Set(key, &Context{VirtualKeyID: "concurrent"})
			}
		}(i)
	}

	wg.Wait()
	// If this test completes without -race flag firing, it passes.
}
