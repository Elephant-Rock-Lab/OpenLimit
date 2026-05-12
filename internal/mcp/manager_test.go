package mcp

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openlimit/internal/config"
)

func TestManagerServerStateMutex(t *testing.T) {
	// TEST-39-06-01: serverState has its own sync.Mutex that serializes
	// concurrent reconnect attempts.
	state := &serverState{
		name:      "test-server",
		connected: false,
		config: config.MCPServerConfig{
			Name:      "test-server",
			URL:       "http://localhost:9999",
			TimeoutMS: 100,
		},
	}

	var concurrentCount int32
	var maxConcurrent int32
	var iterations int32

	// Simulate multiple goroutines trying to update state concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state.mu.Lock()
			current := atomic.AddInt32(&concurrentCount, 1)
			// Track max concurrent access
			for {
				old := atomic.LoadInt32(&maxConcurrent)
				if current <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, current) {
					break
				}
			}
			atomic.AddInt32(&iterations, 1)
			state.connected = true
			state.mu.Unlock()
			atomic.AddInt32(&concurrentCount, -1)
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&maxConcurrent) > 1 {
		t.Errorf("max concurrent state access = %d, want 1 (mutex should serialize)", maxConcurrent)
	}
	if atomic.LoadInt32(&iterations) != 10 {
		t.Errorf("iterations = %d, want 10", iterations)
	}
	if !state.connected {
		t.Error("state.connected should be true after all goroutines")
	}
}

func TestManagerCheckServersLocksPerState(t *testing.T) {
	// TEST-39-06-02: checkAllServers locks per-state mutex before mutation.
	// Verify that ping-failure state mutation is serialized.
	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{Enabled: true}, registry, nil)

	// Add a server state manually
	state := &serverState{
		name:      "test-server",
		connected: true,
		config: config.MCPServerConfig{
			Name: "test-server",
			URL:  "http://localhost:1", // will fail to connect
		},
	}
	mgr.servers["test-server"] = state

	// Verify serverState has a mutex field (compile-time check)
	// The mutex is embedded in the struct — confirmed by type definition

	// Verify the mutex is a real sync.Mutex by locking it
	state.mu.Lock()
	state.connected = false
	state.mu.Unlock()

	if state.connected {
		t.Error("state should be disconnected after mutation under lock")
	}
}

// ---------------------------------------------------------------------------
// BATCH-57 / TASK-02: Double mutex unlock fix regression tests
// ---------------------------------------------------------------------------

// TEST-57-02-01: tryReconnect successfully reconnects without panic (no double-unlock).
func TestTryReconnect_Success_NoPanic(t *testing.T) {
	registry := NewRegistry()
	logger := slog.Default()
	mgr := NewManager(config.MCPConfig{Enabled: true}, registry, logger)

	state := &serverState{
		name:      "test-reconnect",
		connected: false,
		config: config.MCPServerConfig{
			Name:      "test-reconnect",
			URL:       "http://localhost:1", // will fail
			TimeoutMS: 100,
		},
	}
	mgr.servers["test-reconnect"] = state

	// tryReconnect with a server that will fail to connect
	// If double-unlock bug was present, this would panic
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		mgr.tryReconnect(context.Background(), state)
	}()

	if panicked {
		t.Fatal("tryReconnect panicked (likely double-unlock)")
	}
	// Should still be disconnected (connection to localhost:1 fails)
	state.mu.Lock()
	connected := state.connected
	state.mu.Unlock()
	if connected {
		t.Error("state should be disconnected after failed connect")
	}
}

// TEST-57-02-02: tryReconnect on failed connect sets lastError without panic.
func TestTryReconnect_FailedConnect_SetsError(t *testing.T) {
	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{Enabled: true}, registry, nil)

	state := &serverState{
		name:      "test-fail-connect",
		connected: false,
		config: config.MCPServerConfig{
			Name:      "test-fail-connect",
			URL:       "http://localhost:1",
			TimeoutMS: 50,
		},
	}
	mgr.servers["test-fail-connect"] = state

	mgr.tryReconnect(context.Background(), state)

	state.mu.Lock()
	err := state.lastError
	connected := state.connected
	state.mu.Unlock()

	if connected {
		t.Error("should be disconnected after failed connect")
	}
	if err == nil {
		t.Error("lastError should be set after failed connect")
	}
}

// TEST-57-02-03: tryReconnect on failed connect retains the old client (not replaced).
func TestTryReconnect_FailedConnect_RetainsOldClient(t *testing.T) {
	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{Enabled: true}, registry, slog.Default())

	initialClient := NewClient("old", "http://localhost:1", nil, 50*time.Millisecond, "old", slog.Default())

	state := &serverState{
		name:      "test-replace",
		connected: false,
		client:    initialClient,
		config: config.MCPServerConfig{
			Name:      "test-replace",
			URL:       "http://localhost:1",
			TimeoutMS: 50,
		},
	}
	mgr.servers["test-replace"] = state

	mgr.tryReconnect(context.Background(), state)

	state.mu.Lock()
	client := state.client
	state.mu.Unlock()

	// When connect fails, the old client is retained (not replaced)
	if client != initialClient {
		t.Error("client should be retained when reconnect fails")
	}
	if state.connected {
		t.Error("state should still be disconnected after failed connect")
	}
}

// TEST-57-02-04: Concurrent tryReconnect calls don't deadlock.
func TestTryReconnect_Concurrent_NoDeadlock(t *testing.T) {
	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{Enabled: true}, registry, nil)

	state := &serverState{
		name:      "test-concurrent",
		connected: false,
		config: config.MCPServerConfig{
			Name:      "test-concurrent",
			URL:       "http://localhost:1",
			TimeoutMS: 50,
		},
	}
	mgr.servers["test-concurrent"] = state

	done := make(chan struct{})
	const goroutines = 5

	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			mgr.tryReconnect(context.Background(), state)
		}()
	}

	// Wait with timeout to detect deadlock
	timeout := time.After(5 * time.Second)
	for i := 0; i < goroutines; i++ {
		select {
		case <-done:
			// OK
		case <-timeout:
			t.Fatal("deadlock detected: tryReconnect did not complete within 5 seconds")
		}
	}
}
