package main

import (
	"os"
	"regexp"
	"testing"

	"openlimit/internal/config"
)

// TEST-33-01-01: GenerateConfig("openai", ...) produces a valid Config
func TestGenerateConfig_OpenAI_ProducesValidConfig(t *testing.T) {
	cfg, err := GenerateConfig(InitInput{
		ProviderType: "openai",
		ProviderName: "openai",
		APIKey:       "sk-test-key-1234567890abcdef",
	})
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("generated config failed Validate(): %v", err)
	}
	// Verify provider exists
	p, ok := cfg.Providers["openai"]
	if !ok {
		t.Fatal("provider 'openai' not found in config")
	}
	if p.Type != "openai" {
		t.Errorf("expected provider type 'openai', got %q", p.Type)
	}
	if len(p.Keys) != 1 || p.Keys[0].Value != "sk-test-key-1234567890abcdef" {
		t.Errorf("expected API key in provider keys, got %v", p.Keys)
	}
}

// TEST-33-01-02: GenerateConfig produces YAML that round-trips through config.Load
func TestGenerateConfig_YAML_RoundTrip(t *testing.T) {
	cfg, err := GenerateConfig(InitInput{
		ProviderType: "openai",
		ProviderName: "openai",
		APIKey:       "sk-test-key-1234567890abcdef",
		DatabaseURL:  "postgres://user:pass@localhost:5432/openlimit",
	})
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	yamlBytes, err := ConfigToYAML(cfg)
	if err != nil {
		t.Fatalf("ConfigToYAML failed: %v", err)
	}

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "gateway-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(yamlBytes); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	tmpFile.Close()

	// Load it back
	loaded, err := config.Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("config.Load round-trip failed: %v", err)
	}

	// Verify key fields match
	if loaded.Server.Port != cfg.Server.Port {
		t.Errorf("server port mismatch: got %d, want %d", loaded.Server.Port, cfg.Server.Port)
	}
	if loaded.Providers["openai"].Type != "openai" {
		t.Errorf("provider type mismatch after round-trip")
	}
	if len(loaded.Models) == 0 {
		t.Error("no models found after round-trip")
	}
}

// TEST-33-01-03: GenerateConfig masks keys in String() output
func TestMaskKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{"long key", "sk-test-key-1234567890abcdef", "sk-t...cdef"},
		{"exactly 12 chars", "sk-1234567890", "sk-...890"},
		{"11 chars", "sk-123456789", "****"},
		{"short key", "sk-short", "****"},
		{"empty key", "", "****"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskKey(tt.key)
			if tt.key != "" && len(tt.key) >= 12 {
				// Should show first 4 and last 4
				if result[:4] != tt.key[:4] {
					t.Errorf("prefix mismatch: got %q, want %q", result[:4], tt.key[:4])
				}
				if result[len(result)-4:] != tt.key[len(tt.key)-4:] {
					t.Errorf("suffix mismatch: got %q, want %q", result[len(result)-4:], tt.key[len(tt.key)-4:])
				}
				// Must not contain the full key
				if result == tt.key {
					t.Errorf("key was not masked: %q", result)
				}
			} else {
				if result != "****" {
					t.Errorf("expected '****' for short key, got %q", result)
				}
			}
		})
	}
}

// TEST-33-01-04: GenerateConfig generates 32-byte hex admin token (64 chars)
func TestGenerateAdminToken(t *testing.T) {
	token, err := GenerateAdminToken()
	if err != nil {
		t.Fatalf("GenerateAdminToken failed: %v", err)
	}

	if len(token) != 64 {
		t.Errorf("expected 64-char hex string (32 bytes), got %d chars", len(token))
	}

	// Must be lowercase hex
	hexPattern := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if !hexPattern.MatchString(token) {
		t.Errorf("token is not valid lowercase hex: %q", token)
	}

	// Generate a second token and ensure they differ (crypto/rand)
	token2, err := GenerateAdminToken()
	if err != nil {
		t.Fatalf("GenerateAdminToken (2nd call) failed: %v", err)
	}
	if token == token2 {
		t.Error("two generated tokens are identical — crypto/rand not working?")
	}
}

// TEST-33-01-05: GenerateConfig rejects unknown provider type
func TestGenerateConfig_RejectsUnknownProvider(t *testing.T) {
	_, err := GenerateConfig(InitInput{
		ProviderType: "badtype",
		APIKey:       "sk-test",
	})
	if err == nil {
		t.Fatal("expected error for unknown provider type, got nil")
	}

	// Error should list supported types
	for _, supported := range []string{"openai", "anthropic", "gemini", "openai-compatible"} {
		if !containsString(err.Error(), supported) {
			t.Errorf("error message should list supported type %q, got: %v", supported, err)
		}
	}
}

// TEST-33-01-06: GenerateConfig with database URL includes auth section
func TestGenerateConfig_WithDatabase_IncludesAuth(t *testing.T) {
	cfg, err := GenerateConfig(InitInput{
		ProviderType: "openai",
		ProviderName: "openai",
		APIKey:       "sk-test-key-1234567890abcdef",
		DatabaseURL:  "postgres://user:pass@localhost:5432/openlimit",
	})
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	if !cfg.Auth.Enabled {
		t.Error("expected auth.Enabled == true when database URL is set")
	}
	if !cfg.Admin.Enabled {
		t.Error("expected admin.Enabled == true when database URL is set")
	}
	if cfg.Admin.BearerToken == "" {
		t.Error("expected admin bearer token to be set")
	}
}

// TEST-33-01-07: GenerateConfig without database URL disables auth
func TestGenerateConfig_WithoutDatabase_DisablesAuth(t *testing.T) {
	cfg, err := GenerateConfig(InitInput{
		ProviderType: "openai",
		ProviderName: "openai",
		APIKey:       "sk-test-key-1234567890abcdef",
		DatabaseURL:  "",
	})
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	if cfg.Auth.Enabled {
		t.Error("expected auth.Enabled == false when database URL is empty")
	}
	if cfg.Admin.Enabled {
		t.Error("expected admin.Enabled == false when database URL is empty")
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
