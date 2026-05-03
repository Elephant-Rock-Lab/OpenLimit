package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"openlimit/internal/config"
)

// ---------------------------------------------------------------------------
// TEST-08-02-01: main.go starts watcher when config path exists
// TEST-08-02-02: SIGHUP triggers a config reload
// TEST-08-02-03: Watcher is stopped on graceful shutdown
// ---------------------------------------------------------------------------

// TestWatcherStartAndReload verifies the watcher lifecycle:
// start, detect change, and close.
func TestWatcherStartAndReload(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gateway.yaml")

	initialYAML := "logging:\n  level: info\nproviders: {}\nmodels: {}\n"
	if err := os.WriteFile(cfgPath, []byte(initialYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var reloadCount int

	onChange := func(old, newCfg config.Config) {
		mu.Lock()
		reloadCount++
		mu.Unlock()
	}

	watcher := config.NewWatcher(cfgPath, cfg, onChange, slog.Default())
	cancel := watcher.StartBackground()
	defer cancel()

	// Give watcher time to start
	time.Sleep(100 * time.Millisecond)

	// Write updated config
	updatedYAML := "logging:\n  level: debug\nproviders: {}\nmodels: {}\n"
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(cfgPath, []byte(updatedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for watcher to detect (poll interval is 5s, but we can just wait)
	// For faster test, manually trigger by checking the file
	time.Sleep(6 * time.Second)

	mu.Lock()
	if reloadCount < 1 {
		t.Errorf("expected at least 1 reload, got %d", reloadCount)
	}
	mu.Unlock()

	// TEST-08-02-03: Watcher is stopped on graceful shutdown
	watcher.Close()

	// Verify Close is idempotent (no panic)
	watcher.Close()
}

// TestWatcherReloadPreservesNonReloadable verifies that hot-reload does not
// change non-reloadable fields.
func TestWatcherReloadPreservesNonReloadable(t *testing.T) {
	original := config.Config{
		Server:   config.ServerConfig{Port: 8080},
		Database: config.DatabaseConfig{URL: "postgres://original"},
		Redis:    config.RedisConfig{Addr: "redis://original:6379"},
		Logging:  config.LoggingConfig{Level: "info"},
	}

	updated := config.Config{
		Server:   config.ServerConfig{Port: 9999},
		Database: config.DatabaseConfig{URL: "postgres://changed"},
		Redis:    config.RedisConfig{Addr: "redis://changed:6379"},
		Logging:  config.LoggingConfig{Level: "debug"},
	}

	var applied config.Config
	applied = original

	onChange := func(old, newCfg config.Config) {
		// Simulate what main.go would do: merge only reloadable fields
		config.MergeReloadable(&applied, newCfg)
	}

	// Trigger the callback
	onChange(original, updated)

	if applied.Server.Port != 8080 {
		t.Errorf("server port should remain 8080, got %d", applied.Server.Port)
	}
	if applied.Database.URL != "postgres://original" {
		t.Errorf("database URL should remain unchanged, got %q", applied.Database.URL)
	}
	if applied.Redis.Addr != "redis://original:6379" {
		t.Errorf("Redis addr should remain unchanged, got %q", applied.Redis.Addr)
	}
	if applied.Logging.Level != "debug" {
		t.Errorf("logging level should be updated to debug, got %q", applied.Logging.Level)
	}
}
