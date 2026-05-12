# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.4.1] - 2026-05-13

### Fixed — Critical Security (BATCH-57)
- **JSON injection in guardrail redaction**: Replace string concat into `json.RawMessage` with `json.Marshal()`
- **Double mutex unlock in MCP tryReconnect**: Remove defer, keep explicit unlock per code path
- **Expired key auth bypass**: Add `expires_at` filter to SQL queries in `LookupVirtualKeyByToken` and `ListVirtualKeysForMCP`
- **readJSON body-close race**: Always return 400 on JSON decode error, no fallback body read
- **Nil LatencyCache for smart routing**: Create LatencyCache when strategy is "smart" or "latency"

### Fixed — Concurrency (BATCH-58)
- **OnKeysChanged panic guard**: Wrap all 5 goroutine launches with `defer recover()` + error logging
- **Atomic Redis breaker**: HINCRBY replaces read-modify-write for failure counter
- **MCP goroutine leak**: `cancelNotif` field cancels old listener before new one on reconnect
- **KeyCache LRU eviction**: `lastAccess` time tracking replaces random map eviction

### Fixed — Resource Management (BATCH-59)
- **Admin body limits**: 64KB body size limit for `/admin/*` routes
- **Context-aware backoff**: `select` on `time.After` + `ctx.Done` replaces `time.Sleep`
- **Default per-call timeout**: 30s when `TimeoutMS` is 0
- **Usage drop metric**: `RecordUsageDrop()` + `SetDropRecorder()` for buffer-full observability
- **Replay ring buffer**: O(1) ring buffer replaces O(n) slice shift

### Fixed — Streaming + Routing (BATCH-60)
- **Streaming output guardrails**: `CheckOutput` on accumulated content before `[DONE]`; Block → SSE error
- **Smart routing integration**: Verified different scores per target, cost-only fallback, health deprioritization
- **Breaker map LRU eviction**: `breakerEntry` wrapper with `lastAccess` for deterministic eviction

### Fixed — Data Integrity (BATCH-61)
- **SQL pagination**: `LIMIT`/`OFFSET` in SQL + `CountVirtualKeys` for `X-Total-Count`
- **Budget context propagation**: `CheckBudget` accepts `context.Context`, no more `nil`
- **Array parsing**: Quoted comma handling in `parseArrayString`; escaping in `arrayString`
- **Quickstart duplicate guard**: Reuse existing project on duplicate creation

### Changed — UX (BATCH-62, BATCH-63)
- **Inline login form**: Replaces `prompt()` auth wall with styled password input
- **5-tab dashboard**: 8→5 tabs (Overview, Keys, Usage, Guardrails, MCP)
- **Guardrail catalog prefill**: Click-to-test workflow
- **ARIA landmarks + focus management**: Full accessibility compliance
- **Init wizard docs**: "Quick Setup" section with `openlimit init`
- **Mobile responsive**: CSS breakpoints at 768px and 1024px
- **Colorblind spend bars**: Stripe patterns + safe/warning/danger classes

### Tests
- **+74 new tests** across 7 batches (BATCH-57→63)
- **733 passing**, 8 pre-existing OBL-05 failures deferred
- Zero regressions



### Added
- **Provider Registry** (BATCH-42): 21 new providers registered (30+ total), zero new adapter code
  - ProviderRegistry with case-insensitive lookup and ApplyDefaults
  - Config-only provider entries: DeepSeek, Together AI, xAI Grok, Fireworks AI, Perplexity, Cerebras, SambaNova, Novita AI, AI21, Ollama, LM Studio, vLLM, LocalAI, Databricks, Snowflake, Watsonx, Replicate, Hugging Face, DeepInfra, Lepton AI
  - Provider validation on startup (soft — logs warnings, doesn't prevent boot)
  - Auto type resolution from registry when config omits `type` field

- **Latency Benchmark Tool** (BATCH-43): `openlimit-bench` command
  - Self-contained in-process benchmark (no external deps, no running gateway needed)
  - Measures P50/P95/P99 latency and requests/second
  - Supports -n (request count) and -c (concurrency) flags
  - Results: ~17K req/sec, P50 ~1ms, P95 ~10ms at concurrency 50

- **Provider Registry Hardening** (BATCH-44): 12 E2E integration tests
  - Full round-trip tests for 5 registry providers (DeepSeek, Together AI, xAI Grok, Fireworks AI, Perplexity)
  - Validates: config resolution, adapter creation, request routing, auth forwarding, error forwarding
  - Validates: streaming SSE, case-insensitive names, user base_url override
  - All registry entries verified: Bearer auth format, ApplyDefaults resolution

- **Dashboard Onboarding Overlay** (BATCH-45): 60-second first-request UX
  - New `GET /admin/providers/registry` endpoint lists all 30+ registry providers
  - Redesigned 3-step overlay: Choose Provider → Get Your Key → Try It Now
  - Provider dropdown fetched from registry API with availability status
  - One-click quickstart (creates project + virtual key via existing API)
  - Auto-generated curl command with the new virtual key

- **Smoke Test Tool** (BATCH-46): `openlimit-test` command
  - Self-contained in-process smoke test (no external deps)
  - Starts mock provider + gateway, sends request, validates response
  - Reports ✓ PASS/✗ FAIL with latency and response details
  - Exit 0 on success, 1 on failure

- **Config Doctor** (BATCH-47): `openlimit-doctor` command
  - Diagnoses common misconfigurations without starting the gateway
  - Checks: provider resolution, model route validity, API key env vars, registry lookup
  - Reports ✓ PASS / ⚠ WARN / ✗ FAIL with actionable suggestions
  - Supports -config flag for custom config path

- **Spend Dashboard** (BATCH-48): Budget utilization in admin UI
  - New `GET /admin/usage/spend` endpoint: per-key spend, budget utilization, status
  - Status thresholds: healthy <75%, warning 75-95%, critical >95%, unlimited (no budget)
  - Admin UI Spend tab: total spend card, per-key budget bars with color-coded warnings
  - Period selector (daily/monthly)
  - `computeKeyStatus` pure function tested with 12 threshold cases

- **Native Guardrail Catalog** (BATCH-49): 6 built-in validators, discoverable and testable
  - New `GET /admin/guardrails/catalog` endpoint: lists 6 validators (PII, keyword, length, regex, JSON schema, webhook)
  - New `POST /admin/guardrails/test` endpoint: evaluates sample text against any validator in-process
  - Admin UI Guardrails tab: catalog cards with config docs + test form with instant results
  - Webhook marked "Live Only" (not testable in dashboard — requires live endpoint)
  - 12 tests covering all validator types, invalid type rejection, direction switching

- **Guardrail Dashboard** (BATCH-50): Live guardrail hit counters and provider health in admin UI
  - New `GET /admin/guardrails/stats` endpoint: in-memory counters for blocks, redactions, passes, requests, and rates
  - Per-stage breakdown with direction tracking (input/output)
  - Stats work without Prometheus (in-memory atomic counters)
  - Admin UI Guardrail Stats section: hit rate cards (blocks/redactions/passes/rates), stage table, provider health cards
  - Auto-refresh every 30 seconds
  - Guardrail pipeline records pass events (RecordGuardrailPass)
  - 10 tests covering counters, rates, per-stage data, direction tracking, auth requirement

- **Guardrail Dashboard** (BATCH-50): Live hit rates and per-stage breakdown
  - New `GET /admin/guardrails/stats` endpoint: in-memory counters for blocks, redactions, passes
  - Block rate % computation: `(total_blocks / total_requests) * 100`
  - Per-stage breakdown: individual stage blocks and redactions by direction
  - Admin UI stats cards: Total Blocks, Redactions, Passes, Block Rate %
  - Per-stage table in dashboard with auto-refresh every 30 seconds
  - In-memory counters work without Prometheus (survive metrics disabled)
  - 10 tests covering counters, rates, auth, per-stage data, direction tracking

- **MCP Add Command** (BATCH-51): `openlimit-mcp add` one-command MCP server registration
  - New CLI tool: `cmd/mcp/main.go` with `add` subcommand
  - Connects to MCP server, runs Initialize + ListTools (auto-discovery)
  - Optional `--ping` flag validates connectivity before registration
  - `--dry-run` flag shows discovery without writing config
  - Atomic config writes (temp file + rename) preserve all existing fields
  - Validates server name (alphanumeric+hyphens), URL format, duplicate detection
  - Config `cmd/mcp/config_writer.go` with reusable `appendServerToConfig`
  - 16 tests: validation, mock MCP server, config persistence, sequential adds

- **MCP Tool Discovery** (BATCH-52): `openlimit-mcp search <query>` keyword search over embedded catalog
  - New: `cmd/mcp/catalog.go` — 42 entries across 12 servers, 10 categories
  - New: `cmd/mcp/search.go` — TF-IDF-inspired scoring (name weight 2.0, description weight 1.0)
  - Case-insensitive, multi-word, tokenized search (AR-05)
  - `--category <cat>` filter, `--limit N`, `--format json` flags
  - `--live` flag merges tools from configured MCP servers with embedded catalog
  - Deduplication: catalog wins over live for same tool name
  - 12 tests: scoring, ranking, filtering, JSON output, live merge, dedup

- **MCP Dashboard** (BATCH-53): Admin dashboard MCP tab with server inventory and health
  - New endpoint: `GET /admin/mcp/servers` — server-centric view with tool lists and health status
  - New: `internal/admin/mcp_servers.go` — closure-pattern handler (consistent with ToolsHandler)
  - Server cards: name, connected/error/disconnected status, tool count, tool list (capped at 50)
  - Aggregate counts: total servers, connected count
  - Auto-refresh every 30s with timer cleanup on tab switch
  - 10 tests: backend endpoint + frontend HTML verification

- **Smart Routing** (BATCH-54): Cost-aware and combined cost+latency+health routing strategies
  - New strategy: `cost` — routes to cheapest provider based on embedded pricing catalog
  - New strategy: `smart` — weighted score combining cost (40%), latency (40%), health (20%)
  - New: `internal/routing/costs.go` — 22 entries covering DeepSeek, OpenAI, Anthropic, Together, xAI, Fireworks, Perplexity, Google
  - Normalization: cost/latency scores scaled 0.0-1.0, missing data defaults to median
  - Configurable smart weights via `routing.cost_weights` YAML field
  - New endpoint: `GET /admin/routing/costs` — pricing catalog, strategy, and weights
  - 16 tests: catalog size, cost selection, smart scoring, health weighting, normalization, regression guards

- **Request Replay & A/B Testing** (BATCH-55): Shadow traffic for confident provider migration
  - New: `internal/replay/manager.go` — fire-and-forget shadow requests after primary response
  - Async goroutine with configurable context timeout (default 30s)
  - Sample rate per route (0.0–1.0) controls what fraction of requests are replayed
  - Ring buffer stores last 1000 results with insertion-order eviction
  - `ReplayManager.Close()` for graceful shutdown, waits for in-flight goroutines
  - Replay hook in chat handler via optional interface field (no executePlan modification)
  - New: `internal/admin/replay.go` — `GET /admin/routing/replay` with results + summary stats
  - Summary computes avg primary/shadow latency, shadow error rate
  - 14 tests: async behavior, sample rate, ring buffer, timeout, error isolation, summary stats

### Fixed
- **OBL-03/OBL-04 Closure** (BATCH-41):
  - Rate limiter `Close()` wired into server shutdown — no more goroutine leak on gateway stop (OBL-03)
  - `TestExecuteGoverned_Success` flakiness fixed — explicit `context.WithTimeout(10s)` prevents deadline exceeded in full-suite runs (OBL-04)
  - `Runtime.CloseHandlers()` method added for clean shutdown sequencing

### Fixed
- **STRESS-02/03 P3 Fixes** (11 new tests, BATCH-40):
  - Stream-incomplete metric counter tracks interrupted SSE streams (STRESS-02 S2-02)
  - Minimum token length enforcement: reject tokens < 11 chars in extractBearerToken (STRESS-02 S5-03)
  - Circuit breaker integration in embeddings handler (STRESS-03 S4-07)
  - Session store `lastSeen` uses `atomic.Int64` — no more RLock write race (STRESS-03 S3-06)
  - `parseArrayString` filters empty strings — `{a,,b}` → `["a","b"]` (STRESS-02 S5-05)
  - OIDC provider discovery uses 30-second timeout — no more startup hang (STRESS-03 S5-11)
  - Health tracker evicts stale entries with 0 failures older than 1 hour (STRESS-02 S6-01)
  - `statusRecorder` implements `http.Hijacker` — WebSocket/SSE hijack works (STRESS-02 S6-02)

### Fixed
- **STRESS-02/03 P2 Fixes** (22 new tests, BATCH-39):
  - SSRF protection on push notification URLs: rejects private IPs (10.x, 172.16.x, 192.168.x), loopback, link-local (169.254.x), IPv6 equivalents, and non-HTTP(S) schemes (STRESS-03 S4-09)
  - MCP executor context leak fixed: extracted `invokeWithDeadline` helper, cancel() called per-round instead of deferred (STRESS-03 S2-03)
  - MCP resolver O(N) scan eliminated: new `store.ListVirtualKeysForMCP()` with SQL WHERE clause filters `allow_mcp_server=true AND revoked_at IS NULL` (STRESS-03 S2-04)
  - Consolidated budget fail-closed enforcement: new `usage.CheckBudget()` helper used across all 3 call sites (auth middleware, governed pipeline, embeddings). Configurable via `billing.fail_closed` (STRESS-02 S4-02, STRESS-03 S4-06)
  - Config watcher deep copy: `Config.DeepCopy()` prevents callback mutations from corrupting watcher state (STRESS-02 S3-03)
  - MCP manager per-state locking: `serverState.mu` serializes reconnect/ping mutations, preventing duplicate client initialization (STRESS-02 S3-04)

### Fixed
- **STRESS-02 P1/P2 Fixes** (9 new regression tests, 509 total):
  - `audit.Logger.Close()` now blocks until all buffered events are drained (S2-01: audit trail data loss on shutdown)
  - Rate limiter buckets map now evicts stale entries (10-minute idle threshold, 10K cap) (S3-01: unbounded growth)
  - Circuit breaker map now capped at 1000 entries with eviction (S3-02: unbounded growth)
  - Negative `budget_limit_usd` rejected in both admin API and store layer (S5-01: negative budget bypass)
  - SQL field name validation with `^[a-z_][a-z0-9_]*$` regex in `UpdateVirtualKey` (S5-06: injection hardening)

### Fixed
- **STRESS-01 P0 Critical Fixes** (9 new regression tests, 500 total):
  - `usage.Writer.Close()` now blocks until all buffered entries are drained (S1-01: billing data loss on shutdown)
  - `KeyRing.Next()` uses `uint64` counter with safe modulo — no panic at ~2B requests (S3-01: integer overflow)
  - `Router` uses `rand.Global` (Go 1.22+) instead of per-instance `rand.Rand` — safe for concurrent use (S3-06: data race)
  - Guardrail output check errors now fail-closed: returns 500 GovernanceError instead of leaking unchecked response (S4-02: fail-open)
  - MCP `handleToolsCall` uses `context.WithTimeout(5 min)` instead of bare `context.Background()` (S4-09: no timeout)
  - `config.Validate` rejects `admin.enabled` with no auth method (S6-03: zero-auth bypass)

### Added
- Dashboard: URL-based tab routing (#overview, #keys, #usage, #providers, #logs) with hashchange support
- Dashboard: ARIA roles (tablist/tab/tabpanel), aria-selected, aria-label on all interactive elements
- Dashboard: 44x44px minimum touch targets on all buttons, tabs, and filter inputs
- Dashboard: Budget display shows period in key detail panel (e.g., "$10.00/monthly" instead of "$10.00/undefined")
- Backend: `POST /admin/keys/{id}/rotate` endpoint for key rotation (returns new raw key)
- Backend: `RotateVirtualKey` store function for key rotation with revoked-key guard
- Backend: DB fallback for auth — serves cached keys with 5-minute grace period when database is unavailable
- Backend: `GetWithGrace` method on KeyCache for grace-period lookups
- Backend: `EventKeyRotate` audit event type
- Backend: Config validation errors formatted as numbered list instead of semicolon-separated string
- Docs: `docs/troubleshooting.md` covering 18 common errors with cause and solution

## [1.3.1] - 2026-05-09

### Added
- Dashboard: Auth cancel guard (fixes infinite 401 loop on prompt Cancel)
- Dashboard: Network error handling with "Connection Error" page
- Dashboard: "Copy to Clipboard" button on key creation (clipboard API + execCommand fallback)
- Dashboard: "Revoke" button per key row with confirmation dialog
- Dashboard: Loading spinners on all 5 panels (CSS animation)
- Dashboard: Actionable empty states with CTAs ("Create your first project")
- Dashboard: Toast notification system (3s auto-dismiss, stacked)
- Dashboard: First-run 3-step overlay (project → key → curl command)
- Dashboard: "Edit Key" modal with all 11 fields (basic + advanced)
- Dashboard: Key detail slide-out panel (click row → full key info)
- Dashboard: Delete project with confirmation dialog (shows orphaned key count)
- Dashboard: Pagination on keys and usage tables (limit/offset + X-Total-Count)
- Backend: `available_models` field in model_not_allowed/model_not_found error responses
- Backend: `Router.ModelNames()` accessor (sorted list of configured models)
- Backend: `ErrorBody.AvailableModels` field (omitempty, backward compatible)
- Backend: `GovernanceError.AvailableModels` propagated to HTTP error responses
- Backend: Streaming audit events — `postStreamGovernance` now emits ChatCompletion audit event
- Backend: `GetVirtualKeyByID` store accessor for direct key lookup
- Backend: Prefix-filtered key lookup (`LookupVirtualKeyByToken` WHERE key_prefix = $1)
- Backend: Direct SELECT in updateKey/patchKey (replaces full-table ListVirtualKeys scan)
- Backend: Offset + X-Total-Count on usage endpoint

### Added (v1.3.1-post)
- CLI: `openlimit init` interactive setup wizard (provider → key → config → curl)
- CLI: `--non-interactive` flag for CI/CD (reads from env vars)
- CLI: `--force` flag to overwrite existing config
- CLI: `--output` flag to specify config path (default: configs/gateway.yaml)
- CLI: Provider templates for openai, anthropic, gemini, openai-compatible
- CLI: Auto-generated admin token (crypto/rand 32-byte hex)
- CLI: Key masking in all output (first 4 + last 4 chars only)
- CLI: Config validation before write (config.Validate() enforced)

### Security (v1.3.1-sec1)
- **Critical**: All bearer token comparisons now use `crypto/subtle.ConstantTimeCompare` (was: raw `==`)
- **Critical**: Request body size limited to 10MB via `http.MaxBytesReader` (configurable: `server.max_body_size_kb`)
- **Critical**: All provider response reads capped at 50MB via `io.LimitReader` (was: unbounded `io.ReadAll`)
- **Critical**: Dashboard usage chart XSS fixed — provider names now escaped via `esc()`

### Changed
- **Governance pipeline refactored**: Steps 1-4 extracted into reusable functions (`buildModelNotAllowedError`, `checkRateLimit`, `checkBudget`, `checkInputGuardrails`) — shared by both streaming and non-streaming paths
- **TypeScript SDK**: Non-AbortError fetch exceptions now throw `NetworkError` (new class) instead of raw `TypeError`
- **Dashboard**: Key filter input debounced at 300ms (was: instant API call per keystroke)

### Added (v1.3.1-post2)
- **Dashboard**: URL hash-based tab routing (#overview, #keys, #usage, #providers, #logs)
- **Dashboard**: ARIA accessibility (role, aria-selected, aria-label, aria-live regions)
- **Dashboard**: Touch target sizing (44x44px minimum on all interactive elements)
- **Docs**: Troubleshooting guide (docs/troubleshooting.md) covering 15+ common errors
- **Auth**: DB fallback with grace period — serves cached keys when database unavailable
- **Auth**: KeyCache.GetWithGrace() method for TTL-extended cache lookups
- **Admin API**: `POST /admin/keys/{id}/rotate` — key rotation endpoint (generates new key)
- **Store**: RotateVirtualKey function for atomic key credential rotation
- **Config**: Validation errors formatted as numbered bullet list (was: semicolon-separated)

### Changed
- Dashboard budget display: shows "Unlimited" instead of "$0.00" in key detail panel

### Changed
- Dashboard: Provider panel switched from /ready to /admin/health/providers
- Dashboard test updated to check /admin/health/providers instead of /ready
- updateKey/patchKey use GetVirtualKeyByID instead of ListVirtualKeys("") + linear scan

### Fixed
- Auth prompt Cancel no longer causes infinite 401 loop
- Network failures no longer produce blank dashboard
- Key creation no longer loses key on modal close (copy button safeguard)
- Model error responses now list available/allowed models for discoverability
- Streaming requests now produce audit events (was gap since initial implementation)
- O(N) bcrypt lookup reduced to O(1-3) via prefix filtering

## [1.2.0] - 2026-05-08

### Added
- A2A multi-turn conversations: message/send accepts optional taskId
- TypeScript SDK (@openlimit/sdk) — zero deps, streaming, types
- Python SDK (openlimit) — zero deps, streaming, types
- Plugin interface: GuardrailPlugin, MiddlewarePlugin, ProviderPlugin
- Plugin registry with config-driven loading
- HeaderInjector example middleware plugin
- A2A File/Data parts: fileUri, base64 bytes, structured JSON data
- Multi-tenant OIDC: multiple issuers with issuer-based token routing
- Request/reply body logging (opt-in via `logging.log_bodies: true`)
- Agent responses appended to task history for multi-turn continuity
- Rate limiting and budget checks for embeddings requests
- Tests for tracing (2), usage (2), lifecycle (2) packages
- Migration guide v1.0 → v1.1

### Changed
- Refactor server.go NewRuntime() — extracted 6 builder functions (29% reduction)
- Usage Writer now safely handles nil database
- Guardrail pipeline supports 'plugin' stage type

### Removed
- Dead code: unused recordUsage() method

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
