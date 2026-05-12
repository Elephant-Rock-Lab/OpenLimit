package providers

import "testing"

// ---------------------------------------------------------------------------
// BATCH-42 TASK-01: ProviderRegistry lookup + ApplyDefaults tests
// ---------------------------------------------------------------------------

func TestLookupDefault_KnownProvider(t *testing.T) {
	// TEST-42-01-01: LookupDefault returns correct ProviderDefault for known name
	d, ok := LookupDefault("deepseek")
	if !ok {
		t.Fatal("expected deepseek to be found in registry")
	}
	if d.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("BaseURL = %q, want %q", d.BaseURL, "https://api.deepseek.com/v1")
	}
	if d.BaseType != "openai-compatible" {
		t.Errorf("BaseType = %q, want %q", d.BaseType, "openai-compatible")
	}
}

func TestLookupDefault_UnknownProvider(t *testing.T) {
	// TEST-42-01-02: LookupDefault returns false for unknown provider
	_, ok := LookupDefault("nonexistent_provider_xyz")
	if ok {
		t.Error("expected unknown provider to return false")
	}
}

func TestLookupDefault_CaseInsensitive(t *testing.T) {
	// TEST-42-01-05: LookupDefault is case-insensitive
	d1, ok1 := LookupDefault("DeepSeek")
	d2, ok2 := LookupDefault("deepseek")
	d3, ok3 := LookupDefault("DEEPSEEK")

	if !ok1 || !ok2 || !ok3 {
		t.Fatal("expected all case variants to be found")
	}
	if d1.BaseURL != d2.BaseURL || d2.BaseURL != d3.BaseURL {
		t.Errorf("case variants returned different URLs: %q, %q, %q", d1.BaseURL, d2.BaseURL, d3.BaseURL)
	}
}

func TestRegistryHas20PlusNewProviders(t *testing.T) {
	// TEST-42-01-06: Registry has 20+ new provider entries
	count := len(DefaultRegistry)
	if count < 20 {
		t.Errorf("registry has %d entries, want at least 20", count)
	}
	t.Logf("registry contains %d providers", count)
}

func TestApplyDefaults_FillsTypeFromRegistry(t *testing.T) {
	// TEST-42-01-03: ApplyDefaults fills BaseType and BaseURL from registry
	cfg := map[string]interface{}{} // no type, no base_url
	result := ApplyDefaults("deepseek", cfg)

	if typ, _ := result["type"].(string); typ != "openai-compatible" {
		t.Errorf("type = %q, want %q", typ, "openai-compatible")
	}
	if url, _ := result["base_url"].(string); url != "https://api.deepseek.com/v1" {
		t.Errorf("base_url = %q, want %q", url, "https://api.deepseek.com/v1")
	}
}

func TestApplyDefaults_PreservesUserConfig(t *testing.T) {
	// TEST-42-01-04: ApplyDefaults preserves user config when both exist
	cfg := map[string]interface{}{
		"type":     "anthropic",
		"base_url": "https://custom.proxy.example.com/v1",
	}
	result := ApplyDefaults("deepseek", cfg)

	if typ, _ := result["type"].(string); typ != "anthropic" {
		t.Errorf("type = %q, want %q (user override)", typ, "anthropic")
	}
	if url, _ := result["base_url"].(string); url != "https://custom.proxy.example.com/v1" {
		t.Errorf("base_url = %q, want %q (user override)", url, "https://custom.proxy.example.com/v1")
	}
}

func TestApplyDefaults_UnknownProviderFallsBack(t *testing.T) {
	// TEST-42-01-07: Unknown provider with no type falls back to openai-compatible
	cfg := map[string]interface{}{}
	result := ApplyDefaults("my_custom_provider", cfg)

	if typ, _ := result["type"].(string); typ != "openai-compatible" {
		t.Errorf("type = %q, want %q (fallback)", typ, "openai-compatible")
	}
	// base_url should remain empty for unknown providers
	if url, _ := result["base_url"].(string); url != "" {
		t.Errorf("base_url = %q, want empty string for unknown provider", url)
	}
}

// ---------------------------------------------------------------------------
// BATCH-42 TASK-02: Provider validation tests
// ---------------------------------------------------------------------------

func TestValidateProvider_RejectsEmptyBaseURLForCompatType(t *testing.T) {
	// TEST-42-02-01: ValidateProvider rejects empty BaseURL for openai-compatible type
	err := ValidateProvider("my_proxy", "openai-compatible", "")
	if err == nil {
		t.Fatal("expected error for empty base_url with openai-compatible type")
	}
	if pve, ok := err.(*ProviderValidationError); !ok {
		t.Fatalf("expected ProviderValidationError, got %T", err)
	} else if pve.Field != "base_url" {
		t.Errorf("Field = %q, want %q", pve.Field, "base_url")
	}
}

func TestValidateProvider_AllowsEmptyBaseURLForBuiltinType(t *testing.T) {
	// Known types with built-in URLs (e.g. "openai") may have empty base_url
	err := ValidateProvider("openai_primary", "openai", "")
	if err != nil {
		t.Errorf("expected no error for openai type with empty base_url, got: %v", err)
	}
}

func TestValidateProvider_RejectsUnknownType(t *testing.T) {
	// TEST-42-02-02: ValidateProvider rejects unknown adapter type
	err := ValidateProvider("test_provider", "unknown_type_xyz", "https://api.example.com/v1")
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if pve, ok := err.(*ProviderValidationError); !ok {
		t.Fatalf("expected ProviderValidationError, got %T", err)
	} else if pve.Field != "type" {
		t.Errorf("Field = %q, want %q", pve.Field, "type")
	}
}

func TestValidateProvider_AcceptsValidConfig(t *testing.T) {
	// TEST-42-02-03: ValidateProvider accepts valid config
	err := ValidateProvider("test_provider", "openai-compatible", "https://api.example.com/v1")
	if err != nil {
		t.Errorf("expected no error for valid config, got: %v", err)
	}
}
