package main

import (
	"os"
	"testing"

	"openlimit/internal/config"
)

// ---------------------------------------------------------------------------
// BATCH-47: Config Doctor tests
// ---------------------------------------------------------------------------

func TestDoctor_NoProviders_Warns(t *testing.T) {
	// TEST-47-01-01: No providers → WARN
	cfg := config.Default()
	results, err := runDoctorWithConfig(&cfg)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Name == "Providers configured" && r.Status == "WARN" {
			found = true
		}
	}
	if !found {
		t.Error("expected WARN for no providers")
	}
}

func TestDoctor_ValidProvider_Passes(t *testing.T) {
	// TEST-47-01-02: Valid provider with base_url → PASS
	cfg := config.Default()
	cfg.Providers = map[string]config.ProviderConfig{
		"deepseek": {
			Type:    "openai-compatible",
			BaseURL: "https://api.deepseek.com/v1",
			Keys:    []config.ProviderKeyConfig{{ID: "test", Value: "sk-test", Weight: 100}},
		},
	}

	results, err := runDoctorWithConfig(&cfg)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}

	for _, r := range results {
		if r.Status == "FAIL" {
			t.Errorf("unexpected FAIL: %s — %s", r.Name, r.Message)
		}
	}
}

func TestDoctor_RegistryProviderWithoutType_Resolves(t *testing.T) {
	// TEST-47-01-03: Registry provider with no type resolves via registry
	cfg := config.Default()
	cfg.Providers = map[string]config.ProviderConfig{
		"together_ai": {
			BaseURL: "https://api.together.xyz/v1",
			Keys:    []config.ProviderKeyConfig{{ID: "test", Value: "sk-test", Weight: 100}},
		},
	}

	results, err := runDoctorWithConfig(&cfg)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}

	for _, r := range results {
		if r.Status == "FAIL" {
			t.Errorf("unexpected FAIL: %s — %s", r.Name, r.Message)
		}
	}
}

func TestDoctor_DanglingModelRoute_Fails(t *testing.T) {
	// TEST-47-01-04: Model route to non-existent provider → FAIL
	cfg := config.Default()
	cfg.Providers = map[string]config.ProviderConfig{
		"deepseek": {
			Type:    "openai-compatible",
			BaseURL: "https://api.deepseek.com/v1",
			Keys:    []config.ProviderKeyConfig{{ID: "test", Value: "sk-test", Weight: 100}},
		},
	}
	cfg.Models = map[string]config.ModelConfig{
		"fast": {Routes: []config.ModelRoute{
			{Provider: "nonexistent_provider", Model: "gpt-4", Weight: 100},
		}},
	}

	results, err := runDoctorWithConfig(&cfg)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Status == "FAIL" && r.Name == `Model "fast" route to "nonexistent_provider"` {
			found = true
		}
	}
	if !found {
		t.Error("expected FAIL for dangling model route")
	}
}

func TestDoctor_MissingEnvVar_Fails(t *testing.T) {
	// TEST-47-01-05: Key with missing env var → FAIL
	cfg := config.Default()
	cfg.Providers = map[string]config.ProviderConfig{
		"openai": {
			Type:    "openai",
			BaseURL: "https://api.openai.com/v1",
			Keys:    []config.ProviderKeyConfig{{ID: "test", Env: "OPENAI_API_KEY_DOES_NOT_EXIST_12345", Weight: 100}},
		},
	}

	results, err := runDoctorWithConfig(&cfg)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Status == "FAIL" && r.Name == `Provider "openai" key env vars` {
			found = true
		}
	}
	if !found {
		t.Error("expected FAIL for missing env var")
	}
}

func TestDoctor_EnvVarPresent_Passes(t *testing.T) {
	// TEST-47-01-06: Key with present env var → PASS
	os.Setenv("DOCTOR_TEST_KEY", "sk-test-123")
	defer os.Unsetenv("DOCTOR_TEST_KEY")

	cfg := config.Default()
	cfg.Providers = map[string]config.ProviderConfig{
		"openai": {
			Type:    "openai",
			BaseURL: "https://api.openai.com/v1",
			Keys:    []config.ProviderKeyConfig{{ID: "test", Env: "DOCTOR_TEST_KEY", Weight: 100}},
		},
	}

	results, err := runDoctorWithConfig(&cfg)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}

	for _, r := range results {
		if r.Status == "FAIL" {
			t.Errorf("unexpected FAIL: %s — %s", r.Name, r.Message)
		}
	}
}

func TestDoctor_UnknownProviderWithNoType_Fails(t *testing.T) {
	// TEST-47-01-07: Unknown provider with no type or base_url → FAIL
	cfg := config.Default()
	cfg.Providers = map[string]config.ProviderConfig{
		"my_custom_provider": {
			// No type, no base_url, not in registry
		},
	}

	results, err := runDoctorWithConfig(&cfg)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Status == "FAIL" {
			found = true
		}
	}
	if !found {
		t.Error("expected at least one FAIL for unknown provider with no type")
	}
}

// runDoctorWithConfig runs the doctor checks with a given config (no file loading).
func runDoctorWithConfig(cfg *config.Config) ([]CheckResult, error) {
	var results []CheckResult
	results = append(results, checkProvidersExist(cfg)...)
	results = append(results, checkProviderResolution(cfg)...)
	results = append(results, checkModelRoutes(cfg)...)
	results = append(results, checkProviderKeys(cfg)...)
	return results, nil
}
