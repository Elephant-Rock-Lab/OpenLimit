# Configuration

**What you'll learn:** Every section of `gateway.yaml` — server, providers, models, routing, cache, Redis, and telemetry. After reading this, you'll be able to configure any OpenLimit deployment from scratch.

The gateway loads configuration from `configs/gateway.yaml`. See `configs/gateway.example.yaml` for a complete example with all options.

---

## Server

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout_seconds: 30
  write_timeout_seconds: 120
  shutdown_timeout_seconds: 15
```

Controls the HTTP server binding, timeouts, and graceful shutdown behavior. `shutdown_timeout_seconds` determines how long the gateway waits for in-flight requests to complete before terminating.

---

## Database

```yaml
database:
  url: "postgres://openlimit:openlimit@localhost:5432/openlimit?sslmode=disable"
```

Postgres connection URL. Required for governance (virtual keys, budgets, audit logs) and semantic cache. The gateway runs migrations automatically on startup.

Environment variable fallback: `DATABASE_URL` is used when `database.url` is not in YAML.

---

## Providers

Each provider block defines an upstream AI service. The `type` field determines which adapter is used.

### OpenAI

```yaml
providers:
  openai:
    type: openai
    base_url: "https://api.openai.com/v1"   # optional, defaults to OpenAI
    keys:
      - id: primary
        env: OPENAI_API_KEY                  # reads API key from environment variable
        weight: 1
      - id: secondary
        env: OPENAI_API_KEY_2
        weight: 1
```

### Anthropic

```yaml
providers:
  anthropic:
    type: anthropic
    keys:
      - id: main
        env: ANTHROPIC_API_KEY
        weight: 1
```

### Google Gemini

```yaml
providers:
  gemini:
    type: gemini
    keys:
      - id: main
        env: GEMINI_API_KEY
        weight: 1
    gemini_model_map:
      fast: "gemini-2.0-flash"
      smart: "gemini-2.5-pro"
```

The `gemini_model_map` field is **required** for Gemini providers. It maps your logical model aliases to Gemini model IDs, because the Gemini API uses different model names than OpenAI.

### Azure OpenAI

```yaml
providers:
  azure:
    type: azure-openai
    keys:
      - id: eastus
        env: AZURE_OPENAI_API_KEY
        weight: 1
    azure_resource: "my-openai-resource"
    azure_api_version: "2024-12-01-preview"
```

The `azure_resource` field is **required** for Azure OpenAI providers. It constructs the deployment URL: `https://{azure_resource}.openai.azure.com/openai/deployments/{model}/chat/completions?api-version={azure_api_version}`.

The `azure_api_version` field defaults to `"2024-12-01-preview"` if not specified.

### OpenAI-compatible (Ollama, vLLM, etc.)

```yaml
providers:
  local:
    type: openai-compatible
    base_url: "http://localhost:11434/v1"
    keys:
      - id: default
        value: "ollama"            # static value, no env lookup
        weight: 1
```

Use `value` for static API keys or `env` for environment variable lookup.

### Provider key encryption

All provider types support encrypted keys via the `encrypted_value` field:

```yaml
providers:
  openai:
    type: openai
    keys:
      - id: main
        encrypted_value: "dek-v1:djE=:6gHx9...base64..."
        weight: 1
```

See [Security](security.md) for KMS setup.

---

## Models

Logical model aliases that route to provider + model pairs. Supports weighted routing and fallbacks.

```yaml
models:
  fast:
    provider: openai
    model: gpt-4o-mini
  smart:
    provider: openai
    model: gpt-4o
    fallbacks:
      - fast                    # if gpt-4o fails, try gpt-4o-mini
  balanced:
    routes:
      - provider: openai
        model: gpt-4o-mini
        weight: 70
      - provider: anthropic
        model: claude-3-haiku-20240307
        weight: 30
    fallbacks:
      - fast
```

Clients use the logical name (`"fast"`, `"smart"`) instead of provider-specific model IDs. The gateway resolves the route and fallback chain.

---

## Routing

### Defaults

```yaml
routing:
  defaults:
    timeout_seconds: 30
    max_retries: 2
    retry_on_status: [429, 500, 502, 503]
```

Retry uses exponential backoff. Only retryable status codes trigger retries.

### Region-aware routing

```yaml
routing:
  region: us-east               # gateway's own region
  region_strategy: priority     # "priority" (default) or "latency"
```

Add `regions` to each provider:

```yaml
providers:
  openai:
    type: openai
    keys:
      - id: primary
        env: OPENAI_API_KEY
    regions:
      - name: us-east
        base_url: https://api.openai.com/v1
        priority: 1
      - name: eu-west
        base_url: https://eu.api.openai.com/v1
        priority: 2
        data_residency: eu
```

**Priority strategy** (default): Routes sorted by priority. When the gateway's `routing.region` matches a route's region and priorities are tied, the local region wins.

**Latency strategy**: Requires `telemetry.metrics.enabled: true`. Routes to the region with the lowest observed p50 latency. Cold start falls back to priority.

**Data residency**: Send `X-Data-Residency: eu` to restrict routing to EU regions. See [Security](security.md) for details.

### Cost weights (smart routing)

When using `smart` routing strategy, configure the weight of each factor:

```yaml
routing:
  strategy: smart
  cost_weights:
    cost: 0.4          # weight for provider cost (0.0–1.0)
    latency: 0.4       # weight for observed latency (0.0–1.0)
    health: 0.2        # weight for provider health score (0.0–1.0)
```

The `smart` strategy computes a weighted score for each route:
- **Cost**: Based on the embedded pricing catalog (22 entries). Lower cost = higher score.
- **Latency**: Based on observed P50 latency from Prometheus metrics. Lower latency = higher score.
- **Health**: Based on circuit breaker state and failure count. Healthier = higher score.

Scores are normalized to 0.0–1.0. Missing data defaults to the median score. The route with the highest combined score wins.

Use `GET /admin/routing/costs` to view the pricing catalog and current strategy.

---

### Replay & A/B Testing

Configure shadow traffic for provider comparison without affecting production:

```yaml
replay:
  enabled: true
  routes:
    - model: smart
      shadow_provider: anthropic
      shadow_model: claude-3-haiku-20240307
      sample_rate: 0.1   # replay 10% of requests
    - model: fast
      shadow_provider: together
      shadow_model: meta-llama/Meta-Llama-3-8B-Instruct
      sample_rate: 0.05  # replay 5% of requests
```

| Field | Required | Description |
|:------|:---------|:------------|
| `replay.enabled` | Yes | Enable/disable replay globally |
| `replay.routes[]` | Yes | List of routes to replay |
| `routes[].model` | Yes | Logical model alias to match (must exist in `models` config) |
| `routes[].shadow_provider` | Yes | Provider to send shadow traffic to |
| `routes[].shadow_model` | Yes | Model to use for shadow requests |
| `routes[].sample_rate` | Yes | Fraction of requests to replay (0.0–1.0) |

**How it works:**
1. Primary request executes normally and returns to client
2. If `sample_rate` passes, a shadow request fires in a background goroutine
3. Shadow results stored in a ring buffer (last 1000 results)
4. `GET /admin/routing/replay` returns results + summary stats

**Summary stats include:** avg primary latency, avg shadow latency, shadow error rate, total replayed requests.

Use replay to compare providers, validate migrations, and measure latency differences — all without risking production traffic.

---

## Cache

### Exact cache (LRU)

```yaml
cache:
  exact:
    enabled: true
    max_entries: 1000
    ttl_seconds: 300
```

In-memory LRU cache for non-streaming responses. O(1) lookup by request hash. When Redis is enabled, the exact cache is backed by Redis for shared state across pods.

### Semantic cache

```yaml
cache:
  semantic:
    enabled: true
    embedder:
      type: "ollama"                        # "ollama" or "openai"
      base_url: "http://localhost:11434"
      model: "nomic-embed-text"
      dimensions: 768
    similarity_threshold: 0.92             # cosine similarity
    max_entries: 10000
    ttl_seconds: 3600
    embedding_cache:
      max_entries: 5000
      ttl_seconds: 3600
```

Requires:
- **pgvector extension** in Postgres
- An **embedding endpoint** (Ollama, OpenAI, or compatible)

**How it works:**
1. Extract text from the last user message
2. Compute embedding (cached in-memory to avoid repeated API calls)
3. Search pgvector for similar entries above the similarity threshold
4. Cache miss → provider call → store response + embedding for future queries

**Graceful degradation:** If pgvector is unavailable, semantic cache disables itself. If the embedder goes down, the circuit breaker opens and all requests become cache misses. The gateway continues serving with exact cache only.

### Tiered lookup order

1. Exact match (O(1) hash lookup)
2. Semantic match (vector similarity search)
3. Provider call (and cache the result in both layers)

---

## Redis

Enable Redis for horizontal scalability — shared rate limits, shared cache, and shared circuit breaker state across multiple gateway instances.

```yaml
redis:
  enabled: true
  addr: "localhost:6379"
  # password: ""
  # db: 0
  # max_retries: 3
  # pool_size: 20
  # health_check_interval_seconds: 10
```

### How Redis integration works

| Subsystem | Redis healthy | Redis down / disabled |
|---|---|---|
| Rate limiting | Shared sliding window (accurate across pods) | Local token bucket (per-instance) |
| Exact cache | Shared Redis cache | Local LRU |
| Circuit breaker | Shared state across pods | Local-only breaker |

When Redis is unavailable, all subsystems fall back to local-only mode. The `gateway_redis_healthy` gauge metric (1=healthy, 0=degraded) signals connectivity.

### Limitations

- No Redis Cluster support — single-node or Sentinel only. Cluster planned for v1.1.
- Cache values are not encrypted in Redis — run Redis in a private VPC.

---

## Telemetry

### Metrics (Prometheus)

```yaml
telemetry:
  metrics:
    enabled: true
```

Scrape from `GET /metrics`. See [Observability](observability.md) for the full metrics reference.

### Tracing (OpenTelemetry)

```yaml
telemetry:
  tracing:
    enabled: true
    endpoint: "localhost:4317"    # OTLP gRPC (Jaeger, Tempo, etc.)
    service_name: "openlimit"
    sample_rate: 0.1             # 10% of traces
```

Every response includes a `traceparent` header for end-to-end correlation when tracing is enabled. `X-Request-ID` is included on every response regardless.

---

## Auth & Admin

```yaml
auth:
  enabled: true                  # false = no virtual key required (open gateway)

admin:
  enabled: true
  bearer_token: "your-admin-secret"    # or set ADMIN_TOKEN env var
  rbac_enabled: false                   # enable for role-based access
```

See [Governance](governance.md) for RBAC, OIDC, and admin API details.

---

## Billing prices

```yaml
billing:
  prices:
    gpt-4o:
      prompt_per_1m: 2.50
      completion_per_1m: 10.00
    gpt-4o-mini:
      prompt_per_1m: 0.15
      completion_per_1m: 0.60
```

Per-model pricing in USD per 1M tokens. Used for budget enforcement and cost tracking.

---

## Guardrails

```yaml
guardrails:
  enabled: true
  input:
    - type: pii
      config:
        types: [credit_card, ssn, email, phone]
        action: redact
  output:
    - type: webhook
      config:
        url: "http://localhost:8888/moderate"
        timeout_ms: 250
  models:
    fast:
      input: true
      output: true
```

See [Governance](governance.md) for all guardrail stages and per-model configuration.

---

## Agent protocols

```yaml
mcp:
  enabled: true
  servers:
    - name: weather
      url: "http://localhost:3001/mcp"
      tool_prefix: "weather"

mcp_server:
  enabled: true
  endpoint: "/mcp"

a2a:
  enabled: true
  endpoint: "/a2a"
  url: "http://localhost:8080"
```

See [Agent Protocols](agent-protocols.md) for full MCP and A2A configuration.

---

## Environment variable fallbacks

| Variable | Used when |
|---|---|
| `DATABASE_URL` | `database.url` not in YAML |
| `ADMIN_TOKEN` | `admin.bearer_token` not in YAML |
| `KMS_STATIC_KEY` | KMS type is `static` |

---

## Plugins

The `plugins` section registers custom plugins that extend the gateway with custom guardrails, HTTP middleware, or provider adapters.

```yaml
plugins:
  - name: example-length-guardrail
    type: guardrail
    config:
      min_length: 10
      max_length: 5000

  - name: header-injector
    type: middleware
    config:
      headers:
        X-Custom-Header: "my-value"
```

| Field | Required | Description |
|:------|:---------|:------------|
| `name` | Yes | Must match the plugin's registered name |
| `type` | Yes | `"guardrail"`, `"middleware"`, or `"provider"` |
| `config` | No | Plugin-specific configuration passed to `Init()` |

For guardrail plugins, reference them in the guardrail pipeline:

```yaml
guardrails:
  input:
    - type: plugin
      config:
        name: example-length-guardrail
```

See [Plugins](plugins.md) for the full plugin development guide with examples.

---

## Next steps

- **[Governance](governance.md)** — Configure virtual keys, budgets, guardrails, RBAC
- **[Plugins](plugins.md)** — Write custom guardrails, middleware, and provider adapters
- **[Agent Protocols](agent-protocols.md)** — Set up MCP or A2A
- **[Security](security.md)** — Encrypt provider keys with KMS
- **[Deployment](deployment.md)** — Deploy with Docker Compose or Helm
