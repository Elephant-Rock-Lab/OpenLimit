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
| Total Go source files | 164 |
| Production LOC | ~19,672 |
| Test LOC | ~14,190 |
| Total tests | 379 |
| Test packages | 30 (all passing) |
| Migrations | 8 |
| Dependencies | 12 direct |
| Internal packages | 29 |
| Git tag | v1.1.0 |

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
| S1 | Streaming Governance Closure (streaming guardrail pipeline, output keyword block) | ✅ Complete |
| 19 | Provider Expansion III (Bedrock, Vertex, Groq, Cohere, Mistral adapters) | ✅ Complete |
| 20 | Key Update + Embeddings (key rotation, /v1/embeddings proxy) | ✅ Complete |
| — | BATCH-06: Multi-Instance A2A SSE (Redis Pub/Sub bridge) | ✅ Complete (v1.1.0) |
| — | BATCH-08: Config Hot Reload (file watcher, SIGHUP) | ✅ Complete (v1.1.0) |
| — | BATCH-09: Admin Dashboard SPA (embed.FS, dark theme) | ✅ Complete (v1.1.0) |
| — | BATCH-10: Prompt Management (CRUD, migration 008) | ✅ Complete (v1.1.0) |
| — | BATCH-11: Webhook mTLS (client cert auth) | ✅ Complete (v1.1.0) |
| — | BATCH-12: Provider Health Dashboard (admin endpoints) | ✅ Complete (v1.1.0) |
| — | BATCH-13: Redis Cluster (UniversalClient) | ✅ Complete (v1.1.0) |
| — | BATCH-RP1: Release Prep (git init, v1.1.0 tag) | ✅ Complete (v1.1.0) |
| — | BATCH-INT1: bytesReader bug fix, embeddings tests restored | ✅ Complete (v1.1.0) |

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

### BATCH-06: Multi-Instance SSE for A2A ✅ SHIPPED v1.1.0
Redis Pub/Sub bridge for cross-instance task notifications. Graceful degradation without Redis.

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

### BATCH-08: Dynamic Config Hot Reload ✅ SHIPPED v1.1.0
File polling watcher + SIGHUP support. ReloadableConfig separates hot-reloadable fields from restart-required fields.

---

### BATCH-09: Admin UI — Static Dashboard ✅ SHIPPED v1.1.0
SPA with dark theme served via embed.FS. 5 sections: Overview, Keys, Usage, Providers, Request Log.

---

### BATCH-10: Prompt Management ✅ SHIPPED v1.1.0
CRUD API at /admin/prompts. Migration 008 for prompt_templates table.

---

### BATCH-11: Webhook Guardrail — mTLS Support ✅ SHIPPED v1.1.0
Client certificate authentication for guardrail webhooks. Falls back to non-mTLS when unconfigured.

---

### BATCH-12: Provider Health Dashboard ✅ SHIPPED v1.1.0
Admin endpoints for provider/model health. Circuit breaker state, failure counts, timestamps.

---

### BATCH-13: Redis Cluster Support ✅ SHIPPED v1.1.0
UniversalClient interface. Cluster mode via `redis.cluster: true`. All callers unchanged.

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

### v1.0 Release (P0 + P1 batches) — ✅ COMPLETE

| Batch | Description | Status |
|---|---|---|
| BATCH-01 | Stale Documentation Cleanup | ✅ Complete |
| BATCH-02 | Admin API OpenAPI Spec | ✅ Complete |
| BATCH-03 | Google Gemini Adapter | ✅ Complete |
| BATCH-04 | Azure OpenAI Adapter | ✅ Complete |
| BATCH-05 | Virtual Key Scoping | ✅ Complete |
| BATCH-06 | Multi-Instance A2A SSE | ✅ Complete |

### v1.1 Release (P2 batches) — ✅ COMPLETE (tagged v1.1.0)

| Batch | Description | Status |
|---|---|---|
| BATCH-08 | Config Hot Reload | ✅ Complete |
| BATCH-09 | Admin UI Dashboard | ✅ Complete |
| BATCH-10 | Prompt Management | ✅ Complete |
| BATCH-11 | Webhook mTLS | ✅ Complete |
| BATCH-12 | Provider Health Dashboard | ✅ Complete |
| BATCH-13 | Redis Cluster | ✅ Complete |

### v1.2 Release (P3 batches) — 🔄 In Progress

| Batch | Description | Sprint |
|---|---|---|
| BATCH-07 | A2A Multi-Turn | Sprint 8 | ✅ Done
| BATCH-14 | TypeScript SDK | Sprint 8 |
| BATCH-15 | Python SDK | Sprint 9 |
| BATCH-16 | Plugin Interface | Sprint 9 |
| BATCH-17 | A2A File/Data Parts | Sprint 10 |
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
| v1.0 | 6 | ~2,300 | ~50 | 3 | ✅ |
| v1.1 | 7 (+RP1, INT1) | ~4,200 | ~90 | 5 | ✅ |
| v1.2 | 6 | ~3,300 | ~55 | 3 | 🔲 |
| **Total** | **19** | **~9,800** | **~195** | **11** | |

---

## 6. TECHNICAL DEBT & KNOWN ISSUES

| ID | Description | Priority | Batch |
|---|---|---|---|
| TD-01 | ~~Known Limitations section stale~~ | ~~P0~~ | ✅ Resolved |
| TD-02 | ~~No OpenAPI spec for admin API~~ | ~~P1~~ | ✅ Resolved |
| TD-03 | ~~Only 2 provider adapters~~ | ~~P1~~ | ✅ Resolved (10 adapters) |
| TD-04 | ~~Virtual keys have no scoping~~ | ~~P1~~ | ✅ Resolved |
| TD-05 | ~~A2A SSE is single-instance only~~ | ~~P1~~ | ✅ Resolved |
| TD-06 | ~~Config changes require restart~~ | ~~P2~~ | ✅ Resolved |
| TD-07 | ~~No admin UI~~ | ~~P2~~ | ✅ Resolved |
| TD-08 | ~~Redis Cluster not supported~~ | ~~P2~~ | ✅ Resolved |
| TD-09 | ~~Embeddings tests hang~~ | ~~P2~~ | ✅ Resolved (BATCH-INT1) |

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
  providers/                  # Provider adapters (OpenAI, Anthropic, Gemini, Azure, Bedrock, Vertex, Groq, Cohere, Mistral)
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
| v1.0.0 | BATCH-01 through BATCH-06 | ✅ Complete | 2026-05-02 |
| v1.1.0 | BATCH-08 through BATCH-13 + RP1 + INT1 | ✅ Complete | 2026-05-03 |
| v1.2.0 | BATCH-07, BATCH-14 through BATCH-18 | 🔲 Planned | TBD |

---

*OpenLimit Master Roadmap v1.0 — AIV Framework compliant*
*This document is the authoritative source for batch planning and release scheduling.*
