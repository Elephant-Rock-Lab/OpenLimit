# OpenLimit Documentation

Welcome to the OpenLimit documentation. This index links to every doc page with the target audience and what you'll find there.

> **Start here:** If you're new to OpenLimit, begin with [Getting Started](getting-started.md) — it takes you from zero to your first governed AI request in under 5 minutes.

---

## Pages

### [Getting Started](getting-started.md)

**Audience:** Everyone — developers, operators, platform teams

**What's inside:**
- Installation (binary build or Docker)
- Configuration basics (provider, database, models)
- Starting the gateway (local or Docker Compose)
- Health check verification
- Creating your first virtual key (quickstart endpoint)
- Making your first chat completion request
- Adding your first guardrail
- Checking usage

Full walkthrough from installation to your first governed request.

---

### [Configuration](configuration.md)

**Audience:** Operators, DevOps

**What's inside:**
- Server settings (host, port, timeouts)
- Database configuration
- All provider types: OpenAI, Anthropic, Gemini, Azure OpenAI, OpenAI-compatible
- Gemini `gemini_model_map` and Azure `azure_resource` / `azure_api_version` fields
- Provider key encryption (`encrypted_value`)
- Model routing (weighted, fallbacks)
- Region-aware routing (priority and latency strategies)
- Exact cache (LRU) and semantic cache (embeddings + pgvector)
- Redis integration (shared state for rate limits, cache, circuit breakers)
- Telemetry (Prometheus metrics, OpenTelemetry tracing)
- Auth and admin settings
- Billing prices
- Guardrails overview
- MCP, MCP server, and A2A config pointers

Complete reference for every section of `gateway.yaml`.

---

### [Governance](governance.md)

**Audience:** Platform teams, security engineers

**What's inside:**
- Virtual API keys (`gw-` prefix, properties, management)
- Quickstart endpoint for one-step onboarding
- Projects (multi-tenancy, cascade delete)
- Rate limiting (token bucket, Redis sliding window)
- Budgets (daily/monthly spend caps)
- Guardrails (6 stages: PII, regex, keyword, length, webhook, json_schema)
- Per-model guardrail configuration
- Custom webhook guardrails
- Streaming behavior
- RBAC (3 roles: admin, editor, viewer)
- User management
- OIDC SSO (Okta, Azure AD, Keycloak, Google)
- Audit logs (event types, query filters, schema)

Everything about controlling access to AI providers.

---

### [Agent Protocols](agent-protocols.md)

**Audience:** AI engineers, agent developers

**What's inside:**
- MCP client mode: tool discovery, namespacing, tool merge, tool governance
- Virtual key tool permissions (glob patterns)
- Tool merge behavior (conflict resolution)
- Multi-round agent loop (max rounds, cumulative timeout)
- MCP server mode: expose virtual keys as tools
- MCP tool naming and deduplication
- MCP authentication modes (none, bearer_token, virtual_key)
- A2A 1.0: agent card, tasks, SSE streaming, push notifications
- Blocking vs non-blocking task execution
- A2A error codes
- Prometheus metrics for agent protocols

How to integrate OpenLimit with AI agents via MCP and A2A.

---

### [Observability](observability.md)

**Audience:** SREs, platform engineers

**What's inside:**
- Prometheus metrics setup
- Full metrics reference (30+ metrics organized by category)
- OpenTelemetry tracing configuration
- W3C `traceparent` header for correlation
- Supported tracing backends (Jaeger, Tempo, SigNoz, etc.)
- Grafana dashboard generation and import
- Docker Compose monitoring profile
- Prometheus recording and alert rules
- Cardinality awareness
- Structured logging conventions
- Monitoring checklist

How to monitor OpenLimit in production.

---

### [Deployment](deployment.md)

**Audience:** DevOps, platform engineers

**What's inside:**
- Docker Compose profiles (default, monitoring, stateful, full stack)
- Helm chart features (deployment, ConfigMap, Secret, Service, Ingress, HPA, ServiceMonitor)
- Helm security defaults (non-root, read-only filesystem, no privilege escalation)
- Kubernetes production checklist (secrets, HA, database, observability, networking, governance, resources)
- Developer commands (make run, test, lint, build)

Running OpenLimit in development and production.

---

### [API Reference](api-reference.md)

**Audience:** Developers, integrators

**What's inside:**
- Link to OpenAPI 3.0.3 specification
- Chat completions API (OpenAI-compatible)
- Supported parameters
- Response headers (X-Request-ID, X-Provider, X-Cache, X-Cost-USD, traceparent)
- Model listing endpoint
- Health endpoints (liveness, readiness)
- Admin API endpoints (16 endpoints)
- Error reference table (16 error types with HTTP status, type, meaning, example, recommended action)
- Guardrail block stages
- Error metric (gateway_errors_total)

How to use the OpenLimit API and understand its responses.

---

### [Security](security.md)

**Audience:** Security teams, compliance officers

**What's inside:**
- Security model (3 layers: network, governance, encryption)
- KMS encryption: static, AWS KMS, HashiCorp Vault
- Provider key encryption (encrypted_value, ciphertext format)
- Key rotation procedure
- Fail-closed behavior
- Data residency enforcement (X-Data-Residency header, matching rules)
- Virtual key protection (bcrypt hashing, key lookup caching)
- Redis security considerations
- OIDC security considerations
- Security checklist

How OpenLimit protects your keys, data, and access.

---

### [Migration to v1.0](migration-v1.0.md)
### [Migration to v1.1](migration-v1.1.md)

**Audience:** Teams upgrading from Phase 5B/6F

**What's inside:**
- v1.0.0 overview (unified governance, new providers, enriched errors)
- Config changes (Gemini provider, Azure OpenAI provider, validation)
- Behavior changes (governance on MCP/A2A, enriched errors, admin error request_id, new headers, quickstart endpoint, gateway_errors_total)
- Go API changes (writeAdminError signature, executePlanSingle removal, ExecuteForMCP identity, IdentityProvider, GovernanceBlockedError)
- Upgrade checklist (before, during, after)
- Rollback procedure

Step-by-step guide for upgrading to v1.0.0.

---

## Quick Links

| What you need | Where to go |
|---|---|
| Install and run the gateway | [Getting Started](getting-started.md) |
| Configure a new provider | [Configuration](configuration.md) |
| Set up API key governance | [Governance](governance.md) |
| Connect MCP tools | [Agent Protocols](agent-protocols.md) |
| Set up monitoring | [Observability](observability.md) |
| Deploy to Kubernetes | [Deployment](deployment.md) |
| Fix an error | [API Reference](api-reference.md) |
| Encrypt provider keys | [Security](security.md) |
| Upgrade from pre-v1.0 | [Migration to v1.0](migration-v1.0.md) |
| Upgrade from v1.0 to v1.1 | [Migration to v1.1](migration-v1.1.md) |

---

## Contributing

Found a bug or want to improve the docs? See [CONTRIBUTING.md](../CONTRIBUTING.md) for development setup and contribution guidelines.
