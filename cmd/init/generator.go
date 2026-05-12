package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"openlimit/internal/config"

	"gopkg.in/yaml.v3"
)

// InitInput holds all user-provided inputs for config generation.
type InitInput struct {
	ProviderType string // openai, anthropic, gemini, openai-compatible
	ProviderName string // logical name for the provider entry
	APIKey       string // provider API key
	DatabaseURL  string // optional database URL
	OutputPath   string // file path for generated config
	BaseURL      string // base URL for openai-compatible
	Model        string // model name override (used for openai-compatible)
}

// ProviderTemplate holds defaults for a known provider type.
type ProviderTemplate struct {
	Type         string // provider type string
	DefaultModel string // default model for this provider
	KeyFieldName string // env var name for the API key
}

// providerTemplates maps known provider types to their defaults.
var providerTemplates = map[string]ProviderTemplate{
	"openai": {
		Type:         "openai",
		DefaultModel: "gpt-4o-mini",
		KeyFieldName: "OPENAI_API_KEY",
	},
	"anthropic": {
		Type:         "anthropic",
		DefaultModel: "claude-sonnet-4-20250514",
		KeyFieldName: "ANTHROPIC_API_KEY",
	},
	"gemini": {
		Type:         "gemini",
		DefaultModel: "gemini-2.0-flash",
		KeyFieldName: "GOOGLE_API_KEY",
	},
	"openai-compatible": {
		Type:         "openai-compatible",
		DefaultModel: "",
		KeyFieldName: "OPENAI_COMPATIBLE_API_KEY",
	},
}

// GenerateConfig builds a complete Config from InitInput.
// The returned Config is guaranteed to pass config.Validate() when the input is valid.
func GenerateConfig(input InitInput) (config.Config, error) {
	template, ok := providerTemplates[input.ProviderType]
	if !ok {
		supported := make([]string, 0, len(providerTemplates))
		for k := range providerTemplates {
			supported = append(supported, k)
		}
		return config.Config{}, fmt.Errorf("unsupported provider type %q; supported types: %s",
			input.ProviderType, strings.Join(supported, ", "))
	}

	// Provider name defaults to provider type if empty
	providerName := input.ProviderName
	if providerName == "" {
		providerName = input.ProviderType
	}

	// Determine model
	model := input.Model
	if model == "" {
		model = template.DefaultModel
	}
	if model == "" {
		// openai-compatible with no model specified
		model = "default"
	}

	cfg := config.Default()

	// Server config
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 8080

	// Database config
	cfg.Database.URL = input.DatabaseURL

	// Auth: only enable when database is configured
	cfg.Auth.Enabled = input.DatabaseURL != ""
	cfg.Auth.KeyCacheSize = 10000
	cfg.Auth.KeyCacheTTLSec = 60

	// Admin: only enable when database is configured
	adminToken, err := GenerateAdminToken()
	if err != nil {
		return config.Config{}, fmt.Errorf("generate admin token: %w", err)
	}
	cfg.Admin.Enabled = input.DatabaseURL != ""
	cfg.Admin.BearerToken = adminToken

	// Routing
	cfg.Routing.Defaults.TimeoutMS = 60000
	cfg.Routing.Defaults.Retry.Attempts = 1

	// Provider
	providerCfg := config.ProviderConfig{
		Type: template.Type,
		Keys: []config.ProviderKeyConfig{
			{
				ID:     "primary",
				Value:  input.APIKey,
				Weight: 100,
			},
		},
	}

	// Type-specific config
	switch input.ProviderType {
	case "openai-compatible":
		providerCfg.BaseURL = input.BaseURL
	case "gemini":
		// Gemini requires gemini_model_map — map the model to itself
		providerCfg.GeminiModelMap = map[string]string{
			model: model,
		}
	}

	cfg.Providers[providerName] = providerCfg

	// Model route
	cfg.Models["fast"] = config.ModelConfig{
		Routes: []config.ModelRoute{
			{
				Provider: providerName,
				Model:    model,
				Weight:   100,
			},
		},
	}

	// Validate the generated config
	if err := config.Validate(cfg); err != nil {
		return config.Config{}, fmt.Errorf("generated config failed validation: %w", err)
	}

	return cfg, nil
}

// GenerateAdminToken creates a 32-byte hex token using crypto/rand.
// Returns a 64-character lowercase hex string.
func GenerateAdminToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// MaskKey masks an API key, showing first 4 and last 4 characters.
// Keys shorter than 12 chars are fully masked as "****".
func MaskKey(key string) string {
	if len(key) < 12 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// ConfigToYAML marshals a Config to YAML bytes.
func ConfigToYAML(cfg config.Config) ([]byte, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal yaml: %w", err)
	}
	return data, nil
}
