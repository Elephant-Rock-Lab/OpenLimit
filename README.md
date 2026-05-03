# OpenLimit

> The open-source control plane for AI operations.

OpenLimit is a production-grade AI gateway with virtual key governance, agent protocol support (MCP & A2A), multi-provider routing, and full observability — designed for teams that need control over how AI is consumed across their organization.

Inspired by Bifrost, Agentgateway, LiteLLM, and Portkey.

---

## Quick Start

```bash
# Build and run
make run
```

Create a project and virtual key in one step:

```bash
curl -X POST http://localhost:8080/admin/quickstart \
  -H "Authorization: Bearer your-admin-secret" \
  -H "Content-Type: application/json"
```

Make your first request:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer gw-<key-from-response>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "fast",
    "messages": [{"role": "user", "content": "Say hello from OpenLimit"}]
  }'
```

For the full walkthrough (Docker Compose, Postgres, guardrails), see [Getting Started](docs/getting-started.md).

---

## Features

| Feature | What it does | Why it matters |
|---|---|---|
| **Multi-provider routing** | OpenAI, Anthropic, Gemini, Azure OpenAI, and any OpenAI-compatible endpoint (Ollama, vLLM). Weighted routing with fallbacks. | Avoid vendor lock-in. Route to the cheapest or fastest provider. |
| **Agent-native protocols** | MCP client (tool discovery + merge), MCP server mode (expose keys as tools), A2A 1.0 (agent-to-agent tasks with SSE streaming). | First-class support for AI agents, not just chat completions. |
| **Governance** | Virtual API keys, per-key RPM/TPM rate limits, daily/monthly budgets, project-based multi-tenancy, per-key model restrictions. | Control who can use what, how much, and how often. |
| **Observability** | 30+ Prometheus metrics, OpenTelemetry tracing, Grafana dashboard, structured logging. Every response includes `X-Request-ID`. | Know exactly what's happening across every provider, key, and model. |
| **Deployment-ready** | Docker Compose (with profiles for Redis, monitoring), Helm chart with HPA and ServiceMonitor, production security defaults. | Deploy the same way in dev and prod. |
| **Open source** | MIT licensed. No vendor tie-in. Runs on your infrastructure. Data stays in your VPC. | Full control, full transparency. |

---

## Architecture

```
cmd/gateway/main.go          # Entrypoint
internal/
  admin/                      # Admin API handlers
  api/openai/                 # OpenAI-compatible endpoints
  auth/                       # Virtual key auth middleware
  billing/                    # Price table and cost calculation
  cache/                      # Exact LRU cache + Redis cache + TieredCache
  cache/semantic/             # Semantic cache (embeddings + pgvector)
  circuit/                    # Provider circuit breakers (local + Redis)
  config/                     # Config loading and validation
  guardrails/                 # Content safety pipeline and stages
  mcp/                        # MCP client, server mode, registry, executor, tool merge, A2A
  health/                     # /health and /ready handlers
  kms/                        # Key management (static KMS, AWS KMS, Vault KMS, AES-256-GCM)
  lifecycle/                  # In-flight request tracking
  logging/                    # Structured slog setup
  metrics/                    # Prometheus metrics collector
  oidc/                       # OpenID Connect SSO (JWT validation, user lookup)
  migrate/                    # Database migration runner
  providers/                  # Provider adapters (OpenAI, Anthropic, Gemini, Azure OpenAI)
  redis/                      # Redis client with health check and graceful degradation
  ratelimit/                  # Token bucket + Redis sliding window rate limiter
  requestid/                  # Request ID context
  routing/                    # Model routing, fallbacks, and region-aware routing
  schema/openai/              # OpenAI-compatible request/response types
  server/                     # HTTP server setup and middleware
  store/                      # Postgres data access layer
  tracing/                    # OpenTelemetry tracing
  usage/                      # Async usage log writer
  pkg/version/                # Version info
deploy/
  prometheus/                 # Prometheus config and alert rules
  grafana/                    # Grafana dashboard JSON
  helm/                       # Helm chart for Kubernetes deployment
scripts/
  generate-dashboard.py       # Regenerate Grafana dashboard from template
configs/                      # YAML configuration files
```

---

## Known Limitations

1. **Streaming + output guardrails** — Output guardrails cannot inspect streaming responses. Only input guardrails apply to streaming requests.
2. **Semantic cache adds latency on miss** — Every miss requires an embedding API call (~50-100ms). The embedding cache mitigates repeated queries.
3. **pgvector required for semantic cache** — Not available on all hosted Postgres. Graceful degradation if missing.
4. **Keyword blocklist is basic** — Not a prompt injection detector. Use the webhook stage for real classifiers.
5. **Rate limit metrics are per-instance without Redis** — Enable Redis for shared state across pods.
6. **Streaming skips MCP tool interception** — Tool calls in streaming responses pass through to the client for execution.
7. **MCP HTTP transport only** — No stdio transport support. Use `supergateway` or similar to bridge stdio servers.
8. **Parallel tool execution** — Multiple tool calls within one round are executed concurrently. Stateful MCP servers may have issues.
9. **A2A SSE is single-instance** — For multi-instance, clients should poll `tasks/get`.
10. **A2A push notifications are best-effort** — 3 retries with backoff, then give up. No dead-letter queue.
11. **RBAC role changes take effect on next request** — Roles are looked up from the DB on each request, not cached in JWT.
12. **No Redis Cluster** — Single-node Redis or Redis Sentinel. Cluster support planned for v1.1.
13. **Latency routing strategy requires Prometheus** — Without metrics enabled, falls back to priority ordering.
14. **Single OIDC issuer** — One IdP per gateway instance. Multi-tenant OIDC is not supported.
15. **Streaming skips cache and output guardrails** — Streaming requests run rate limiting, budget checks, and input guardrails pre-stream, plus usage logging and metrics post-stream. However, cache lookup/store and output guardrails are not applied to streaming requests.
16. **A2A has no per-key governance** — A2A requests use gateway-level auth (bearer token). Rate limits, budgets, and per-key model restrictions are not enforced on A2A entry.

---

## Documentation

| Page | Audience | Description |
|---|---|---|
| [Getting Started](docs/getting-started.md) | Everyone | Install, configure, create a key, make your first request, add guardrails |
| [Configuration](docs/configuration.md) | Operators | Full `gateway.yaml` reference: providers, models, routing, cache, Redis |
| [Governance](docs/governance.md) | Platform teams | Virtual keys, projects, budgets, rate limits, guardrails, RBAC, OIDC, audit logs |
| [Agent Protocols](docs/agent-protocols.md) | AI engineers | MCP client (tool governance), MCP server mode, A2A 1.0 |
| [Observability](docs/observability.md) | SREs | Prometheus metrics, OpenTelemetry tracing, Grafana dashboard, structured logging |
| [Deployment](docs/deployment.md) | DevOps | Docker Compose profiles, Helm chart, Kubernetes production checklist |
| [API Reference](docs/api-reference.md) | Developers | OpenAPI spec, error reference, response headers |
| [Security](docs/security.md) | Security teams | KMS (static, AWS KMS, Vault), data residency, bcrypt key hashing |
| [Migration to v1.0](docs/migration-v1.0.md) | Upgraders | Upgrade guide from Phase 5B/6F: config, behavior, and Go API changes |
| [Documentation Index](docs/index.md) | Everyone | Hub page linking to all documentation |

---

## License

OpenLimit is released under the [MIT License](LICENSE).
