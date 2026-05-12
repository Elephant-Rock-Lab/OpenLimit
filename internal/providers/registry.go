package providers

import "strings"

// ProviderDefault contains the default configuration for a known provider.
// When a user declares a provider by name in their config without specifying
// type or base_url, the registry fills in these defaults.
type ProviderDefault struct {
	// BaseType is the adapter type to use (e.g. "openai-compatible", "anthropic").
	BaseType string
	// BaseURL is the default API endpoint for the provider.
	BaseURL string
	// AuthHeader is the HTTP header used for authentication.
	// Common values: "Authorization", "x-api-key".
	AuthHeader string
	// AuthPrefix is the prefix prepended to the API key in the auth header.
	// Common values: "Bearer ", "" (empty for x-api-key style).
	AuthPrefix string
}

// DefaultRegistry maps lowercase provider names to their default configuration.
// All entries use the "openai-compatible" adapter type since these providers
// implement the OpenAI chat completions API.
//
// User-provided config always takes precedence over registry defaults (AR-01).
// Lookup is case-insensitive (AR-04).
var DefaultRegistry = map[string]ProviderDefault{
	// ── Major LLM Providers ──────────────────────────────────
	"deepseek": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.deepseek.com/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"together_ai": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.together.xyz/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"grok": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.x.ai/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"fireworks_ai": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.fireworks.ai/inference/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"perplexity": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.perplexity.ai",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"cerebras": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.cerebras.ai/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"sambanova": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.sambanova.ai/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"novita_ai": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.novita.ai/v3/openai",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"ai21": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.ai21.com/studio/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"xai": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.x.ai/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},

	// ── Open-Source / Self-Hosted ────────────────────────────
	"ollama": {
		BaseType:   "openai-compatible",
		BaseURL:    "http://localhost:11434/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"lm_studio": {
		BaseType:   "openai-compatible",
		BaseURL:    "http://localhost:1234/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"vllm": {
		BaseType:   "openai-compatible",
		BaseURL:    "http://localhost:8000/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"localai": {
		BaseType:   "openai-compatible",
		BaseURL:    "http://localhost:8080/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},

	// ── Cloud / Enterprise ───────────────────────────────────
	"databricks": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://dbc-dummy.cloud.databricks.com/serving-endpoints",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"snowflake": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://<account>.snowflakecomputing.com/api/v2",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"watsonx": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://us-south.ml.cloud.ibm.com/ml/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"replicate": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.replicate.com/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"hugging_face": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api-inference.huggingface.co/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},

	// ── Fast Inference / Edge ────────────────────────────────
	"deepinfra": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://api.deepinfra.com/v1/openai",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
	"lepton_ai": {
		BaseType:   "openai-compatible",
		BaseURL:    "https://llama2-7b.lepton.run/api/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	},
}

// LookupDefault returns the ProviderDefault for the given provider name.
// Lookup is case-insensitive. Returns false if the name is not in the registry.
func LookupDefault(name string) (ProviderDefault, bool) {
	d, ok := DefaultRegistry[strings.ToLower(name)]
	return d, ok
}

// ApplyDefaults fills in missing fields from the provider registry.
// User-provided config always takes precedence (AR-01).
// Provider type resolution order (AR-02):
//   1. config type field (if non-empty)
//   2. registry BaseType (if name is in registry)
//   3. fallback to "openai-compatible" (AR-03)
func ApplyDefaults(name string, cfg map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		result[k] = v
	}

	def, found := LookupDefault(name)

	// Fill type from registry if not set
	if typ, _ := result["type"].(string); typ == "" {
		if found && def.BaseType != "" {
			result["type"] = def.BaseType
		} else {
			result["type"] = "openai-compatible"
		}
	}

	// Fill base_url from registry if not set
	if url, _ := result["base_url"].(string); url == "" && found {
		result["base_url"] = def.BaseURL
	}

	return result
}

// ValidateProvider checks that a provider config has minimum required fields.
// Returns nil if valid, or an error describing what's missing.
//
// Known types with built-in default URLs (openai, anthropic, etc.) may have
// an empty baseURL — the adapter handles the default internally.
// Only registry-only providers (type resolved to "openai-compatible" from
// the registry) require an explicit baseURL.
func ValidateProvider(name string, providerType, baseURL string) error {
	// Types with built-in default URLs — empty baseURL is acceptable.
	builtInTypes := map[string]bool{
		"openai": true,
	}
	if builtInTypes[providerType] {
		return nil
	}

	if baseURL == "" {
		return &ProviderValidationError{
			Provider: name,
			Field:    "base_url",
			Message:  "provider has no base_url configured and is not in the registry",
		}
	}

	knownTypes := map[string]bool{
		"openai":            true,
		"openai-compatible": true,
		"anthropic":         true,
		"gemini":            true,
		"azure-openai":      true,
		"bedrock":           true,
		"vertex":            true,
		"groq":              true,
		"cohere":            true,
		"mistral":           true,
	}

	if !knownTypes[providerType] {
		return &ProviderValidationError{
			Provider: name,
			Field:    "type",
			Message:  "unknown adapter type: " + providerType,
		}
	}

	return nil
}

// ProviderValidationError describes a provider configuration problem.
type ProviderValidationError struct {
	Provider string
	Field    string
	Message  string
}

func (e *ProviderValidationError) Error() string {
	return "provider " + e.Provider + ": " + e.Field + ": " + e.Message
}
