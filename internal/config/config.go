package config

import (
	"fmt"
	"net"
)

// Config is the top-level gateway configuration loaded from gateway.yaml.
type Config struct {
	Server     ServerConfig              `yaml:"server"`
	Database   DatabaseConfig            `yaml:"database"`
	Redis      RedisConfig               `yaml:"redis"`
	KMS        KMSConfig                 `yaml:"kms"`
	Auth       AuthConfig                `yaml:"auth"`
	Admin      AdminConfig               `yaml:"admin"`
	Billing    BillingConfig             `yaml:"billing"`
	Telemetry  TelemetryConfig           `yaml:"telemetry"`
	Logging    LoggingConfig             `yaml:"logging"`
	Cache      CacheConfig               `yaml:"cache"`
	Guardrails GuardrailsConfig          `yaml:"guardrails"`
	MCP        MCPConfig                 `yaml:"mcp"`
	MCPServer  MCPServerModeConfig       `yaml:"mcp_server"`
	A2A        A2AConfig                 `yaml:"a2a"`
	Routing    RoutingConfig             `yaml:"routing"`
	Providers  map[string]ProviderConfig `yaml:"providers"`
	Models     map[string]ModelConfig    `yaml:"models"`
	Plugins    []PluginConfig            `yaml:"plugins"`
}

type DatabaseConfig struct {
	URL          string `yaml:"url"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

type RedisConfig struct {
	Enabled                bool   `yaml:"enabled"`
	Addr                   string `yaml:"addr"`
	Password               string `yaml:"password"`
	DB                     int    `yaml:"db"`
	MaxRetries             int    `yaml:"max_retries"`
	PoolSize               int    `yaml:"pool_size"`
	HealthCheckIntervalSec int    `yaml:"health_check_interval_seconds"`
	Cluster                bool   `yaml:"cluster"`
}

// KMSConfig configures the key management service for provider key encryption.
type KMSConfig struct {
	Enabled bool           `yaml:"enabled"`
	Type    string         `yaml:"type"`   // "static" | "aws-kms" | "vault"
	KeyID   string         `yaml:"key_id"` // KMS key ID, label, or Vault secret path
	Vault   VaultKMSConfig `yaml:"vault"`
}

type VaultKMSConfig struct {
	Addr          string `yaml:"addr"`
	Token         string `yaml:"token"`
	Namespace     string `yaml:"namespace"`
	TLSSkipVerify bool   `yaml:"tls_skip_verify"`
}

type AuthConfig struct {
	Enabled        bool `yaml:"enabled"`
	KeyCacheSize   int  `yaml:"key_cache_size"`
	KeyCacheTTLSec int  `yaml:"key_cache_ttl_seconds"`
}

type AdminConfig struct {
	Enabled     bool       `yaml:"enabled"`
	BearerToken string     `yaml:"bearer_token"`
	RBACEnabled bool       `yaml:"rbac_enabled"`
	OIDC        OIDCConfig `yaml:"oidc"`
}

type OIDCConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Issuer       string `yaml:"issuer"`
	Audience     string `yaml:"audience"`
	DefaultRole  string `yaml:"default_role"`
	JWKSCacheTTL int    `yaml:"jwks_cache_ttl_seconds"`
	// Multi-tenant: additional providers (when set, Issuer/Audience above is provider[0])
	Providers    []OIDCProviderConfig `yaml:"providers"`
}

type OIDCProviderConfig struct {
	Issuer       string `yaml:"issuer"`
	Audience     string `yaml:"audience"`
	DefaultRole  string `yaml:"default_role"`
	JWKSCacheTTL int    `yaml:"jwks_cache_ttl_seconds"`
}

type BillingConfig struct {
	Prices []PriceEntry `yaml:"prices"`
}

type TelemetryConfig struct {
	Metrics MetricsConfig `yaml:"metrics"`
	Tracing TracingConfig `yaml:"tracing"`
}

type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`
}

type TracingConfig struct {
	Enabled     bool    `yaml:"enabled"`
	Endpoint    string  `yaml:"endpoint"`
	ServiceName string  `yaml:"service_name"`
	SampleRate  float64 `yaml:"sample_rate"`
}

type PriceEntry struct {
	Provider        string  `yaml:"provider"`
	Model           string  `yaml:"model"`
	PromptPer1M     float64 `yaml:"prompt_per_1m"`
	CompletionPer1M float64 `yaml:"completion_per_1m"`
}

type ServerConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	ReadTimeoutMS   int    `yaml:"read_timeout_ms"`
	WriteTimeoutMS  int    `yaml:"write_timeout_ms"`
	IdleTimeoutMS   int    `yaml:"idle_timeout_ms"`
	ShutdownTimeout int    `yaml:"shutdown_timeout_ms"`
}

type LoggingConfig struct {
	Level         string `yaml:"level"`
	Format        string `yaml:"format"`
	AddSource     bool   `yaml:"add_source"`
	RedactPrompts bool   `yaml:"redact_prompts"`
	LogBodies     bool   `yaml:"log_bodies"` // capture request/response bodies in audit log
}

type GuardrailsConfig struct {
	Enabled bool                            `yaml:"enabled"`
	Input   []GuardrailStageConfig          `yaml:"input"`
	Output  []GuardrailStageConfig          `yaml:"output"`
	Models  map[string]GuardrailModelConfig `yaml:"models"`
}

type GuardrailStageConfig struct {
	Type   string         `yaml:"type"`
	Config map[string]any `yaml:"config"`
}

type GuardrailModelConfig struct {
	Input  bool `yaml:"input"`
	Output bool `yaml:"output"`
}

type CacheConfig struct {
	Exact    ExactCacheConfig    `yaml:"exact"`
	Semantic SemanticCacheConfig `yaml:"semantic"`
}

type ExactCacheConfig struct {
	Enabled    bool `yaml:"enabled"`
	MaxEntries int  `yaml:"max_entries"`
	TTLSeconds int  `yaml:"ttl_seconds"`
}

type SemanticCacheConfig struct {
	Enabled             bool                 `yaml:"enabled"`
	Embedder            EmbedderConfig       `yaml:"embedder"`
	SimilarityThreshold float64              `yaml:"similarity_threshold"`
	MaxEntries          int                  `yaml:"max_entries"`
	TTLSeconds          int                  `yaml:"ttl_seconds"`
	EmbeddingCache      EmbeddingCacheConfig `yaml:"embedding_cache"`
}

type EmbedderConfig struct {
	Type       string `yaml:"type"`
	BaseURL    string `yaml:"base_url"`
	Model      string `yaml:"model"`
	APIKey     string `yaml:"api_key"`
	Dimensions int    `yaml:"dimensions"`
}

type EmbeddingCacheConfig struct {
	MaxEntries int `yaml:"max_entries"`
	TTLSeconds int `yaml:"ttl_seconds"`
}

type RoutingConfig struct {
	Defaults       RouteDefaults `yaml:"defaults"`
	Region         string        `yaml:"region"`          // gateway's own region, e.g., "us-east"
	RegionStrategy string        `yaml:"region_strategy"` // "priority" (default) or "latency"
}

type RouteDefaults struct {
	TimeoutMS int         `yaml:"timeout_ms"`
	Retry     RetryConfig `yaml:"retry"`
}

type RetryConfig struct {
	Attempts  int      `yaml:"attempts"`
	Backoff   string   `yaml:"backoff"`
	InitialMS int      `yaml:"initial_ms"`
	MaxMS     int      `yaml:"max_ms"`
	RetryOn   []string `yaml:"retry_on"`
}

type ProviderConfig struct {
	Type            string              `yaml:"type"`
	BaseURL         string              `yaml:"base_url"`
	Keys            []ProviderKeyConfig `yaml:"keys"`
	Regions         []RegionConfig      `yaml:"regions"`
	GeminiModelMap  map[string]string   `yaml:"gemini_model_map"`
	AzureResource   string              `yaml:"azure_resource"`
	AzureAPIVersion string              `yaml:"azure_api_version"`
	Region          string              `yaml:"region"`    // AWS region (bedrock) or Vertex region
	Project         string              `yaml:"project"`   // GCP project ID (vertex)
	Publisher       string              `yaml:"publisher"` // Vertex publisher: "google" or "google-genai"
}

type RegionConfig struct {
	Name          string `yaml:"name"`
	BaseURL       string `yaml:"base_url"`
	Priority      int    `yaml:"priority"`       // 1 = highest (default: 1)
	DataResidency string `yaml:"data_residency"` // e.g., "eu", "us" (optional)
}

type ProviderKeyConfig struct {
	ID             string `yaml:"id"`
	Env            string `yaml:"env"`
	Value          string `yaml:"value"`
	EncryptedValue string `yaml:"encrypted_value"`
	Weight         int    `yaml:"weight"`
}

type ModelConfig struct {
	Routes    []ModelRoute `yaml:"routes"`
	Fallbacks []ModelRoute `yaml:"fallbacks"`
}

// MCPConfig configures the MCP client integration.
type MCPConfig struct {
	Enabled              bool              `yaml:"enabled"`
	MaxToolRounds        int               `yaml:"max_tool_rounds"`
	ToolTimeoutMS        int               `yaml:"tool_timeout_ms"`
	MaxTotalDurationSec  int               `yaml:"max_total_duration_seconds"`
	MaxResultBytes       int               `yaml:"max_result_bytes"`
	AutoInjectTools      bool              `yaml:"auto_inject_tools"`
	ToolConflictStrategy string            `yaml:"tool_conflict_strategy"`
	Servers              []MCPServerConfig `yaml:"servers"`
}

// MCPServerConfig configures a single MCP server connection.
type MCPServerConfig struct {
	Name       string            `yaml:"name"`
	URL        string            `yaml:"url"`
	Headers    map[string]string `yaml:"headers"`
	TimeoutMS  int               `yaml:"timeout_ms"`
	ToolPrefix string            `yaml:"tool_prefix"`
}

// MCPServerModeConfig configures the gateway's MCP server mode.
// When enabled, external MCP clients can connect and call virtual keys as tools.
type MCPServerModeConfig struct {
	Enabled       bool          `yaml:"enabled"`
	Endpoint      string        `yaml:"endpoint"`
	Auth          MCPAuthConfig `yaml:"authentication"`
	SessionTTLSec int           `yaml:"session_ttl_seconds"`
}

// MCPAuthConfig configures authentication for the MCP server endpoint.
type MCPAuthConfig struct {
	Mode        string `yaml:"mode"` // "none", "bearer_token", "virtual_key"
	BearerToken string `yaml:"bearer_token"`
}

// IsSeparatePort returns true if the endpoint is a separate listen address (starts with ":").
func (c MCPServerModeConfig) IsSeparatePort() bool {
	return c.Enabled && len(c.Endpoint) > 0 && c.Endpoint[0] == ':'
}

// Path returns the URL path for the MCP server endpoint.
// If endpoint is a separate port, defaults to "/mcp".
func (c MCPServerModeConfig) Path() string {
	if c.IsSeparatePort() {
		return "/mcp"
	}
	if c.Endpoint != "" {
		return c.Endpoint
	}
	return "/mcp"
}

// ListenAddr returns the listen address for a separate-port setup, or empty string.
func (c MCPServerModeConfig) ListenAddr() string {
	if c.IsSeparatePort() {
		return c.Endpoint
	}
	return ""
}

// A2AConfig configures the gateway's A2A server mode.
// When enabled, external agents can discover and interact with the gateway
// using the Agent-to-Agent (A2A) protocol v1.0 over JSON-RPC 2.0.
type A2AConfig struct {
	Enabled        bool            `yaml:"enabled"`
	Endpoint       string          `yaml:"endpoint"` // path ("/a2a") or separate port (":8082")
	URL            string          `yaml:"url"`      // public URL for agent card
	Authentication MCPAuthConfig   `yaml:"authentication"`
	DefaultModel   string          `yaml:"default_model"` // model used for message/send
	TaskTTLSec     int             `yaml:"task_ttl_seconds"`
	MaxTasks       int             `yaml:"max_tasks"`
	MaxWorkers     int             `yaml:"max_workers"`   // concurrent background task executors
	BlockingMode   bool            `yaml:"blocking_mode"` // true = block until complete, false = return immediately
	AgentCard      AgentCardConfig `yaml:"agent_card"`
}

// AgentCardConfig configures the A2A agent card discovery document.
type AgentCardConfig struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
}

// A2AIsSeparatePort returns true if the endpoint is a separate listen address.
func (c A2AConfig) A2AIsSeparatePort() bool {
	return c.Enabled && len(c.Endpoint) > 0 && c.Endpoint[0] == ':'
}

// A2APath returns the URL path for the A2A endpoint.
func (c A2AConfig) A2APath() string {
	if c.A2AIsSeparatePort() {
		return "/a2a"
	}
	if c.Endpoint != "" {
		return c.Endpoint
	}
	return "/a2a"
}

// A2AListenAddr returns the listen address for a separate-port setup.
func (c A2AConfig) A2AListenAddr() string {
	if c.A2AIsSeparatePort() {
		return c.Endpoint
	}
	return ""
}

type ModelRoute struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Weight   int    `yaml:"weight"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			Host:           "0.0.0.0",
			Port:           8080,
			ReadTimeoutMS:  15000,
			WriteTimeoutMS: 15000,
			IdleTimeoutMS:  60000,
		},
		Database: DatabaseConfig{
			MaxOpenConns: 25,
			MaxIdleConns: 5,
		},
		Auth: AuthConfig{
			Enabled:        false,
			KeyCacheSize:   10000,
			KeyCacheTTLSec: 60,
		},
		Redis: RedisConfig{
			Enabled:                false,
			Addr:                   "localhost:6379",
			MaxRetries:             3,
			PoolSize:               20,
			HealthCheckIntervalSec: 10,
		},
		KMS: KMSConfig{
			Enabled: false,
			Type:    "static",
		},
		Admin: AdminConfig{
			Enabled: false,
		},
		Logging: LoggingConfig{
			Level:         "info",
			Format:        "json",
			RedactPrompts: true,
		},
		Cache: CacheConfig{
			Exact: ExactCacheConfig{
				Enabled:    true,
				MaxEntries: 10000,
				TTLSeconds: 3600,
			},
		},
		Guardrails: GuardrailsConfig{
			Enabled: false,
		},
		Routing: RoutingConfig{
			Defaults: RouteDefaults{
				TimeoutMS: 60000,
				Retry: RetryConfig{
					Attempts:  3,
					Backoff:   "exponential",
					InitialMS: 250,
					MaxMS:     4000,
					RetryOn:   []string{"rate_limit", "timeout", "server_error"},
				},
			},
		},
		Providers: map[string]ProviderConfig{},
		Models:    map[string]ModelConfig{},
	}
}

// PluginConfig configures a single plugin.
type PluginConfig struct {
	Name   string         `yaml:"name"`
	Type   string         `yaml:"type"`   // "guardrail", "middleware", "provider"
	Config map[string]any `yaml:"config"` // Plugin-specific configuration
}

func (s ServerConfig) Address() string {
	return net.JoinHostPort(s.Host, fmt.Sprintf("%d", s.Port))
}
