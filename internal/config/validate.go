package config

import (
	"fmt"
	"strings"
)

var supportedProviderTypes = map[string]bool{
	"":                  true,
	"openai":            true,
	"openai-compatible": true,
	"anthropic":         true,
	"gemini":            true,
	"azure-openai":      true,
}

func Validate(cfg Config) error {
	var errs []string

	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		errs = append(errs, "server.port must be between 1 and 65535")
	}

	for name, provider := range cfg.Providers {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, "provider name cannot be empty")
			continue
		}
		if !supportedProviderTypes[provider.Type] {
			errs = append(errs, fmt.Sprintf("provider %q has unsupported type %q", name, provider.Type))
		}
		if provider.Type == "openai-compatible" && strings.TrimSpace(provider.BaseURL) == "" {
			errs = append(errs, fmt.Sprintf("provider %q type openai-compatible requires base_url", name))
		}
		switch provider.Type {
		case "gemini":
			if len(provider.GeminiModelMap) == 0 {
				errs = append(errs, fmt.Sprintf("provider %q: gemini_model_map is required for gemini provider type", name))
			}
		case "azure-openai":
			if strings.TrimSpace(provider.AzureResource) == "" {
				errs = append(errs, fmt.Sprintf("provider %q: azure_resource is required for azure-openai provider type", name))
			}
			if strings.TrimSpace(provider.AzureAPIVersion) == "" {
				provider.AzureAPIVersion = "2025-06-01"
				cfg.Providers[name] = provider
			}
		}
		for i, key := range provider.Keys {
			if strings.TrimSpace(key.ID) == "" {
				errs = append(errs, fmt.Sprintf("provider %q key %d requires id", name, i))
			}
			if strings.TrimSpace(key.Env) == "" && strings.TrimSpace(key.Value) == "" {
				errs = append(errs, fmt.Sprintf("provider %q key %q requires env or value", name, key.ID))
			}
		}
		validateRegions(&errs, name, provider.Regions)
	}

	for name, model := range cfg.Models {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, "model name cannot be empty")
			continue
		}
		if len(model.Routes) == 0 {
			errs = append(errs, fmt.Sprintf("model %q requires at least one route", name))
		}
		validateRoutes(&errs, cfg, name, "route", model.Routes)
		validateRoutes(&errs, cfg, name, "fallback", model.Fallbacks)
	}

	if cfg.Routing.Defaults.Retry.Attempts < 0 {
		errs = append(errs, "routing.defaults.retry.attempts cannot be negative")
	}
	if rs := strings.ToLower(cfg.Routing.RegionStrategy); rs != "" && rs != "priority" && rs != "latency" {
		errs = append(errs, "routing.region_strategy must be \"priority\" or \"latency\"")
	}

	if cfg.Auth.Enabled && strings.TrimSpace(cfg.Database.URL) == "" {
		errs = append(errs, "auth is enabled but database.url is not configured")
	}
	if cfg.Admin.Enabled && strings.TrimSpace(cfg.Database.URL) == "" {
		errs = append(errs, "admin is enabled but database.url is not configured")
	}

	if cfg.Telemetry.Tracing.Enabled {
		if cfg.Telemetry.Tracing.SampleRate <= 0 || cfg.Telemetry.Tracing.SampleRate > 1 {
			errs = append(errs, "telemetry.tracing.sample_rate must be between 0 and 1")
		}
	}

	if cfg.Cache.Exact.MaxEntries < 0 {
		errs = append(errs, "cache.exact.max_entries cannot be negative")
	}
	if cfg.Cache.Exact.TTLSeconds < 0 {
		errs = append(errs, "cache.exact.ttl_seconds cannot be negative")
	}

	// Guardrails validation
	if cfg.Guardrails.Enabled {
		supportedTypes := map[string]bool{
			"pii": true, "regex": true, "keyword": true,
			"length": true, "webhook": true, "json_schema": true,
		}
		for i, stage := range cfg.Guardrails.Input {
			if !supportedTypes[stage.Type] {
				errs = append(errs, fmt.Sprintf("guardrails.input[%d] has unknown type %q", i, stage.Type))
			}
		}
		for i, stage := range cfg.Guardrails.Output {
			if !supportedTypes[stage.Type] {
				errs = append(errs, fmt.Sprintf("guardrails.output[%d] has unknown type %q", i, stage.Type))
			}
		}
	}

	// Semantic cache validation
	if cfg.Cache.Semantic.Enabled {
		if strings.TrimSpace(cfg.Database.URL) == "" {
			errs = append(errs, "cache.semantic is enabled but database.url is not configured")
		}
		etype := strings.ToLower(cfg.Cache.Semantic.Embedder.Type)
		if etype != "openai" && etype != "ollama" {
			errs = append(errs, "cache.semantic.embedder.type must be \"openai\" or \"ollama\"")
		}
		if strings.TrimSpace(cfg.Cache.Semantic.Embedder.BaseURL) == "" {
			errs = append(errs, "cache.semantic.embedder.base_url is required")
		}
		if cfg.Cache.Semantic.SimilarityThreshold < 0 || cfg.Cache.Semantic.SimilarityThreshold > 1 {
			errs = append(errs, "cache.semantic.similarity_threshold must be between 0 and 1")
		}
	}

	// MCP validation
	validateMCP(cfg.MCP, &errs)

	// Redis validation
	if cfg.Redis.Enabled {
		if strings.TrimSpace(cfg.Redis.Addr) == "" {
			errs = append(errs, "redis.addr is required when redis is enabled")
		}
		if cfg.Redis.PoolSize < 0 {
			errs = append(errs, "redis.pool_size cannot be negative")
		}
		if cfg.Redis.HealthCheckIntervalSec < 0 {
			errs = append(errs, "redis.health_check_interval_seconds cannot be negative")
		}
	}

	// KMS validation
	if cfg.KMS.Enabled {
		switch cfg.KMS.Type {
		case "static", "aws-kms", "vault":
			// valid
		default:
			errs = append(errs, "kms.type must be 'static', 'aws-kms', or 'vault'")
		}
		if cfg.KMS.Type == "aws-kms" && strings.TrimSpace(cfg.KMS.KeyID) == "" {
			errs = append(errs, "kms.key_id is required when kms.type is aws-kms")
		}
		if cfg.KMS.Type == "vault" && strings.TrimSpace(cfg.KMS.Vault.Addr) == "" {
			errs = append(errs, "kms.vault.addr is required when kms.type is vault")
		}
	}

	// A2A validation
	if cfg.A2A.Enabled {
		if strings.TrimSpace(cfg.A2A.DefaultModel) == "" {
			errs = append(errs, "a2a.default_model is required when a2a is enabled")
		}
		if cfg.A2A.Authentication.Mode == "bearer_token" && strings.TrimSpace(cfg.A2A.Authentication.BearerToken) == "" {
			errs = append(errs, "a2a.authentication.bearer_token is required when mode=bearer_token")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid config: %s", strings.Join(errs, "; "))
	}
	return nil
}

func validateMCP(cfg MCPConfig, errs *[]string) {
	if !cfg.Enabled {
		return
	}

	if cfg.MaxToolRounds < 0 || cfg.MaxToolRounds > 20 {
		*errs = append(*errs, "mcp.max_tool_rounds must be between 1 and 20")
	}
	if cfg.ToolTimeoutMS < 0 {
		*errs = append(*errs, "mcp.tool_timeout_ms cannot be negative")
	}
	if cfg.MaxTotalDurationSec < 0 {
		*errs = append(*errs, "mcp.max_total_duration_seconds cannot be negative")
	}
	if cfg.MaxResultBytes < 0 {
		*errs = append(*errs, "mcp.max_result_bytes cannot be negative")
	}
	if cfg.ToolConflictStrategy != "" && cfg.ToolConflictStrategy != "skip" && cfg.ToolConflictStrategy != "error" {
		*errs = append(*errs, "mcp.tool_conflict_strategy must be \"skip\" or \"error\"")
	}

	names := make(map[string]bool)
	for i, server := range cfg.Servers {
		if strings.TrimSpace(server.Name) == "" {
			*errs = append(*errs, fmt.Sprintf("mcp.servers[%d] requires name", i))
		}
		if strings.TrimSpace(server.URL) == "" {
			*errs = append(*errs, fmt.Sprintf("mcp.servers[%d] requires url", i))
		}
		if !strings.HasPrefix(server.URL, "http://") && !strings.HasPrefix(server.URL, "https://") {
			*errs = append(*errs, fmt.Sprintf("mcp.servers[%d].url must be a valid HTTP(S) URL", i))
		}
		if strings.TrimSpace(server.ToolPrefix) == "" {
			*errs = append(*errs, fmt.Sprintf("mcp.servers[%d] requires tool_prefix", i))
		}
		if names[server.Name] {
			*errs = append(*errs, fmt.Sprintf("mcp.servers[%d] has duplicate name %q", i, server.Name))
		}
		names[server.Name] = true
	}
}

func validateRoutes(errs *[]string, cfg Config, modelName string, label string, routes []ModelRoute) {
	for i, route := range routes {
		if strings.TrimSpace(route.Provider) == "" {
			*errs = append(*errs, fmt.Sprintf("model %q %s %d requires provider", modelName, label, i))
			continue
		}
		if _, ok := cfg.Providers[route.Provider]; !ok {
			*errs = append(*errs, fmt.Sprintf("model %q %s %d references unknown provider %q", modelName, label, i, route.Provider))
		}
		if strings.TrimSpace(route.Model) == "" {
			*errs = append(*errs, fmt.Sprintf("model %q %s %d requires model", modelName, label, i))
		}
		if route.Weight < 0 {
			*errs = append(*errs, fmt.Sprintf("model %q %s %d weight cannot be negative", modelName, label, i))
		}
	}
}

func validateRegions(errs *[]string, providerName string, regions []RegionConfig) {
	seen := make(map[string]bool)
	for i, region := range regions {
		if strings.TrimSpace(region.Name) == "" {
			*errs = append(*errs, fmt.Sprintf("provider %q region %d requires name", providerName, i))
		}
		if strings.EqualFold(region.Name, "default") {
			*errs = append(*errs, fmt.Sprintf("provider %q region %d: name %q is reserved", providerName, i, region.Name))
		}
		if strings.TrimSpace(region.BaseURL) == "" {
			*errs = append(*errs, fmt.Sprintf("provider %q region %d requires base_url", providerName, i))
		}
		nameLower := strings.ToLower(region.Name)
		if seen[nameLower] {
			*errs = append(*errs, fmt.Sprintf("provider %q has duplicate region name %q", providerName, region.Name))
		}
		seen[nameLower] = true
	}
}
