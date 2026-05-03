BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-12
Blueprint Version:        1.0
Cycle Mode:               SIMPLIFIED
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-03

SIMPLIFIED CYCLE ELIGIBILITY — confirm all:
  [X] Exactly 1 Task — add provider health endpoints
  [X] No existing source files modified (only extending health package)
  [ ] No Hard Boundaries required — SKIP (adding new endpoints to existing handler)
  [X] Single deliverable

Note: Hard Boundaries not required per §3.2. Using STANDARD cycle since
existing health/handler.go is modified. Re-declaring as STANDARD.

───────────────────────────────────────────────────────────

Batch ID:                 BATCH-12
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-03
Review SLA:               30 min
Execution SLA per Task:   60 min
Partial Sign-Off SLA:     15 min
Task Sequencing:          Sequential

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Add admin health endpoints that expose provider circuit breaker state,
latency metrics, and error rates. Add provider key validation at startup
and readiness check.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - GET /admin/health/providers — circuit breaker state, last error, recovery time
  - GET /admin/health/models — p50 latency, error rates per model
  - Provider key validation at /ready — fail readiness if no provider keys configured
  - Provider key validation at startup — warn if no keys configured
  - RBAC-protected (viewer minimum)

What the code MUST NOT do:
  - MUST NOT add new dependencies
  - MUST NOT modify existing health/tracker.go interface
  - MUST NOT change /health or /ready existing behavior

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: Existing /health and /ready endpoints MUST NOT change behavior.
  HB-02: New endpoints MUST require admin bearer token auth.

───────────────────────────────────────────────────────────
DATA MODELS / SCHEMA
───────────────────────────────────────────────────────────
Existing (internal/health/tracker.go):
  - ModelHealth: {Provider, Model, Region, LastSuccess, LastFailure, ConsecutiveFailures}
  - Tracker: methods RecordSuccess, RecordFailure, IsHealthy

Existing (internal/health/handler.go):
  - Handler (GET /health), ReadyHandlerWithOIDC (GET /ready)
  - ProviderReadinessStatus: {Name, Type, Ready, RequiresAuth, ConfiguredKeys, ActiveKeys, MissingEnv, Reason}

New endpoint responses:
  - GET /admin/health/providers: [{name, healthy, last_success, last_failure, consecutive_failures}]
  - GET /admin/health/models: [{provider, model, region, healthy, last_success, last_failure, consecutive_failures}]

───────────────────────────────────────────────────────────
DEPENDENCY MAP
───────────────────────────────────────────────────────────
  - internal/health/tracker.go (existing — Tracker.GetAll() to be added)
  - internal/health/handler.go (existing — /ready already validates keys)
  - internal/admin/handler.go (existing — route registration)

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
  Baseline at Blueprint issuance:  401 tests
  Expected delta:                  +6 new tests
  Expected total at Batch close:   407

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-12/TASK-01 — Provider Health Endpoints
  Description:      Add GetAll() method to Tracker, add admin health endpoints
                    that return provider/model health data. Register in admin routes.
  Files in scope:
    - internal/health/tracker.go          (MODIFY — add GetAll method)
    - internal/health/handler.go          (MODIFY — add admin handlers)
    - internal/health/tracker_test.go     (MODIFY — add GetAll test)
  Depends on:       None
  Required Tests:
    | Test ID          | Type | Pass Criteria |
    |:-----------------|:-----|:--------------|
    | TEST-12-01-01    | unit | GetAll returns all recorded health entries |
    | TEST-12-01-02    | unit | Admin provider health endpoint returns JSON array |
    | TEST-12-01-03    | unit | Admin model health endpoint returns JSON array |
    | TEST-12-01-04    | unit | Provider with no keys returns unhealthy in /ready |
    | TEST-12-01-05    | unit | Endpoints return 401 without bearer token |
  Acceptance Criteria:
    AC-01-01: GetAll() returns all ModelHealth entries from Tracker
    AC-01-02: /admin/health/providers returns provider health JSON
    AC-01-03: /admin/health/models returns model health JSON

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
───────────────────────────────────────────────────────────
  BAC-01: Admin health endpoints expose circuit breaker state
  BAC-02: Provider key validation at startup warns, at /ready reports
  BAC-03: CHANGELOG.md updated
  BAC-04: All documents archived

───────────────────────────────────────────────────────────
LEAD RESPONSE TO REVIEW REPORT
───────────────────────────────────────────────────────────

Reviewer Report ID:       REVIEW-BATCH-12-2026-05-03
Review Cycle:             1
Lead Decision:            [X] ACCEPT

Blueprint Version after response: 1.0
Lead Sign:                Craft Agent (Lead) — 2026-05-03T18:54:00Z
