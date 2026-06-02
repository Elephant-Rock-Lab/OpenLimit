package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TEST-08-01-01: Watcher detects file modification and reloads config
// ---------------------------------------------------------------------------

func TestWatcher_DetectsFileModification(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")

	// Write initial config
	initialYAML := `
logging:
  level: info
providers: {}
models: {}
`
	if err := os.WriteFile(cfgPath, []byte(initialYAML), 0644); err != nil {
		t.Fatal(err)
	}

	initial, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var reloadCount int
	var lastLevel string

	onChange := func(old, new Config) {
		mu.Lock()
		defer mu.Unlock()
		reloadCount++
		lastLevel = new.Logging.Level
	}

	w := NewWatcher(cfgPath, initial, onChange, slog.Default())

	// Get the initial mod time (after first write)
	initialInfo, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	lastMod := initialInfo.ModTime()

	// Update the file to trigger reload
	updatedYAML := `
logging:
  level: debug
providers: {}
models: {}
`
	// Small delay to ensure different mtime
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(cfgPath, []byte(updatedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Manually trigger poll — will detect the new mtime
	w.poll(&lastMod)

	// Wait for reload
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if reloadCount != 1 {
		t.Errorf("expected 1 reload, got %d", reloadCount)
	}
	if lastLevel != "debug" {
		t.Errorf("expected log level 'debug', got %q", lastLevel)
	}
	mu.Unlock()
}

// ---------------------------------------------------------------------------
// TEST-08-01-02: Watcher skips reload when validation fails
// ---------------------------------------------------------------------------

func TestWatcher_SkipsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")

	initialYAML := `
logging:
  level: info
providers: {}
models: {}
`
	if err := os.WriteFile(cfgPath, []byte(initialYAML), 0644); err != nil {
		t.Fatal(err)
	}

	initial, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	var reloadCount int
	onChange := func(old, new Config) {
		reloadCount++
	}

	w := NewWatcher(cfgPath, initial, onChange, slog.Default())

	// Write invalid YAML
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(cfgPath, []byte("{{invalid yaml}}"), 0644); err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(cfgPath)
	lastMod := info.ModTime()
	w.poll(&lastMod)

	time.Sleep(100 * time.Millisecond)

	if reloadCount != 0 {
		t.Errorf("expected 0 reloads for invalid config, got %d", reloadCount)
	}
}

// ---------------------------------------------------------------------------
// TEST-08-01-03: Watcher debounces rapid changes
// ---------------------------------------------------------------------------

func TestWatcher_DebouncesRapidChanges(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")

	if err := os.WriteFile(cfgPath, []byte("logging:\n  level: info\nproviders: {}\nmodels: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	initial, _ := Load(cfgPath)

	var mu sync.Mutex
	var reloadCount int
	onChange := func(old, new Config) {
		mu.Lock()
		reloadCount++
		mu.Unlock()
	}

	w := NewWatcher(cfgPath, initial, onChange, slog.Default())
	w.debounce = 1 * time.Second

	// Trigger two rapid reloads
	info, _ := os.Stat(cfgPath)
	lastMod := info.ModTime()
	w.tryReload(&lastMod)
	w.tryReload(&lastMod) // should be debounced

	mu.Lock()
	if reloadCount != 1 {
		t.Errorf("expected 1 reload (debounced), got %d", reloadCount)
	}
	mu.Unlock()
}

// ---------------------------------------------------------------------------
// TEST-08-01-04: ReloadableFields returns only safe-to-reload fields
// ---------------------------------------------------------------------------

func TestWatcher_ReloadableFieldsExtraction(t *testing.T) {
	cfg := Config{
		Logging:    LoggingConfig{Level: "debug"},
		Guardrails: GuardrailsConfig{Enabled: true},
		Routing:    RoutingConfig{Region: "us-east"},
		Providers:  map[string]ProviderConfig{"openai": {Type: "openai"}},
		Models:     map[string]ModelConfig{"gpt-4": {}},
		Billing:    BillingConfig{Prices: []PriceEntry{{Provider: "openai"}}},
		// Non-reloadable:
		Server:   ServerConfig{Port: 9090},
		Database: DatabaseConfig{URL: "postgres://test"},
		Redis:    RedisConfig{Addr: "localhost:6379"},
	}

	reloadable := ReloadableFields(cfg)

	if reloadable.Logging.Level != "debug" {
		t.Error("expected logging level to be extracted")
	}
	if !reloadable.Guardrails.Enabled {
		t.Error("expected guardrails enabled to be extracted")
	}
	if reloadable.Routing.Region != "us-east" {
		t.Error("expected routing region to be extracted")
	}
	if _, ok := reloadable.Providers["openai"]; !ok {
		t.Error("expected providers to be extracted")
	}
	if _, ok := reloadable.Models["gpt-4"]; !ok {
		t.Error("expected models to be extracted")
	}
	if len(reloadable.Billing.Prices) != 1 {
		t.Error("expected billing prices to be extracted")
	}
}

// ---------------------------------------------------------------------------
// TEST-08-01-05: MergeReloadable applies only reloadable fields
// ---------------------------------------------------------------------------

func TestWatcher_MergeReloadableOnlyTargetFields(t *testing.T) {
	dst := Config{
		Server:   ServerConfig{Port: 8080},
		Database: DatabaseConfig{URL: "postgres://original"},
		Redis:    RedisConfig{Addr: "redis://original"},
		Logging:  LoggingConfig{Level: "info"},
	}

	src := Config{
		Server:     ServerConfig{Port: 9999},                  // should NOT be applied
		Database:   DatabaseConfig{URL: "postgres://changed"}, // should NOT be applied
		Redis:      RedisConfig{Addr: "redis://changed"},      // should NOT be applied
		Logging:    LoggingConfig{Level: "debug"},             // SHOULD be applied
		Guardrails: GuardrailsConfig{Enabled: true},           // SHOULD be applied
	}

	MergeReloadable(&dst, src)

	// Reloadable fields should be applied
	if dst.Logging.Level != "debug" {
		t.Errorf("expected logging level 'debug', got %q", dst.Logging.Level)
	}
	if !dst.Guardrails.Enabled {
		t.Error("expected guardrails to be applied")
	}

	// Non-reloadable fields should NOT be applied
	if dst.Server.Port != 8080 {
		t.Errorf("expected server port to remain 8080, got %d", dst.Server.Port)
	}
	if dst.Database.URL != "postgres://original" {
		t.Errorf("expected database URL to remain unchanged, got %q", dst.Database.URL)
	}
	if dst.Redis.Addr != "redis://original" {
		t.Errorf("expected redis addr to remain unchanged, got %q", dst.Redis.Addr)
	}
}

// ---------------------------------------------------------------------------
// BATCH-39 TASK-05: Config deep copy tests
// ---------------------------------------------------------------------------

func TestConfigDeepCopy_IndependentMaps(t *testing.T) {
	// TEST-39-05-01: DeepCopy produces an independent copy
	original := Config{
		Providers: map[string]ProviderConfig{
			"openai": {Type: "openai", BaseURL: "https://api.openai.com/v1"},
		},
		Models: map[string]ModelConfig{
			"gpt-4": {},
		},
	}

	cp := original.DeepCopy()

	// Mutate copy
	cp.Providers["anthropic"] = ProviderConfig{Type: "anthropic"}
	cp.Providers["openai"] = ProviderConfig{Type: "openai", BaseURL: "https://modified"}
	cp.Models["claude-3"] = ModelConfig{}

	// Original should be unchanged
	if _, ok := original.Providers["anthropic"]; ok {
		t.Error("original.Providers should not contain 'anthropic' added to copy")
	}
	if original.Providers["openai"].BaseURL != "https://api.openai.com/v1" {
		t.Errorf("original.Providers[openai].BaseURL was mutated: %q", original.Providers["openai"].BaseURL)
	}
	if _, ok := original.Models["claude-3"]; ok {
		t.Error("original.Models should not contain 'claude-3' added to copy")
	}
}

func TestWatcher_OnChangeReceivesDeepCopy(t *testing.T) {
	// TEST-39-05-02: onChange callback receives a deep copy
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")

	initialYAML := `
logging:
  level: info
providers:
  openai:
    type: openai
models: {}
`
	if err := os.WriteFile(cfgPath, []byte(initialYAML), 0644); err != nil {
		t.Fatal(err)
	}

	initial, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var receivedConfig Config

	onChange := func(old, new Config) {
		mu.Lock()
		defer mu.Unlock()
		receivedConfig = new
		// Mutate the received config to test independence
		new.Providers["injected"] = ProviderConfig{Type: "injected"}
		new.Logging.Level = "mutated"
	}

	w := NewWatcher(cfgPath, initial, onChange, slog.Default())

	// Write updated config
	updatedYAML := `
logging:
  level: debug
providers:
  openai:
    type: openai
models: {}
`
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(cfgPath, []byte(updatedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(cfgPath)
	lastMod := info.ModTime()
	// Manually call tryReload to ensure it fires
	w.lastReload = time.Time{} // reset debounce
	w.tryReload(&lastMod)
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Verify the watcher's stored current config was NOT mutated by callback
	// Access w.current through a poll-based check: try to read w.current
	// Since w.current is set after tryReload, we can check it indirectly
	// by looking at the Providers map
	currentProviders := w.current.Providers
	if _, ok := currentProviders["injected"]; ok {
		t.Error("watcher.current should NOT contain 'injected' — deep copy failed")
	}
	if w.current.Logging.Level == "mutated" {
		t.Error("watcher.current.Logging.Level should NOT be 'mutated' — deep copy failed")
	}

	// Verify the received config was valid at receipt time
	if receivedConfig.Logging.Level != "debug" {
		t.Errorf("received config should have level 'debug', got %q", receivedConfig.Logging.Level)
	}
}
