package config

import "testing"

func TestValidate_GeminiEmptyModelMap(t *testing.T) {
	cfg := Default()
	cfg.Providers["gemini-provider"] = ProviderConfig{
		Type: "gemini",
		Keys: []ProviderKeyConfig{
			{ID: "key1", Value: "test-key"},
		},
	}
	cfg.Models["gpt-4"] = ModelConfig{
		Routes: []ModelRoute{
			{Provider: "gemini-provider", Model: "gemini-2.0-flash", Weight: 1},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for gemini provider with empty model map, got nil")
	}
	expected := `provider "gemini-provider": gemini_model_map is required for gemini provider type`
	if !contains(err.Error(), expected) {
		t.Errorf("expected error to contain %q, got %q", expected, err.Error())
	}
}

func TestValidate_AzureEmptyResource(t *testing.T) {
	cfg := Default()
	cfg.Providers["azure-provider"] = ProviderConfig{
		Type: "azure-openai",
		Keys: []ProviderKeyConfig{
			{ID: "key1", Value: "test-key"},
		},
	}
	cfg.Models["gpt-4"] = ModelConfig{
		Routes: []ModelRoute{
			{Provider: "azure-provider", Model: "gpt-4", Weight: 1},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for azure-openai provider with empty resource, got nil")
	}
	expected := `provider "azure-provider": azure_resource is required for azure-openai provider type`
	if !contains(err.Error(), expected) {
		t.Errorf("expected error to contain %q, got %q", expected, err.Error())
	}
}

func TestValidate_AzureDefaultAPIVersion(t *testing.T) {
	cfg := Default()
	cfg.Providers["azure-provider"] = ProviderConfig{
		Type:            "azure-openai",
		AzureResource:   "my-resource",
		AzureAPIVersion: "",
		Keys: []ProviderKeyConfig{
			{ID: "key1", Value: "test-key"},
		},
	}
	cfg.Models["gpt-4"] = ModelConfig{
		Routes: []ModelRoute{
			{Provider: "azure-provider", Model: "gpt-4", Weight: 1},
		},
	}

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("expected no validation error, got %v", err)
	}

	actual := cfg.Providers["azure-provider"].AzureAPIVersion
	if actual != "2025-06-01" {
		t.Errorf("expected AzureAPIVersion to default to %q, got %q", "2025-06-01", actual)
	}
}

func TestValidate_GeminiValidModelMap(t *testing.T) {
	cfg := Default()
	cfg.Providers["gemini-provider"] = ProviderConfig{
		Type: "gemini",
		GeminiModelMap: map[string]string{
			"gemini-pro": "gemini-2.0-pro-exp-02-05",
		},
		Keys: []ProviderKeyConfig{
			{ID: "key1", Value: "test-key"},
		},
	}
	cfg.Models["gemini-pro"] = ModelConfig{
		Routes: []ModelRoute{
			{Provider: "gemini-provider", Model: "gemini-pro", Weight: 1},
		},
	}

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("expected no validation error, got %v", err)
	}
}

func TestValidate_AllProviderTypesAccepted(t *testing.T) {
	types := []string{"openai", "openai-compatible", "anthropic", "gemini", "azure-openai",
		"bedrock", "vertex", "groq", "cohere", "mistral", ""}

	for _, ptype := range types {
		t.Run(ptype, func(t *testing.T) {
			cfg := Default()
			name := "test-provider"
			pc := ProviderConfig{
				Type: ptype,
				Keys: []ProviderKeyConfig{{ID: "k1", Value: "test"}},
			}
			// Fill required fields per type
			switch ptype {
			case "gemini":
				pc.GeminiModelMap = map[string]string{"m": "m1"}
			case "azure-openai":
				pc.AzureResource = "res"
			case "vertex":
				pc.Project = "proj"
				pc.Region = "us-central1"
			case "openai-compatible":
				pc.BaseURL = "http://localhost:11434/v1"
			}
			cfg.Providers[name] = pc
			cfg.Models["test-model"] = ModelConfig{
				Routes: []ModelRoute{{Provider: name, Model: "m", Weight: 1}},
			}

			err := Validate(cfg)
			if err != nil {
				t.Fatalf("provider type %q should be accepted, got error: %v", ptype, err)
			}
		})
	}
}

func TestValidate_VertexRequiresProject(t *testing.T) {
	cfg := Default()
	cfg.Providers["vertex-test"] = ProviderConfig{
		Type:   "vertex",
		Region: "us-central1",
		Keys:   []ProviderKeyConfig{{ID: "k1", Value: "test"}},
	}
	cfg.Models["m"] = ModelConfig{
		Routes: []ModelRoute{{Provider: "vertex-test", Model: "m", Weight: 1}},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for vertex without project")
	}
	if !containsSubstr(err.Error(), "project is required for vertex") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TEST-21-01-01: Cohere config without base_url gets correct default (https://api.cohere.com/v2)
func TestValidate_CohereDefaultBaseURL(t *testing.T) {
	cfg := Default()
	cfg.Providers["cohere-provider"] = ProviderConfig{
		Type: "cohere",
		Keys: []ProviderKeyConfig{
			{ID: "key1", Value: "test-key"},
		},
	}
	cfg.Models["command-r"] = ModelConfig{
		Routes: []ModelRoute{
			{Provider: "cohere-provider", Model: "command-r", Weight: 1},
		},
	}

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("expected no validation error, got %v", err)
	}

	actual := cfg.Providers["cohere-provider"].BaseURL
	expected := "https://api.cohere.com/v2"
	if actual != expected {
		t.Errorf("expected Cohere BaseURL to default to %q, got %q", expected, actual)
	}
}

// TEST-21-01-02: Cohere config WITH explicit base_url preserves user value
func TestValidate_CohereExplicitBaseURL(t *testing.T) {
	cfg := Default()
	userURL := "https://custom-cohere.example.com/v2"
	cfg.Providers["cohere-provider"] = ProviderConfig{
		Type:    "cohere",
		BaseURL: userURL,
		Keys: []ProviderKeyConfig{
			{ID: "key1", Value: "test-key"},
		},
	}
	cfg.Models["command-r"] = ModelConfig{
		Routes: []ModelRoute{
			{Provider: "cohere-provider", Model: "command-r", Weight: 1},
		},
	}

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("expected no validation error, got %v", err)
	}

	actual := cfg.Providers["cohere-provider"].BaseURL
	if actual != userURL {
		t.Errorf("expected Cohere BaseURL to preserve user value %q, got %q", userURL, actual)
	}
}

func TestValidate_MultipleErrors_FormattedAsNumberedList(t *testing.T) {
	cfg := Default()
	cfg.Server.Port = 0 // invalid: must be 1-65535
	cfg.Providers["bad-type"] = ProviderConfig{
		Type: "unsupported",
		Keys: []ProviderKeyConfig{{ID: "k1", Value: "test"}},
	}
	cfg.Models["m"] = ModelConfig{
		Routes: []ModelRoute{{Provider: "bad-type", Model: "m", Weight: 1}},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for multiple issues")
	}
	msg := err.Error()
	if !containsSubstr(msg, "1. ") {
		t.Errorf("expected numbered list with '1. ', got: %s", msg)
	}
	if !containsSubstr(msg, "2. ") {
		t.Errorf("expected numbered list with '2. ', got: %s", msg)
	}
}

func TestValidate_SingleError_NoNumber(t *testing.T) {
	cfg := Default()
	cfg.Server.Port = 0 // invalid

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for invalid port")
	}
	msg := err.Error()
	if containsSubstr(msg, "1. ") {
		t.Errorf("single error should not be numbered, got: %s", msg)
	}
}

// TestValidateAdminEnabledNoAuth verifies that admin.enabled=true with no auth method configured returns an error.
func TestValidateAdminEnabledNoAuth(t *testing.T) {
	cfg := Default()
	cfg.Admin.Enabled = true
	cfg.Admin.BearerToken = ""
	cfg.Admin.OIDC.Enabled = false
	cfg.Database.URL = "postgres://localhost/test" // satisfy DB check

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for admin with no auth, got nil")
	}
	expected := "admin is enabled but no authentication method is configured"
	if !containsSubstr(err.Error(), expected) {
		t.Errorf("expected error to contain %q, got %q", expected, err.Error())
	}
}

// TestValidateAdminEnabledWithBearerToken_NoError verifies that admin.enabled=true with a bearer token passes validation.
func TestValidateAdminEnabledWithBearerToken_NoError(t *testing.T) {
	cfg := Default()
	cfg.Admin.Enabled = true
	cfg.Admin.BearerToken = "secret-token"
	cfg.Admin.OIDC.Enabled = false
	cfg.Database.URL = "postgres://localhost/test"

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("expected no validation error, got %v", err)
	}
}

// TestValidateAdminEnabledWithOIDC_NoError verifies that admin.enabled=true with OIDC enabled passes validation.
func TestValidateAdminEnabledWithOIDC_NoError(t *testing.T) {
	cfg := Default()
	cfg.Admin.Enabled = true
	cfg.Admin.BearerToken = ""
	cfg.Admin.OIDC.Enabled = true
	cfg.Database.URL = "postgres://localhost/test"

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("expected no validation error, got %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
