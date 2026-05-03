# OpenLimit — Master Project Roadmap
### AIV Framework — Phases, Batches & Delivery Plan

**Document ID:** OPENLIMIT-ROADMAP-001
**Version:** 1.0
**Lead Programmer:** Lead (via pi session)
**Date Issued:** 2026-04-30
**Classification:** Master Planning Document

---

## 1. PROJECT OVERVIEW

OpenLimit is an AI gateway written in Go, inspired by Bifrost, Agentgateway, LiteLLM, and Portkey. It provides a unified, multi-tenant API for routing chat completions across LLM providers with governance, observability, guardrails, caching, MCP client/server, and A2A protocol support.

### Current State

| Metric | Value |
|---|---|
| Total Go source files | 125 |
| Production LOC | ~14,560 |
| Test LOC | ~8,090 |
| Total tests | 272 |
| Test packages | 18 (all passing) |
| Migrations | 7 |
| Dependencies | 12 direct |
| Internal packages | 27 |

### Phases Completed

| Phase | Description | Status |
|---|---|---|
| 0 | Project Scaffolding | ✅ Complete |
| 1 | Core Proxy (OpenAI/Anthropic adapters, routing, streaming) | ✅ Complete |
| 2 | Governance (virtual keys, projects, budgets, usage logging, rate limiting) | ✅ Complete |
| 3 | Observability (OpenTelemetry tracing, Prometheus metrics, structured logging) | ✅ Complete |
| 4 | Guardrails (input/output pipeline, keyword blocklist, webhook, JSON schema, streaming support) | ✅ Complete |
| 5A | MCP Client (JSON-RPC, SSE transport, tool discovery, tool merge) | ✅ Complete |
| 5B | MCP Server + A2A v1.0 (server mode, tool execution proxy, agent card, blocking A2A) | ✅ Complete |
| 6A | Redis-Backed State (rate limiter, cache, circuit breaker) | ✅ Complete |
| 6B | Audit Logs & KMS (Postgres audit, AES-256-GCM, AWS KMS) | ✅ Complete |
| 6C | RBAC, OIDC SSO & Vault KMS (3 roles, JWT validation, user provisioning) | ✅ Complete |
| 6D | Kubernetes Helm Chart (deployment, HPA, ServiceMonitor) | ✅ Complete |
| 6E | Region-Aware Routing (priority/latency strategy, data residency, per-region breakers) | ✅ Complete |
| 6F | A2A Async (non-blocking tasks, SSE streaming, push notifications, Postgres persistence) | ✅ Complete |

### Remaining Work

The following batches represent all known remaining work organized into logical delivery sprints. Each batch will be governed by a formal AIV Blueprint when its sprint begins.

---

## 2. BATCH MAP

### Priority Legend
- **P0** — Production blocker. Must ship before v1.0.
- **P1** — Important. Should ship in v1.0 or v1.1.
- **P2** — Nice-to-have. Post-v1.0.
- **P3** — Future consideration.

---

### BATCH-01: Stale Documentation Cleanup
**Priority:** P0
**Estimate:** ~150 LOC
**Dependencies:** None (standalone)

The Known Limitations section (README.md) contains items 9, 11, 12 that are now resolved by Phase 6F. The A2A Setup section still describes blocking-only mode. Several config fields (`blocking_mode`, `max_workers`) are undocumented in the limitations. The persistent store tests skip without a running Postgres.

| Item | Description |
|---|---|
| MUST | Update Known Limitations to remove items 9, 11, 12 (A2A blocking, in-memory tasks, no streaming/push) |
| MUST | Update A2A limitations to reflect current state (SSE single-instance, push best-effort, in-memory fallback) |
| MUST | Verify all config fields in `gateway.example.yaml` have README documentation |
| MUST | Run full test suite and confirm 18/18 packages pass |
| MUST NOT | Change any code logic |

---

### BATCH-02: Admin API — OpenAPI 3.0 Spec
**Priority:** P1
**Estimate:** ~600 LOC (spec only)
**Dependencies:** None

The admin API has 15+ endpoints with no formal API spec. Generate an OpenAPI 3.0 specification document covering all admin routes, request/response schemas, and authentication requirements.

| Item | Description |
|---|---|
| MUST | Produce `docs/admin-api.yaml` — OpenAPI 3.0 spec covering all `/admin/*` endpoints |
| MUST | Include all CRUD endpoints for projects, virtual keys, users, tools, MCP tools |
| MUST | Include usage, audit, and health endpoints |
| MUST | Document auth requirements (bearer token, RBAC roles per endpoint) |
| MUST NOT | Change any server code |
| MUST NOT | Generate server stubs or clients |

---

### BATCH-03: Provider Adapter — Google Gemini
**Priority:** P1
**Estimate:** ~400 LOC
**Dependencies:** None

Add a third provider adapter for Google Gemini (Generative Language API). Follow the same pattern as `providers/openai/` and `providers/anthropic/`.

| Item | Description |
|---|---|
| MUST | `internal/providers/gemini/adapter.go` — `CompleteChat` and `StreamChat` methods |
| MUST | Translate OpenAI-format requests to Gemini API format |
| MUST | Translate Gemini responses back to `ChatResult` |
| MUST | Handle Gemini-specific errors (safety blocks, quota) |
| MUST | Integration with routing (config-based, fallback support) |
| MUST NOT | Add Gemini-specific config that breaks existing provider config format |
| MUST NOT | Add new dependencies beyond `net/http` |

---

### BATCH-04: Provider Adapter — Azure OpenAI
**Priority:** P1
**Estimate:** ~300 LOC
**Dependencies:** None

Add Azure OpenAI as a provider. Azure OpenAI uses the same API shape as OpenAI but different base URLs and authentication (API key per deployment).

| Item | Description |
|---|---|
| MUST | `internal/providers/azure/adapter.go` — reuse OpenAI adapter with Azure URL patterns |
| MUST | Support deployment-based endpoints (`/openai/deployments/{deployment}/chat/completions`) |
| MUST | Support Azure Active Directory token auth (in addition to API key) |
| MUST NOT | Duplicate the entire OpenAI adapter — share or embed |

---

### BATCH-05: Virtual Key API Key Scoping
**Priority:** P1
**Estimate:** ~500 LOC
**Dependencies:** None

Virtual keys currently grant access to all models and providers. Add optional model/provider scoping to virtual keys so that each key can be restricted to a subset.

| Item | Description |
|---|---|
| MUST | Add `allowed_models` and `allowed_providers` columns to `virtual_keys` table (migration 008) |
| MUST | Validate key scope during auth middleware — reject if requested model/provider not in scope |
| MUST | Admin API endpoints to set/get key scopes |
| MUST | Empty scope = access to all (backward compat) |
| MUST NOT | Break existing virtual keys that have no scope set |
| MUST NOT | Change the virtual key format or hashing |

---

### BATCH-06: Multi-Instance SSE for A2A
**Priority:** P1
**Estimate:** ~350 LOC
**Dependencies:** Redis (existing)

A2A SSE streaming is currently single-instance. Add Redis Pub/Sub to broadcast task status changes across gateway instances so any instance can serve SSE watchers.

| Item | Description |
|---|---|
| MUST | Publish task updates to Redis channel `a2a:task:{id}` when status changes |
| MUST | Subscribe to Redis channel in SSE handler when local notifier has no watchers |
| MUST | Fall back to polling `tasks/get` if Redis unavailable (graceful degradation) |
| MUST NOT | Require Redis — single-instance SSE must still work without it |
| MUST NOT | Change the SSE event format or client-facing API |

---

### BATCH-07: A2A Task History & Multi-Turn
**Priority:** P2
**Estimate:** ~600 LOC
**Dependencies:** BATCH-01

Support multi-turn A2A conversations where `message/send` appends to an existing task's history instead of creating a new task.

| Item | Description |
|---|---|
| MUST | Accept `taskId` param in `message/send` to continue an existing task |
| MUST | Append new message to task history and re-execute |
| MUST | Maintain full conversation history in the task |
| MUST NOT | Break existing single-turn `message/send` behavior |
| MUST NOT | Add stateful session management beyond the task |

---

### BATCH-08: Dynamic Config Hot Reload
**Priority:** P2
**Estimate:** ~400 LOC
**Dependencies:** None

Support reloading config without restarting the gateway. Watch `gateway.yaml` for changes and apply them to routing, rate limits, and guardrails.

| Item | Description |
|---|---|
| MUST | File watcher on `configs/gateway.yaml` (fsnotify or polling) |
| MUST | Hot-reload: model routing, provider config, guardrail rules, rate limits |
| MUST NOT | Hot-reload: database URL, Redis config, server address (require restart) |
| MUST | Validate new config before applying — reject and log if invalid |
| MUST | Emit audit event on config reload |
| MUST NOT | Use any new dependencies beyond stdlib (use polling, not fsnotify) |

---

### BATCH-09: Admin UI — Static Dashboard
**Priority:** P2
**Estimate:** ~1,200 LOC (HTML/JS/CSS)
**Dependencies:** BATCH-02

Build a minimal, single-page admin dashboard served by the gateway at `/admin/ui/`. Uses the existing admin API. No build step — plain HTML + vanilla JS.

| Item | Description |
|---|---|
| MUST | Dashboard with: project list, virtual key list (masked), usage charts, health status |
| MUST | Served as static files from the Go binary (embed.FS) |
| MUST | Authenticate via the same admin bearer token |
| MUST NOT | Require Node.js, npm, or any build tooling |
| MUST NOT | Implement full CRUD — read-only dashboard is sufficient |
| MUST NOT | Add any CSS/JS framework dependencies |

---

### BATCH-10: Prompt Management
**Priority:** P2
**Estimate:** ~700 LOC
**Dependencies:** Postgres (existing)

Versioned prompt templates with variable injection. Store prompts in Postgres, inject via API headers or config.

| Item | Description |
|---|---|
| MUST | `prompts` table (migration 009) — name, content, version, variables, created_at |
| MUST | Admin CRUD for prompt templates |
| MUST | Variable injection: `{{variable}}` placeholders replaced at request time |
| MUST | `X-Prompt-Template` header to select a template for chat completions |
| MUST NOT | Implement prompt chaining or agent logic |
| MUST NOT | Add a template engine dependency — simple string replacement only |

---

### BATCH-11: Webhook Guardrail — mTLS Support
**Priority:** P2
**Estimate:** ~200 LOC
**Dependencies:** None

The webhook guardrail stage currently supports bearer token auth only. Add mutual TLS support for enterprise deployments where the guardrail service requires client certificates.

| Item | Description |
|---|---|
| MUST | Add `tls_cert_file` and `tls_key_file` fields to webhook guardrail config |
| MUST | Configure HTTP client with TLS client certificate |
| MUST | Fall back to non-mTLS if cert not configured |
| MUST NOT | Change the webhook request/response format |

---

### BATCH-12: Provider Health Dashboard
**Priority:** P2
**Estimate:** ~500 LOC
**Dependencies:** BATCH-02

Add admin endpoints for provider health: circuit breaker status, latency p50 per provider/model, error rates. Expose as JSON API for dashboard consumption.

| Item | Description |
|---|---|
| MUST | `GET /admin/health/providers` — per-provider circuit breaker state, last error, recovery time |
| MUST | `GET /admin/health/models` — per-model p50 latency from Prometheus, error rates |
| MUST | RBAC-protected (viewer minimum) |
| MUST NOT | Require a new dependency — use existing circuit breaker and metrics packages |

---

### BATCH-13: Redis Cluster Support
**Priority:** P2
**Estimate:** ~300 LOC
**Dependencies:** Redis (existing)

Add Redis Cluster support alongside the existing single-node client. Configurable via `redis.mode: cluster`.

| Item | Description |
|---|---|
| MUST | `redis.mode` config field: `single` (default) or `cluster` |
| MUST | `redis.cluster_addrs` for cluster node addresses |
| MUST | Same `Client` interface — callers unchanged |
| MUST NOT | Break single-node Redis configurations |
| MUST NOT | Add Redis Sentinel support (deferred) |

---

### BATCH-14: Gateway SDK — TypeScript Client
**Priority:** P3
**Estimate:** ~800 LOC
**Dependencies:** BATCH-02

TypeScript/JavaScript client library for the OpenAI-compatible API and admin API. Published to npm.

| Item | Description |
|---|---|
| MUST | `openlimit-client` package with chat completions, streaming, and admin API methods |
| MUST | Virtual key auth, request ID propagation |
| MUST | Auto-generated from OpenAPI spec (BATCH-02) |
| MUST NOT | Implement a full provider SDK — gateway API only |

---

### BATCH-15: Gateway SDK — Python Client
**Priority:** P3
**Estimate:** ~600 LOC
**Dependencies:** BATCH-02

Python client library. Published to PyPI. Follows the same pattern as the TypeScript client.

| Item | Description |
|---|---|
| MUST | `openlimit-client` Python package |
| MUST | Compatible with OpenAI Python SDK conventions |
| MUST NOT | Implement model training or fine-tuning APIs |

---

### BATCH-16: Plugin Interface
**Priority:** P3
**Estimate:** ~1,000 LOC
**Dependencies:** None

Extensible plugin interface for custom guardrails, caching backends, and routing policies. Plugins register via Go interface and are loaded at startup.

| Item | Description |
|---|---|
| MUST | `internal/plugin/` package with `GuardrailPlugin`, `CachePlugin`, `RoutingPlugin` interfaces |
| MUST | Plugin registry loaded from config |
| MUST | Each plugin receives a structured context (request, response, config) |
| MUST NOT | Support hot-loading or dynamic plugin registration |
| MUST NOT | Use reflection-based plugin loading — explicit registration only |

---

### BATCH-17: A2A File & Data Parts
**Priority:** P3
**Estimate:** ~400 LOC
**Dependencies:** BATCH-01

Extend A2A to support file and data parts in messages, as defined in the A2A 1.0 spec.

| Item | Description |
|---|---|
| MUST | Support `type: "file"` and `type: "data"` in `A2APart` |
| MUST | File parts: base64-encoded content with MIME type |
| MUST | Data parts: structured JSON with schema |
| MUST NOT | Implement binary streaming or chunked uploads |

---

### BATCH-18: Multi-Tenant OIDC
**Priority:** P3
**Estimate:** ~500 LOC
**Dependencies:** RBAC/OIDC (existing)

Support multiple OIDC issuers for multi-tenant deployments. Each project or virtual key can specify its own IdP.

| Item | Description |
|---|---|
| MUST | Multiple OIDC issuer configs keyed by project or domain |
| MUST | Token validation routes to the correct issuer based on request context |
| MUST NOT | Break single-issuer OIDC configurations |
| MUST NOT | Implement SAML or LDAP — OIDC only |

---

## 3. DELIVERY SCHEDULE

### v1.0 Release (P0 + P1 batches)

| Batch | Description | Sprint |
|---|---|---|
| BATCH-01 | Stale Documentation Cleanup | Sprint 1 |
| BATCH-02 | Admin API OpenAPI Spec | Sprint 1 |
| BATCH-03 | Google Gemini Adapter | Sprint 2 |
| BATCH-04 | Azure OpenAI Adapter | Sprint 2 |
| BATCH-05 | Virtual Key Scoping | Sprint 3 |
| BATCH-06 | Multi-Instance A2A SSE | Sprint 3 |

### v1.1 Release (P2 batches)

| Batch | Description | Sprint |
|---|---|---|
| BATCH-07 | A2A Multi-Turn | Sprint 4 |
| BATCH-08 | Config Hot Reload | Sprint 4 |
| BATCH-09 | Admin UI Dashboard | Sprint 5 |
| BATCH-10 | Prompt Management | Sprint 5 |
| BATCH-11 | Webhook mTLS | Sprint 6 |
| BATCH-12 | Provider Health Dashboard | Sprint 6 |
| BATCH-13 | Redis Cluster | Sprint 7 |

### v1.2 Release (P3 batches)

| Batch | Description | Sprint |
|---|---|---|
| BATCH-14 | TypeScript SDK | Sprint 8 |
| BATCH-15 | Python SDK | Sprint 8 |
| BATCH-16 | Plugin Interface | Sprint 9 |
| BATCH-17 | A2A File/Data Parts | Sprint 9 |
| BATCH-18 | Multi-Tenant OIDC | Sprint 10 |

---

## 4. AIV PROCESS MAP

Each batch follows the AIV Framework v3.0 cycle:

```
BATCH-XX
  │
  ├─ Phase I:   Lead writes BLUEPRINT → docs/aiv/BATCH-XX/blueprint.md
  ├─ Phase I-B: Reviewer evaluates blueprint → docs/aiv/BATCH-XX/review-report.md
  │              Lead responds: ACCEPT / ACCEPT WITH MODIFICATIONS / REJECT
  ├─ Phase II:  Assistant implements → docs/aiv/BATCH-XX/implementation-report.md
  │              Code + docs + test evidence
  └─ Phase III: Lead verifies → docs/aiv/BATCH-XX/sign-off-certificate.md
                APPROVED → batch closed, work merged
                RETURNED → corrections required, resubmit
```

### Document Archive

All AIV documents are stored at `docs/aiv/BATCH-XX/`:
1. `blueprint.md` — The technical contract
2. `review-report.md` — Reviewer flags
3. `blueprint-accepted.md` — Blueprint with Lead Response section completed
4. `implementation-report.md` — Assistant's proof of compliance
5. `sign-off-certificate.md` — Lead's final verdict

---

## 5. ESTIMATED EFFORT

| Release | Batches | New LOC (est.) | Tests (est.) | Sprints |
|---|---|---|---|---|
| v1.0 | 6 | ~2,300 | ~50 | 3 |
| v1.1 | 7 | ~3,950 | ~70 | 4 |
| v1.2 | 5 | ~3,300 | ~55 | 3 |
| **Total** | **18** | **~9,550** | **~175** | **10** |

---

## 6. TECHNICAL DEBT & KNOWN ISSUES

| ID | Description | Priority | Batch |
|---|---|---|---|
| TD-01 | Known Limitations section stale (items 9, 11, 12 resolved) | P0 | BATCH-01 |
| TD-02 | No OpenAPI spec for admin API | P1 | BATCH-02 |
| TD-03 | Only 2 provider adapters (OpenAI, Anthropic) | P1 | BATCH-03, 04 |
| TD-04 | Virtual keys have no model/provider scoping | P1 | BATCH-05 |
| TD-05 | A2A SSE is single-instance only | P1 | BATCH-06 |
| TD-06 | Config changes require restart | P2 | BATCH-08 |
| TD-07 | No admin UI | P2 | BATCH-09 |
| TD-08 | Redis Cluster not supported | P2 | BATCH-13 |

---

## 7. ARCHITECTURE (CURRENT)

```
cmd/gateway/main.go          # Entrypoint + graceful shutdown
internal/
  admin/                      # Admin API handlers (CRUD, RBAC, audit, usage)
  api/openai/                 # OpenAI-compatible /v1/chat/completions
  auth/                       # Virtual key auth middleware + key cache
  billing/                    # Price table and cost calculation
  cache/                      # Exact LRU cache + Redis cache + TieredCache
  cache/semantic/             # Semantic cache (embeddings + pgvector + circuit breaker)
  circuit/                    # Provider circuit breakers (local + Redis)
  config/                     # YAML config loading and validation
  guardrails/                 # Content safety pipeline (keyword, webhook, schema)
  health/                     # /health, /ready handlers (OIDC status)
  kms/                        # Key management (static, AWS KMS, Vault, AES-256-GCM)
  lifecycle/                  # In-flight request tracking
  logging/                    # Structured slog setup
  mcp/                        # MCP client, server, registry, executor, tool merge, A2A
  metrics/                    # Prometheus metrics collector
  migrate/                    # Database migration runner (7 migrations)
  oidc/                       # OpenID Connect SSO (JWT validation, auto-provision)
  providers/                  # Provider adapters (OpenAI, Anthropic)
  redis/                      # Redis client with health check
  ratelimit/                  # Token bucket + Redis sliding window limiter
  requestid/                  # Request ID context propagation
  routing/                    # Model routing, fallbacks, region-aware, latency strategy
  schema/openai/              # OpenAI-compatible request/response types
  server/                     # HTTP server setup, middleware wiring, runtime
  store/                      # Postgres data access (projects, keys, usage, RBAC)
  tracing/                    # OpenTelemetry tracing
  usage/                      # Async usage log writer
  pkg/version/                # Version info
deploy/
  helm/openlimit/             # Kubernetes Helm chart
  prometheus/                 # Prometheus config + alerts
  grafana/                    # Grafana dashboard JSON
configs/                      # Example config files
```

---

## 8. VERSION MATRIX

| Version | Phase | Status | Date |
|---|---|---|---|
| v0.1.0 | Phase 0-1 (Core Proxy) | ✅ Complete | 2026-04-29 |
| v0.2.0 | Phase 2 (Governance) | ✅ Complete | 2026-04-29 |
| v0.3.0 | Phase 3 (Observability) | ✅ Complete | 2026-04-29 |
| v0.4.0 | Phase 4 (Guardrails) | ✅ Complete | 2026-04-29 |
| v0.5.0 | Phase 5A (MCP Client) | ✅ Complete | 2026-04-29 |
| v0.5.1 | Phase 5B (MCP Server + A2A) | ✅ Complete | 2026-04-29 |
| v0.6.0 | Phase 6A-6F (Enterprise) | ✅ Complete | 2026-04-30 |
| v1.0.0 | BATCH-01 through BATCH-06 | 🔲 Planned | TBD |
| v1.1.0 | BATCH-07 through BATCH-13 | 🔲 Planned | TBD |
| v1.2.0 | BATCH-14 through BATCH-18 | 🔲 Planned | TBD |

---

*OpenLimit Master Roadmap v1.0 — AIV Framework compliant*
*This document is the authoritative source for batch planning and release scheduling.*
