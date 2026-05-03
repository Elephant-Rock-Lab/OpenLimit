BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-09
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
Deliver a read-only admin dashboard SPA served from the Go binary (embed.FS)
that shows projects, virtual keys, usage analytics, and provider health status.
No Node.js build tooling required — pure HTML/CSS/JS.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Serve a single HTML page at GET /admin/dashboard with embedded CSS/JS
  - Dashboard shows: project list, virtual key list (masked), health status
  - Usage analytics view: cost per model/day, token trends, top models by spend
  - Real-time request log viewer: filterable by key, model, provider, status
  - Provider status cards: healthy/unhealthy, latency info, error rates
  - Authenticate via admin bearer token (same as admin API)
  - Use embed.FS to serve static files from Go binary

What the code MUST NOT do:
  - MUST NOT require Node.js, npm, or any build tooling
  - MUST NOT implement full CRUD — read-only dashboard is sufficient
  - MUST NOT add new API endpoints — use existing /admin/* endpoints
  - MUST NOT add new external dependencies

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  Lint command:  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: Dashboard MUST be served from Go binary via embed.FS — no external file serving.
  HB-02: Dashboard MUST use the same admin bearer token auth as existing /admin/* endpoints.
  HB-03: Dashboard MUST NOT create new admin API endpoints — it consumes existing ones.
  HB-04: No external JS/CSS frameworks — vanilla HTML/CSS/JS only (no React, Vue, etc.).

───────────────────────────────────────────────────────────
DATA MODELS / SCHEMA
───────────────────────────────────────────────────────────
Existing endpoints consumed by the dashboard:
  - GET /admin/projects → [{id, name, created_at}]
  - GET /admin/keys → [{id, key_prefix, name, project_id, allowed_models, rpm_limit, ...}]
  - GET /admin/usage?limit=100 → usage log rows
  - GET /admin/usage/summary → aggregated usage by period/model/provider
  - GET /health → {status, version}
  - GET /ready → {status, providers[{name, ready, configured_keys}]}

New files:
  - internal/admin/dashboard.go — embed.FS handler + route registration
  - internal/admin/static/ — directory with HTML/CSS/JS files
    - index.html — single-page dashboard

───────────────────────────────────────────────────────────
AUTHORITY RULES
───────────────────────────────────────────────────────────
  - Dashboard requires admin bearer token (same as all /admin/* endpoints)
  - Read-only: no create/update/delete operations from the dashboard

───────────────────────────────────────────────────────────
DEPENDENCY MAP
───────────────────────────────────────────────────────────
  - internal/admin/handler.go (existing — RegisterRoutes)
  - internal/admin/static/ (NEW — dashboard assets)
  - internal/server/server.go (existing — wiring)

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
  Baseline at Blueprint issuance:  393 tests
  Expected delta (all Tasks):      +8 new tests
  Expected total at Batch close:   401

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-09/TASK-01 — Dashboard Backend
  Description:      Create embed.FS handler for dashboard static files,
                    register GET /admin/dashboard route.
  Files in scope:
    - internal/admin/dashboard.go        (NEW)
    - internal/admin/dashboard_test.go   (NEW)
    - internal/admin/static/index.html   (NEW — placeholder, expanded in TASK-02)
  Depends on:       None
  Required Tests:
    | Test ID          | Type | Pass Criteria |
    |:-----------------|:-----|:--------------|
    | TEST-09-01-01    | unit | GET /admin/dashboard returns 200 with HTML content |
    | TEST-09-01-02    | unit | GET /admin/dashboard requires auth (401 without token) |
    | TEST-09-01-03    | unit | embed.FS correctly serves the HTML file |
  Acceptance Criteria:
    AC-01-01: DashboardHandler serves embedded HTML at /admin/dashboard
    AC-01-02: Route requires admin bearer token
    AC-01-03: Content-Type is text/html

TASK-02: BATCH-09/TASK-02 — Dashboard Frontend
  Description:      Build the full SPA with project/key list, usage analytics,
                    request log viewer, and provider status cards. All in a
                    single index.html with inline CSS and JS.
  Files in scope:
    - internal/admin/static/index.html   (MODIFY — full SPA implementation)
  Depends on:       TASK-01
  Required Tests:
    | Test ID          | Type | Pass Criteria |
    |:-----------------|:-----|:--------------|
    | TEST-09-02-01    | unit | Dashboard HTML contains key UI sections |
    | TEST-09-02-02    | unit | Dashboard HTML contains JS fetch calls to /admin/* endpoints |
    | TEST-09-02-03    | unit | Dashboard HTML contains CSS styles |
  Acceptance Criteria:
    AC-02-01: Dashboard has sections for projects, keys, usage, and providers
    AC-02-02: JS code calls existing /admin/* endpoints for data
    AC-02-03: No external JS/CSS dependencies

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
───────────────────────────────────────────────────────────
  BAC-01: Dashboard is accessible at /admin/dashboard with bearer token auth
  BAC-02: Dashboard displays projects, keys, usage analytics, and provider health
  BAC-03: CHANGELOG.md updated with BATCH-09 entry.
  BAC-04: All documents archived.

───────────────────────────────────────────────────────────
LEAD RESPONSE TO REVIEW REPORT
───────────────────────────────────────────────────────────

Reviewer Report ID:       REVIEW-BATCH-09-2026-05-03
Review Cycle:             1
Lead Decision:            [X] ACCEPT

Blueprint Version after response: 1.0
Lead Sign:                Craft Agent (Lead) — 2026-05-03T18:47:00Z
