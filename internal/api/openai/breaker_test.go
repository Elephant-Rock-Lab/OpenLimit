package openaiapi

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"openlimit/internal/circuit"
)

func TestBreakerMapCap(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &Handler{
		logger:   logger,
		breakers: make(map[string]*breakerEntry),
	}

	// Simulate what getBreaker does: insert entries and enforce cap.
	for i := 0; i < maxBreakers+100; i++ {
		// Generate unique keys to force new entries
		key := "provider-" + string(rune('A'+i%26)) + ":region-" + string(rune('A'+i%20)) + ":model-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26))
		h.breakers[key] = &breakerEntry{
			breaker:    circuit.NewBreaker(nil, "p", "m", logger),
			lastAccess: time.Now(),
		}
		// Evict oldest (LRU) if over cap
		for len(h.breakers) > maxBreakers {
			var oldestKey string
			var oldestTime time.Time
			first := true
			for k, e := range h.breakers {
				if first || e.lastAccess.Before(oldestTime) {
					oldestKey = k
					oldestTime = e.lastAccess
					first = false
				}
			}
			if oldestKey != "" {
				delete(h.breakers, oldestKey)
			}
		}
	}

	if len(h.breakers) > maxBreakers {
		t.Fatalf("expected at most %d breakers, got %d", maxBreakers, len(h.breakers))
	}
}
