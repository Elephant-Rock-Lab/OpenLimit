package config

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ReloadableConfig contains only the fields that can be safely hot-reloaded
// at runtime without restarting the gateway. Fields NOT listed here (server
// host/port, database URL, Redis address, KMS config) require a full restart.
type ReloadableConfig struct {
	Logging    LoggingConfig
	Guardrails GuardrailsConfig
	Routing    RoutingConfig
	Providers  map[string]ProviderConfig
	Models     map[string]ModelConfig
	Billing    BillingConfig
}

// Watcher monitors a config file for changes and triggers reload when the
// file is modified. It uses polling (stat-based) instead of filesystem events
// to avoid external dependencies.
type Watcher struct {
	path       string
	current    Config
	onChange   func(old, new Config)
	logger     *slog.Logger
	interval   time.Duration
	debounce   time.Duration
	debounceMu sync.Mutex
	lastReload time.Time
	cancel     context.CancelFunc
	closeOnce  sync.Once
}

// NewWatcher creates a config file watcher. The onChange callback is called
// with the old and new configs when a valid change is detected.
func NewWatcher(path string, initial Config, onChange func(old, new Config), logger *slog.Logger) *Watcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Watcher{
		path:     path,
		current:  initial,
		onChange: onChange,
		logger:   logger.With("component", "config_watcher"),
		interval: 5 * time.Second,
		debounce: 1 * time.Second,
	}
}

// Start begins watching the config file. Blocks until context is cancelled.
// Should be run in a goroutine.
func (w *Watcher) Start(ctx context.Context) {
	// Also listen for SIGHUP
	hupCh := make(chan os.Signal, 1)
	signal.Notify(hupCh, syscall.SIGHUP)
	defer signal.Stop(hupCh)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	var lastMod time.Time

	// Get initial mod time
	if info, err := os.Stat(w.path); err == nil {
		lastMod = info.ModTime()
	}

	w.logger.Info("config watcher started", "path", w.path, "interval", w.interval)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("config watcher stopped")
			return
		case <-hupCh:
			w.logger.Info("SIGHUP received, triggering config reload")
			w.tryReload(&lastMod)
		case <-ticker.C:
			w.poll(&lastMod)
		}
	}
}

// StartBackground starts the watcher in a background goroutine.
// Returns a cancel function to stop the watcher.
func (w *Watcher) StartBackground() context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.Start(ctx)
	return cancel
}

// Close stops the watcher. Safe to call multiple times.
func (w *Watcher) Close() {
	w.closeOnce.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
	})
}

// poll checks if the file modification time has changed.
func (w *Watcher) poll(lastMod *time.Time) {
	info, err := os.Stat(w.path)
	if err != nil {
		// File might be temporarily removed during edit (e.g., vim saves)
		return
	}
	if info.ModTime().After(*lastMod) {
		*lastMod = info.ModTime()
		w.tryReload(lastMod)
	}
}

// tryReload attempts to reload the config with debounce protection.
func (w *Watcher) tryReload(lastMod *time.Time) {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()

	if time.Since(w.lastReload) < w.debounce {
		w.logger.Debug("skipping reload, debounce window active")
		return
	}

	newCfg, err := Load(w.path)
	if err != nil {
		w.logger.Warn("config reload failed, keeping current config",
			"error", err,
		)
		return
	}

	old := w.current
	w.current = newCfg
	w.lastReload = time.Now()

	if w.onChange != nil {
		w.onChange(old, newCfg.DeepCopy())
	}

	w.logger.Info("config reloaded",
		"path", w.path,
		"log_level", newCfg.Logging.Level,
		"providers", len(newCfg.Providers),
		"models", len(newCfg.Models),
	)
}

// ReloadableFields extracts the hot-reloadable subset from a Config.
func ReloadableFields(cfg Config) ReloadableConfig {
	return ReloadableConfig{
		Logging:    cfg.Logging,
		Guardrails: cfg.Guardrails,
		Routing:    cfg.Routing,
		Providers:  cfg.Providers,
		Models:     cfg.Models,
		Billing:    cfg.Billing,
	}
}

// MergeReloadable applies only reloadable fields from src into dst.
// Non-reloadable fields in dst are left unchanged.
func MergeReloadable(dst *Config, src Config) {
	dst.Logging = src.Logging
	dst.Guardrails = src.Guardrails
	dst.Routing = src.Routing
	dst.Providers = src.Providers
	dst.Models = src.Models
	dst.Billing = src.Billing
}
