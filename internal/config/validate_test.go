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
