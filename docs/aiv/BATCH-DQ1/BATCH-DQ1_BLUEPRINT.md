BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-DQ1
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-04
Review SLA:               30 min
Execution SLA per Task:   60 min
Task Sequencing: TASK-01 first (refactor enables TASK-04), then TASK-02, TASK-03, TASK-04, TASK-05

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Improve design quality: refactor server.go, add missing tests for untested
packages, fix documentation gaps, extend governance to embeddings, remove dead code.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Refactor NewRuntime() into focused builder functions
  - Add tests for tracing, usage, and lifecycle packages
  - Create migration guide v1.0→v1.1 and update 4 other doc files
  - Wire rate limiting and budget checks into embeddings handler
  - Remove dead code (unused recordUsage, duplicate helpers)

What the code MUST NOT do:
  - MUST NOT change any existing test assertions
  - MUST NOT break any existing provider functionality

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: All existing tests must pass without assertion changes.
  HB-02: No new Go dependencies.

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
  Baseline: ~383 tests
  Expected delta: +15 new tests
  Expected total: ~398

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: Refactor server.go into Builder Functions
  Description: Extract NewRuntime() into 7 builder functions, keeping
    NewRuntime() as a thin orchestrator under 80 lines.
  Files in scope:
    - internal/server/server.go (MODIFY — extract builders)
  Depends on: None
  Required Tests:
    | Test ID          | Type | Pass Criteria                        |
    |:-----------------|:-----|:-------------------------------------|
    | TEST-DQ1-01-01   | unit | Existing server tests pass            |
    | TEST-DQ1-01-02   | unit | NewRuntime returns non-nil Runtime    |
  Acceptance Criteria:
    AC-01-01: NewRuntime() body under 80 lines

TASK-02: Add Missing Tests
  Description: Add unit tests for tracing (tracer creation, middleware),
    usage (batch writer), and lifecycle (tracker).
  Files in scope:
    - internal/tracing/tracing_test.go (CREATE)
    - internal/usage/writer_test.go (CREATE)
    - internal/lifecycle/tracker_test.go (CREATE)
  Depends on: None
  Required Tests:
    | Test ID          | Type | Pass Criteria                                  |
    |:-----------------|:-----|:-----------------------------------------------|
    | TEST-DQ1-02-01   | unit | tracing: NewTracer disabled returns no-op       |
    | TEST-D2-02-02    | unit | tracing: HTTP middleware adds span              |
    | TEST-DQ1-02-03   | unit | usage: Writer batches and records entries        |
    | TEST-DQ1-02-04   | unit | lifecycle: MarkShuttingDown + Wait completes    |
  Acceptance Criteria:
    AC-02-01: All 4 new test files pass

TASK-03: Documentation Updates
  Description: Create migration guide and update 4 doc files.
  Files in scope:
    - docs/migration-v1.1.md (CREATE)
    - docs/configuration.md (MODIFY)
    - docs/api-reference.md (MODIFY)
    - docs/governance.md (MODIFY)
    - docs/index.md (MODIFY)
  Depends on: None
  Acceptance Criteria:
    AC-03-01: Migration guide exists with v1.0→v1.1 upgrade steps
    AC-03-02: All 5 doc files updated

TASK-04: Extend Governance to Embeddings
  Description: Add rate limiting and budget checks to the embeddings handler
    before the provider call.
  Files in scope:
    - internal/api/openai/embeddings.go (MODIFY)
  Depends on: TASK-01 (refactored handler construction)
  Required Tests:
    | Test ID          | Type | Pass Criteria                                |
    |:-----------------|:-----|:---------------------------------------------|
    | TEST-DQ1-04-01   | unit | Embeddings respects RPM limit                 |
    | TEST-DQ1-04-02   | unit | Embeddings respects budget limit              |
  Acceptance Criteria:
    AC-04-01: Embeddings handler checks rate limit before provider call
    AC-04-02: All existing embeddings tests still pass

TASK-05: Remove Dead Code
  Description: Remove unused recordUsage() method and consolidate duplicate
    JSON quote stripping helpers.
  Files in scope:
    - internal/api/openai/chat_completions.go (MODIFY — remove recordUsage)
  Depends on: TASK-04 (ensure recordUsage is truly unused)
  Required Tests:
    | Test ID          | Type | Pass Criteria                        |
    |:-----------------|:-----|:-------------------------------------|
    | TEST-DQ1-05-01   | unit | go vet passes, full suite green       |
  Acceptance Criteria:
    AC-05-01: recordUsage() method removed
    AC-05-02: No compilation errors

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
───────────────────────────────────────────────────────────
  BAC-01: NewRuntime() under 80 lines
  BAC-02: Tests added for tracing, usage, lifecycle
  BAC-03: Migration guide and 4 docs updated
  BAC-04: Embeddings goes through rate limit + budget
  BAC-05: Dead code removed, go vet clean, all tests pass
  BAC-06: CHANGELOG updated, tag v1.1.2, AIV archived

───────────────────────────────────────────────────────────
LEAD RESPONSE TO REVIEW REPORT
───────────────────────────────────────────────────────────
Reviewer Report ID:       REVIEW-BATCH-DQ1-2026-05-04
Lead Decision:            [X] ACCEPT
Blueprint Version:        1.0
Lead Sign:                Craft Agent (Lead) — 2026-05-04T12:30:00Z
