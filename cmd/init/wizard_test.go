package main

import (
	"os"
	"path/filepath"
	"testing"

	"openlimit/internal/config"
)

// TEST-33-02-01: NonInteractiveMode returns error when PROVIDER_TYPE is missing
func TestRunNonInteractive_MissingProviderType(t *testing.T) {
	// Ensure env vars are unset
	os.Unsetenv("PROVIDER_TYPE")
	os.Unsetenv("PROVIDER_KEY")

	err := RunNonInteractive(filepath.Join(t.TempDir(), "gateway.yaml"), false)
	if err == nil {
		t.Fatal("expected error when PROVIDER_TYPE is missing, got nil")
	}
	if !containsSubstr(err.Error(), "PROVIDER_TYPE") {
		t.Errorf("error should mention PROVIDER_TYPE, got: %v", err)
	}
}

// TEST-33-02-02: NonInteractiveMode with all env vars produces valid config
func TestRunNonInteractive_AllEnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "gateway.yaml")

	os.Setenv("PROVIDER_TYPE", "openai")
	os.Setenv("PROVIDER_KEY", "sk-test-key-1234567890abcdef")
	os.Setenv("PROVIDER_NAME", "my-openai")
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/test")
	defer func() {
		os.Unsetenv("PROVIDER_TYPE")
		os.Unsetenv("PROVIDER_KEY")
		os.Unsetenv("PROVIDER_NAME")
		os.Unsetenv("DATABASE_URL")
	}()

	err := RunNonInteractive(outputPath, false)
	if err != nil {
		t.Fatalf("RunNonInteractive failed: %v", err)
	}

	// Verify the file was written
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("config file not created: %v", statErr)
	}

	// Load it back and validate
	cfg, err := config.Load(outputPath)
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("loaded config failed Validate(): %v", err)
	}
}

// TEST-33-02-03: WriteConfig writes valid YAML to temp file
func TestWriteConfig_CreatesValidFile(t *testing.T) {
	cfg, err := GenerateConfig(InitInput{
		ProviderType: "anthropic",
		ProviderName: "anthropic",
		APIKey:       "sk-ant-test-key-1234567890abcdef",
		DatabaseURL:  "",
	})
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "gateway.yaml")
	if err := WriteConfig(cfg, outputPath, false); err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	// File must exist
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("file not created: %v", statErr)
	}

	// Must load successfully
	loaded, err := config.Load(outputPath)
	if err != nil {
		t.Fatalf("config.Load failed on written file: %v", err)
	}
	if loaded.Providers["anthropic"].Type != "anthropic" {
		t.Error("provider type mismatch after write/load")
	}
}

// TEST-33-02-04: WriteConfig refuses to overwrite without force flag
func TestWriteConfig_RefusesOverwriteWithoutForce(t *testing.T) {
	cfg, err := GenerateConfig(InitInput{
		ProviderType: "openai",
		ProviderName: "openai",
		APIKey:       "sk-test-key-1234567890abcdef",
	})
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "gateway.yaml")

	// Write initially
	if err := WriteConfig(cfg, outputPath, false); err != nil {
		t.Fatalf("first WriteConfig failed: %v", err)
	}

	// Try to write again without force — should fail
	err = WriteConfig(cfg, outputPath, false)
	if err == nil {
		t.Fatal("expected error when overwriting without force, got nil")
	}
	if !containsSubstr(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}

	// Write again with force — should succeed
	if err := WriteConfig(cfg, outputPath, true); err != nil {
		t.Fatalf("WriteConfig with force failed: %v", err)
	}
}

// TEST-33-02-05: Provider defaults are correct
func TestProviderDefaults(t *testing.T) {
	defaults := map[string]struct {
		model string
		key   string
	}{
		"openai":            {model: "gpt-4o-mini", key: "OPENAI_API_KEY"},
		"anthropic":         {model: "claude-sonnet-4-20250514", key: "ANTHROPIC_API_KEY"},
		"gemini":            {model: "gemini-2.0-flash", key: "GOOGLE_API_KEY"},
		"openai-compatible": {model: "", key: "OPENAI_COMPATIBLE_API_KEY"},
	}

	for providerType, expected := range defaults {
		t.Run(providerType, func(t *testing.T) {
			tmpl, ok := providerTemplates[providerType]
			if !ok {
				t.Fatalf("providerTemplates missing entry for %q", providerType)
			}
			if tmpl.DefaultModel != expected.model {
				t.Errorf("expected default model %q, got %q", expected.model, tmpl.DefaultModel)
			}
			if tmpl.KeyFieldName != expected.key {
				t.Errorf("expected key field name %q, got %q", expected.key, tmpl.KeyFieldName)
			}
		})
	}
}
