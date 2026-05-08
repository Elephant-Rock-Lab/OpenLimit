# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.2.0] - 2026-05-08

### Added
- A2A multi-turn conversations: message/send accepts optional taskId to continue existing task
- Agent responses appended to task history for multi-turn continuity
- Rate limiting and budget checks for embeddings requests
- Tests for tracing (2), usage (2), lifecycle (2) packages
- Migration guide v1.0 → v1.1

### Changed
- Refactor server.go NewRuntime() — extracted 6 builder functions (29% reduction)
- Usage Writer now safely handles nil database (no goroutine crash)

### Removed
- Dead code: unused recordUsage() method on Handler

## [1.1.2] - 2026-05-04

### Changed
- Refactor server.go NewRuntime() — extracted 6 builder functions for clarity

### Added
- Tests for tracing, usage, and lifecycle packages
- Rate limiting and budget checks for embeddings requests
- Migration guide v1.0 → v1.1 (docs/migration-v1.1.md)

### Removed
- Dead code: unused recordUsage() method on Handler

## [1.1.1] - 2026-05-04

### Fixed
- Wire missing provider adapters (bedrock, vertex, groq, cohere, mistral) in
  server.go — these were defined but never instantiated, causing runtime failures
- Add bedrock, vertex, groq, cohere, mistral to config validation supported types
- Fix A2A Redis bridge for cluster mode: changed `*goredis.Client` to
  `UniversalClient` so the bridge works with both standalone and cluster Redis
- Add `region`, `project`, `publisher` fields to ProviderConfig for vertex/bedrock

## [1.1.0] - 2026-05-03

### Fixed
- bytesReader test helper had value receiver on Read(), causing infinite loop
  in io.ReadAll — changed to pointer receiver
- All 5 embeddings handler tests now pass in default test run
  (previously timed out due to the bytesReader bug)

### Added
- Multi-instance A2A SSE streaming via Redis Pub/Sub (RedisTaskBridge)
- TaskBridgePublisher interface for cross-instance task notification
- Loop prevention: each gateway instance filters self-originated messages
- Graceful degradation: single-instance behavior when Redis is unavailable
- Exponential backoff reconnection for Redis subscription
- `Notifier()` accessor on A2AHandler for bridge wiring
- `NewInstanceID()` utility for unique gateway identification
- Config hot-reload: file watcher detects gateway.yaml changes at runtime
- SIGHUP signal support for manual config reload trigger
- Debounce protection for rapid file changes
- ReloadableConfig / MergeReloadable: only safe fields applied at runtime
- Non-reloadable fields (server port, DB URL, Redis, KMS) require restart
- Admin Dashboard SPA: read-only UI served from Go binary via embed.FS
- Dashboard sections: Overview, Keys, Usage Analytics, Providers, Request Log
- Dark theme UI with vanilla HTML/CSS/JS (no external dependencies)
- Bearer token auth for dashboard (same as admin API)
- Admin health endpoints: GET /admin/health/providers, GET /admin/health/models
- Tracker.GetAll() method exposes all recorded health entries
- Provider circuit breaker state, failure counts, timestamps exposed via API
- Prompt template management: CRUD API at /admin/prompts
- Database migration for prompt_templates table
- Prompt store layer with create/list/get/update/delete operations
- Webhook mTLS support: client certificate authentication for guardrail webhooks
- NewWebhookStageWithTLS constructor with cert/key/CA file loading
- Redis Cluster support: cluster flag in config creates ClusterClient
- UniversalClient interface for Redis standalone/cluster abstraction

## [1.0.0] - 2026-05-02

### Added
- Unified governance pipeline (ExecuteGoverned) for all entry points: direct API, MCP server, A2A
- Google Gemini provider adapter with streaming support
- Azure OpenAI provider adapter with deployment-based URL construction
- Per-model passive health tracking with 30-second window
- OpenAPI 3.0.3 specification for the admin API (15 endpoints)
- Operational response headers: X-Provider, X-Cache, X-Cost-USD
- Unified error format across all HTTP APIs with structured details and stage fields
- Admin API errors now include request_id
- Actionable provider error messages (EnrichProviderError)
- SSE error events for streaming provider failures
- POST /admin/quickstart endpoint for single-step onboarding
- gateway_errors_total Prometheus metric
- IdentityProvider interface for cross-package identity passing
- GovernanceBlockedError interface for A2A error detection
- GitHub Actions CI pipeline

### Changed
- MCP server and A2A paths now enforce full governance (rate limits, budgets, guardrails, caching, usage logging)
- Admin error signature: writeAdminError now requires *http.Request parameter
- Provider errors now include actionable messages instead of raw pass-through
- North star: "The open-source control plane for AI operations"

### Removed
- executePlanSingle() — the ungoverned shortcut; all calls go through ExecuteGoverned()

### Fixed
- Dual-path governance gap: agent paths can no longer bypass any governance control
