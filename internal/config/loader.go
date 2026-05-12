package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML config file. If the file is missing, defaults are returned.
func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			normalize(&cfg)
			return cfg, Validate(cfg)
		}
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}

	normalize(&cfg)

	// Allow DATABASE_URL env var as fallback when not set in YAML.
	if cfg.Database.URL == "" {
		if envURL := os.Getenv("DATABASE_URL"); envURL != "" {
			cfg.Database.URL = envURL
		}
	}

	// Allow ADMIN_TOKEN env var as fallback for admin bearer token.
	if cfg.Admin.BearerToken == "" {
		if envToken := os.Getenv("ADMIN_TOKEN"); envToken != "" {
			cfg.Admin.BearerToken = envToken
		}
	}

	if err := Validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func normalize(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}

	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}

	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}

	if cfg.Database.MaxOpenConns <= 0 {
		cfg.Database.MaxOpenConns = 25
	}
	if cfg.Database.MaxIdleConns <= 0 {
		cfg.Database.MaxIdleConns = 5
	}
	if cfg.Auth.KeyCacheSize <= 0 {
		cfg.Auth.KeyCacheSize = 10000
	}
	if cfg.Auth.KeyCacheTTLSec <= 0 {
		cfg.Auth.KeyCacheTTLSec = 60
	}
	if cfg.Cache.Exact.MaxEntries <= 0 {
		cfg.Cache.Exact.MaxEntries = 10000
	}
	if cfg.Cache.Exact.TTLSeconds <= 0 {
		cfg.Cache.Exact.TTLSeconds = 3600
	}
	if cfg.Routing.Defaults.TimeoutMS <= 0 {
		cfg.Routing.Defaults.TimeoutMS = 60000
	}
	if cfg.Routing.Defaults.Retry.Attempts <= 0 {
		cfg.Routing.Defaults.Retry.Attempts = 1
	}
	if cfg.Routing.Defaults.Retry.InitialMS <= 0 {
		cfg.Routing.Defaults.Retry.InitialMS = 250
	}
	if cfg.Routing.Defaults.Retry.MaxMS <= 0 {
		cfg.Routing.Defaults.Retry.MaxMS = 4000
	}
	if cfg.Server.MaxBodySizeKB <= 0 {
		cfg.Server.MaxBodySizeKB = 10240 // 10MB default
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	if cfg.Models == nil {
		cfg.Models = map[string]ModelConfig{}
	}
}
