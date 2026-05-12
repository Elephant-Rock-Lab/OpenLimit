# CODEBASE STATE

Last Updated:       2026-05-12
Updated By:         Craft Agent (Lead) — via BATCH-56 Close
Framework Version:  5.3

───────────────────────────────────────────────────────────
VERIFIED MODULE MAP
───────────────────────────────────────────────────────────
Verified paths and exports that future Batches can rely on.
Every entry here was confirmed by an Adaptation or manual audit.

  Module:              internal/admin
  Actual exports:      Handler, RegisterRoutes, MCPServersHandler, RoutingCostsHandler,
                       ReplayHandler, GuardrailCatalog, SpendHandler
  Verified in:        BATCH-45 → 56
  Notes:               Closure pattern for admin endpoints. Dashboard SPA served via embed.FS.

  Module:              internal/api/openai
  Actual exports:      Handler, ChatCompletions, ExecuteGoverned
  Verified in:        BATCH-21 → 56
  Notes:               Unified governance pipeline for all entry points. Streaming and non-streaming paths.

  Module:              internal/audit
  Actual exports:      Logger, Event
  Verified in:        BATCH-38
  Notes:               Buffered event channel with drain-on-close. Nil-DB logger is a no-op.

  Module:              internal/auth
  Actual exports:      Middleware, Context
  Verified in:        BATCH-26
  Notes:               Bearer token auth with prefix-filtered key lookup. DB grace-period fallback.

  Module:              internal/billing
  Actual exports:      PriceTable, CalculateCost
  Verified in:        BATCH-21
  Notes:               Takes []config.PriceEntry (not a map).

  Module:              internal/cache
  Actual exports:      Cache, ExactCache
  Verified in:        BATCH-21 → 56
  Notes:               Exact LRU + Redis-backed + TieredCache. Semantic cache in cache/semantic/ sub-package.

  Module:              internal/circuit
  Actual exports:      Breaker
  Verified in:        BATCH-21
  Notes:               Local + Redis-backed circuit breaker state.

  Module:              internal/config
  Actual exports:      Config, Load, RoutingConfig, ReplayConfig, CostWeights
  Verified in:        BATCH-37 → 56
  Notes:               Hot-reload via file watcher. DeepCopy prevents callback mutations. Non-reloadable fields require restart.

  Module:              internal/guardrails
  Actual exports:      Pipeline, Factory, Stages
  Verified in:        BATCH-23 → 50
  Notes:               6 built-in stages (PII, keyword, length, regex, json_schema, webhook) + plugin stage adapter.

  Module:              internal/health
  Actual exports:      Tracker
  Verified in:        BATCH-21
  Notes:               Per-model passive health tracking with 30-second window. Evicts stale entries >1 hour.

  Module:              internal/mcp
  Actual exports:      Client, Registry, Manager, ServerHandler, Executor, A2AHandler
  Verified in:        BATCH-24 → 53
  Notes:               MCP client/server, tool merge, executor, A2A with Redis bridge. Per-state mutex for reconnect safety.

  Module:              internal/metrics
  Actual exports:      Collector, GuardrailStatsSnapshot
  Verified in:        BATCH-21 → 50
  Notes:               30+ Prometheus metrics. GuardrailStatsSnapshot for in-memory counters (no Prometheus dependency).

  Module:              internal/oidc
  Actual exports:      Provider, UserLookupFunc
  Verified in:        BATCH-26
  Notes:               Multi-tenant OIDC with issuer-based token routing. 30-second discovery timeout.

  Module:              internal/plugins
  Actual exports:      Registry, HeaderInjector
  Verified in:        BATCH-23
  Notes:               Three plugin types (guardrail, middleware, provider). Thread-safe registry with panic-on-duplicate.

  Module:              internal/providers
  Actual exports:      Adapter, Target, KeyRing, ProviderDefault, DefaultRegistry
  Verified in:        BATCH-21 → 44
  Notes:               30+ providers in embedded registry. Case-insensitive lookup. User config wins over registry defaults.

  Module:              internal/ratelimit
  Actual exports:      Limiter
  Verified in:        BATCH-38
  Notes:               Token bucket + Redis sliding window. Background eviction (idle>10min, 10K cap). Close() stops goroutine.

  Module:              internal/replay
  Actual exports:      Manager, ReplayResult, ReplaySummary
  Verified in:        BATCH-55
  Notes:               Fire-and-forget shadow requests. Ring buffer (1000 results). Sample rate per route.

  Module:              internal/redis
  Actual exports:      Client
  Verified in:        BATCH-21
  Notes:               Standalone + cluster support. UniversalClient interface.

  Module:              internal/routing
  Actual exports:      Router, Plan, CostCatalog, SmartWeights
  Verified in:        BATCH-21 → 55
  Notes:               Weighted routing, fallbacks, region-aware (priority/latency). Smart routing: cost+latency+health scoring.

  Module:              internal/store
  Actual exports:      Store, Keys, Projects, RBAC
  Verified in:        BATCH-21 → 39
  Notes:               Postgres data access. SQL field validation. Prefix-filtered key lookup. ListVirtualKeysForMCP.

  Module:              internal/usage
  Actual exports:      Writer, Budget
  Verified in:        BATCH-21 → 39
  Notes:               Async buffered writer with drain-on-close. CheckBudget helper for consolidated budget enforcement.

  Module:              pkg/version
  Actual exports:      var Version string
  Verified in:        BATCH-56
  Notes:               Single string variable, bumped per release.

  Module:              sdks/typescript
  Actual exports:      OpenLimitClient (client), OpenLimitAdmin (admin)
  Verified in:        BATCH-22
  Notes:               24 tests. Admin uses adminToken. 11 admin methods.

  Module:              sdks/python
  Actual exports:      OpenLimitClient (client), OpenLimitAdmin (admin)
  Verified in:        BATCH-22
  Notes:               22 tests. Mirrors TypeScript admin client.

───────────────────────────────────────────────────────────
ARCHITECTURAL DECISIONS
───────────────────────────────────────────────────────────

  DEC-001:  Zero external dependencies philosophy
            All CLI tools (bench, test, doctor, mcp) are self-contained in-process.
            No CGO, no external binary dependencies.
  Source:    Design principle (BATCH-42 → 47)
  Active:    YES
  Overridden: NO

  DEC-002:  Provider registry embedded in binary (30+ providers)
            DefaultRegistry provides base_url, auth header, and type for known providers.
            Zero new adapter code for config-only providers.
  Source:    BATCH-42
  Active:    YES
  Overridden: NO

  DEC-003:  Case-insensitive provider lookup
            Provider names are normalized to lowercase in both config and registry.
            "OpenAI", "openai", "OPENAI" all resolve to the same provider.
  Source:    BATCH-42
  Active:    YES
  Overridden: NO

  DEC-004:  User config wins over registry defaults
            If a user sets base_url, type, or auth headers in config, those override
            the registry defaults. This is the ApplyDefaults pattern (AR-01).
  Source:    BATCH-42
  Active:    YES
  Overridden: NO

  DEC-005:  Closure pattern for admin endpoints
            Admin handlers use closures (no interface on Handler).
            Dependencies injected via constructor, not method arguments.
  Source:    BATCH-45 → 55
  Active:    YES
  Overridden: NO

  DEC-006:  MCP status via closure (not direct Manager reference)
            MCPServersHandler accesses MCP Manager state via closure function,
            not by holding a direct reference to the Manager.
  Source:    BATCH-53
  Active:    YES
  Overridden: NO

  DEC-007:  Provider default URLs set in validate.go, not in adapters
            Adapters never override defaults. Config validation is the single source of truth.
  Source:    BATCH-19, BATCH-21
  Active:    YES
  Overridden: NO

  DEC-008:  Usage logging uses concrete *usage.Writer, not an interface
            Test code creates Writer with nil DB for no-op recording.
  Source:    BATCH-21
  Active:    YES
  Overridden: NO

  DEC-009:  SDK admin clients are separate classes
            OpenLimitAdmin uses adminToken, OpenLimitClient uses apiKey.
            Not mixed into a single class.
  Source:    BATCH-22
  Active:    YES
  Overridden: NO

───────────────────────────────────────────────────────────
KNOWN GOTCHAS
───────────────────────────────────────────────────────────

  GOTCHA-001: Cohere API domain is api.cohere.com (.com, not .ai).
              If adding new providers, always verify the actual API domain.
  Discovered:  BATCH-21
  Status:      MITIGATED — fixed in validate.go, test added

  GOTCHA-002: EmbeddingsUsage in embeddings response is a pointer (*EmbeddingsUsage)
              with omitempty. Always nil-check before dereferencing.
  Discovered:  BATCH-21
  Status:      MITIGATED — nil-guard added, test added

  GOTCHA-003: Admin endpoints /admin/prompts and /admin/audit are registered
              in RegisterRoutes but NOT included in SDK admin clients.
  Discovered:  BATCH-22
  Status:      DOCUMENTED — acknowledged as future work

───────────────────────────────────────────────────────────
ADAPTATION LOG (ROLLING — LAST 10 BATCHES)
───────────────────────────────────────────────────────────

  BATCH-21/TASK-01: Blueprint stated validProviderTypes → actual: supportedProviderTypes. Resolution: Assistant read actual code.
  BATCH-21/TASK-01: Blueprint stated Validate() []string → actual: Validate() error. Resolution: Assistant read actual code.
  BATCH-21/TASK-03: Blueprint implied mock usage writer interface → actual: concrete *usage.Writer struct. Resolution: Used NewWriter(nil, logger, 100) for tests.
  BATCH-22: Blueprint said 6 TS tests, actual was 7. Resolution: Lead Response corrected count.
  BATCH-22: Blueprint said "8 admin methods", actual is 11. Resolution: Lead Response corrected count.
  BATCH-39: Reviewer CHK-20 flagged a2a_push_test.go already exists (4 tests). Resolution: Changed to (modify).
  BATCH-39: Test delta stated +18 in Blueprint v1.0, actual is +22 (arithmetic error). Resolution: Corrected in Blueprint v1.1.
  BATCH-42→55: 14 consecutive batches with zero architecture-level adaptations needed.
                All code matched blueprint expectations after BATCH-21→39 lessons learned.

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────

  Last verified count: 700 Go tests (551 baseline + 149 new across BATCH-42→55)
  Verified in:         BATCH-56 / 2026-05-12
  Breakdown:
    Go unit:           700 tests across 40+ packages
    TS SDK:            24 unit tests (10 client + 11 admin + 3 admin extended)
    Python SDK:        22 unit tests (9 client + 13 admin)

  Pre-existing:        8 server integration test failures (OBL-05 port binding) — deferred to post-launch

───────────────────────────────────────────────────────────
CARRY-FORWARD OBLIGATIONS
───────────────────────────────────────────────────────────

  OBL-05: 8 server integration tests fail due to port binding conflicts.
          Tests: TestProviderRequestTimeout, TestAuthRequired*, TestOpenAICompatible*,
          TestAnthropic*, TestFallbackFromPrimaryFailure, TestExactCacheHit.
          Cause: Server lifecycle (NewRuntime → httptest) leaves listeners open.
          Fix: Use httptest.NewUnstartedServer + separate ports.
    Status:   OPEN
    Source:    Pre-existing (before BATCH-42)
    Promised:  Post-launch

───────────────────────────────────────────────────────────
CLI TOOLS
───────────────────────────────────────────────────────────

  openlimit-gateway   cmd/gateway     Main gateway server
  openlimit-bench     cmd/bench       Latency benchmark (P50/P95/P99, ~17K req/sec)
  openlimit-test      cmd/test        Smoke test (in-process mock → gateway → validate)
  openlimit-doctor    cmd/doctor      Config doctor (provider/key/model diagnostics)
  openlimit-mcp       cmd/mcp         MCP hub: add (register server), search (tool catalog)

───────────────────────────────────────────────────────────
ADMIN API ENDPOINTS (10 new in v1.4)
───────────────────────────────────────────────────────────

  GET  /admin/providers/registry      Provider registry list (30+ providers)
  GET  /admin/usage/spend             Spend dashboard: per-key budget utilization
  GET  /admin/guardrails/catalog      6 built-in validators with config docs
  POST /admin/guardrails/test         Test sample text against a validator
  GET  /admin/guardrails/stats        Live guardrail hit counters and rates
  GET  /admin/mcp/servers             MCP server inventory with health status
  GET  /admin/tools                   Tool discovery endpoint
  GET  /admin/mcp/tools               MCP tool listing
  GET  /admin/routing/costs           Smart routing pricing catalog + strategy
  GET  /admin/routing/replay          Replay results + summary stats

═══════════════════════════════════════════════════════════
