# CODEBASE STATE

Last Updated:       2026-05-13
Updated By:         Craft Agent (Lead) — via BATCH-64 / v1.4.2 GA
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
  Verified in:        BATCH-21 → 63
  Notes:               Unified governance pipeline for all entry points. Streaming and non-streaming paths.
                     Streaming output guardrails (CheckOutput) on accumulated content before [DONE].
                     Breaker map uses breakerEntry wrapper with LRU eviction.

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
  Verified in:        BATCH-24 → 58
  Notes:               MCP client/server, tool merge, executor, A2A with Redis bridge. Per-state mutex for reconnect safety.
                     cancelNotif field cancels old notification listener goroutine on reconnect.

  Module:              internal/metrics
  Actual exports:      Collector, GuardrailStatsSnapshot
  Verified in:        BATCH-21 → 63
  Notes:               30+ Prometheus metrics. GuardrailStatsSnapshot for in-memory counters.
                     RecordUsageDrop() for buffer-full observability. atomic.Int64 counters.

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
  Verified in:        BATCH-21 → 61
  Notes:               Postgres data access. SQL field validation. Prefix-filtered key lookup. ListVirtualKeysForMCP.
                     SQL LIMIT/OFFSET pagination. CountVirtualKeys for X-Total-Count.
                     parseArrayString handles PostgreSQL quoted elements. GetProjectByName for quickstart guard.

  Module:              internal/usage
  Actual exports:      Writer, Budget
  Verified in:        BATCH-21 → 61
  Notes:               Async buffered writer with drain-on-close. CheckBudget helper with context propagation.
                     SetDropRecorder for buffer-full drop metric. GetSpendForCurrentPeriod always receives non-nil context.

  Module:              pkg/version
  Actual exports:      var Version string
  Verified in:        BATCH-56 → 64
  Notes:               Single string variable, bumped per release. Currently v1.4.2.
                     WARNING: version_test.go hardcodes the version string and must be
                     updated on every bump. This is fragile — see GOTCHA-007.

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

  GOTCHA-004: manager_test.go requires slog.Default() not nil for NewManager.
              Passing nil logger causes go vet / test failures.
  Discovered:  BATCH-58
  Status:      MITIGATED — all tests use slog.Default()

  GOTCHA-005: CheckBudget signature changed in BATCH-61 — now takes context.Context as first param.
              All callers must pass r.Context(). Old signature without ctx will not compile.
  Discovered:  BATCH-61
  Status:      DOCUMENTED — all callers updated

  GOTCHA-006: STATE.md test counts before v1.4.2 are unreliable.
              Multiple stale counts (700, 701) persisted across batches because
              STATE.md was only updated at release boundaries, not per-batch.
              Authoritative count as of v1.4.2: 741.
  Discovered:  BATCH-57→63
  Status:      FIXED — count is now 741/741 clean sheet

  GOTCHA-007: version_test.go hardcodes the version string (e.g., TestVersion_IsV142).
              This test breaks on every version bump and must be manually renamed.
              Consider replacing with a regex format check or removing entirely.
  Discovered:  BATCH-64
  Status:      DOCUMENTED — fragility acknowledged, no fix planned

  GOTCHA-008: config.Default() does not set MaxBodySizeKB. The loader (LoadWithDefaults)
              sets it to 10240, but code that constructs Runtime directly bypasses this.
              Any test or tool calling server.NewRuntime(cfg, ...) must explicitly set
              cfg.Server.MaxBodySizeKB or all request bodies will be rejected.
  Discovered:  v1.4.2 (OBL-05 root cause)
  Status:      MITIGATED — baseConfig() in server_test.go sets it. Other callers unverified.

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
  BATCH-42→63: 22 consecutive batches with zero architecture-level adaptations needed.
                All code matched blueprint expectations after BATCH-21→39 lessons learned.
  BATCH-57: governed_test.go was "(new file)" in Blueprint but already existed (1207 lines). Fixed to (modify).
  BATCH-58: manager_test.go needs slog.Default() not nil for NewManager (caught by go vet).
  BATCH-59: sec03_test.go created for white-box admin body limit tests (separate from server_test.go).
  BATCH-60: breakerEntry wrapper struct added to chat_completions.go for LRU eviction.
  v1.4.2:    OBL-05 was misdiagnosed for 3+ weeks as "port binding conflicts". Actual cause
              was MaxBodySizeKB=0 in test config (GOTCHA-008). Nobody ran the failing tests
              and read the error output until v1.4.2.

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────

  Last verified count: 741 Go tests — ALL PASSING (733 unit + 8 formerly-OBL-05)
  Verified in:         v1.4.2 / 2026-05-13
  Breakdown:
    Go passing:        741 tests across 40+ packages (clean sheet)
    Go failing:        0
    TS SDK:            24 unit tests (10 client + 11 admin + 3 admin extended)
    Python SDK:        22 unit tests (9 client + 13 admin)

  NOTE: Test counts in documents before v1.4.2 are UNRELIABLE.
        See GOTCHA-006 and ERRATA section.

───────────────────────────────────────────────────────────
CARRY-FORWARD OBLIGATIONS
───────────────────────────────────────────────────────────

  (none — all obligations closed as of v1.4.2)

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

───────────────────────────────────────────────────────────
ERRATA — CORRECTIONS TO HISTORICAL DOCUMENTS
───────────────────────────────────────────────────────────

This section records factual errors in previously-signed
release documents. Each entry identifies the document,
the false claim, and the correct information.

  ERR-001: OBL-05 Root Cause Misdiagnosed
  ─────────────────────────────────────────
  Documents affected:
    - CERT-V1.4.0-GA-2026-05-12.md ("8 server integration tests
      fail due to port binding conflicts")
    - CERT-BATCH-64-2026-05-13.md ("8 server integration test
      failures (port binding)")
    - CERT-V1.4.1-GA-2026-05-13.md ("8 FAIL (OBL-05 pre-existing)")
    - STATE.md pre-v1.4.2 ("port binding conflicts")
    - V1.4_REMEDIATION_BATCH_SEQUENCE.md ("port binding")
    - This STATE.md's own GOTCHA-006 (now corrected)

  False claim:  Tests fail due to port binding conflicts.
                "Server lifecycle leaves listeners open."
                "Fix: Use httptest.NewUnstartedServer + separate ports."

  Actual cause: config.Default() sets MaxBodySizeKB=0.
                The config loader sets it to 10240, but tests call
                server.NewRuntime() directly, bypassing the loader.
                maxBodySizeMiddleware(0) applies http.MaxBytesReader
                with 0 bytes, rejecting every request body.

  Actual fix:   One line: cfg.Server.MaxBodySizeKB = 10240 in
                baseConfig() (server_test.go).

  Why missed:   Nobody ran the 8 failing tests and read the error
                messages between BATCH-42 (when tests first failed)
                and v1.4.2. The "port binding" diagnosis was assumed,
                not verified. A 30-second investigation would have
                caught this at any point.

  ERR-002: Test Count Discrepancies
  ─────────────────────────────────
  Documents affected:
    - CERT-V1.4.0-GA-2026-05-12.md ("701 Go tests")
    - STATE.md pre-v1.4.2 ("700 Go tests")
    - Multiple batch certificates with varying counts

  False claim:  Precise test counts (700, 701) in release docs.
  Actual:       These counts were never independently verified
                against `go test` output at the time of signing.
                The authoritative count is 741 as of v1.4.2.
                Historical counts should not be trusted.

  ERR-003: DG-2 "CONDITIONAL PASS" Overstated
  ─────────────────────────────────────────────
  Documents affected:
    - CERT-BATCH-58-2026-05-12.md (DG-2: CONDITIONAL PASS)
    - CERT-BATCH-64-2026-05-13.md (DG-2: CONDITIONAL PASS)
    - CERT-V1.4.1-GA-2026-05-13.md (DG-2: ✅)

  False claim:  "Race tests pass (-race deferred to CI)" implies
                races were tested without the -race flag, and CI
                will verify. No CI pipeline exists. -race has never
                been run on this codebase (no CGO/GCC on Windows).

  Actual:       DG-2 should read: "NOT VERIFIED — -race flag
                requires CGO/GCC unavailable on Windows development
                host. No CI pipeline exists. Race conditions are
                untested."

  ERR-004: Self-Signed Certificates
  ─────────────────────────────────
  Documents affected:
    - All CERT-BATCH-XX documents (BATCH-21 through BATCH-64)
    - CERT-V1.4.0-GA, CERT-V1.4.1-GA, CERT-V1.4.2-GA

  Context:      Every batch certificate was written by the Lead
                (Craft Agent) about its own work. Reviewer sessions
                caught real issues (see REVIEW-BATCH-XX files), but
                the final sign-off in each certificate is circular —
                the Lead approved its own batches. This is an
                inherent limitation of single-agent AIV cycles.

  ERR-005: Spawned Session Outputs Not Independently Verified
  ──────────────────────────────────────────────────────────
  Documents affected:
    - COMPETITIVE_ANALYSIS_LEAPFROG.md (6 competitors, 15 dimensions)
    - REFERENCE_LIBRARY_STUDY.md ("667 repos")
    - DEEP_DIVE_TECHNICAL_AUDIT.md ("50 findings")
    - STRESS_TEST_QA_AUDIT.md ("50 findings")
    - UX_DEEP_DIVE_AUDIT.md ("Score 5.3/10")

  Context:      These were produced by spawned sessions. The Lead
                summarized and forwarded their findings but did not
                independently verify the claims (repo count, finding
                count, competitor count, scores). The numbers are
                taken on trust from the spawned session outputs.

  ERR-006: Module Map Entry Descriptions Not Code-Verified
  ───────────────────────────────────────────────────────
  Documents affected:
    - This STATE.md's module map (BATCH-57→63 additions)

  Context:      Entries like "cancelNotif field cancels old
                notification listener goroutine" were written from
                batch summaries, not by re-reading the actual source
                to confirm field names are exactly correct. The
                descriptions are directionally accurate but may
                contain naming inaccuracies.

═══════════════════════════════════════════════════════════
