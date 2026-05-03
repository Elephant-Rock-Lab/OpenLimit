BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-INT1
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-03
Review SLA:               30 min
Execution SLA per Task:   60 min
Task Sequencing:          Sequential

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Fix the root cause of the embeddings test hang and restore all 4 integration-tagged
tests to the default test run. Add database-backed integration tests for prompt
template CRUD.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Fix embeddings handler test wiring so mock server receives requests
  - Remove //go:build integration tag — all tests run by default
  - Verify all 4 embeddings handler tests pass

What the code MUST NOT do:
  - MUST NOT change production embeddings handler logic
  - MUST NOT add new test dependencies

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: Embeddings handler production code must not change.
  HB-02: All other tests must continue to pass.

───────────────────────────────────────────────────────────
DEPENDENCY MAP
───────────────────────────────────────────────────────────
  - internal/api/openai/embeddings_test.go
  - internal/api/openai/embeddings_integration_test.go
  - internal/api/openai/embeddings.go (read-only analysis)

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
  Baseline: ~416 tests (4 excluded by integration tag)
  Expected delta: +4 restored, 0 new
  Expected total: ~420

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-INT1/TASK-01 — Fix Embeddings Test Wiring
  Description:      Diagnose why h.Embeddings() hangs in tests. The embeddings
                    handler makes direct HTTP calls to the provider URL via
                    callProviderEmbeddings(). The test creates a mock server but
                    the handler's routing flow (executeEmbeddingsPlan) may not
                    correctly resolve the base URL from the test adapter config.
                    Fix the test helper or wire the adapter correctly so the
                    handler connects to the mock server.
  Files in scope:
    - internal/api/openai/embeddings_test.go          (MODIFY)
    - internal/api/openai/embeddings_integration_test.go (MODIFY → remove, merge back)
  Depends on:       None
  Required Tests:
    | Test ID          | Type | Pass Criteria                                      |
    |:-----------------|:-----|:---------------------------------------------------|
    | TEST-INT1-01-01  | unit | TestEmbeddings_ValidResponse passes within 5s       |
    | TEST-INT1-01-02  | unit | TestEmbeddings_BadModel passes                      |
    | TEST-INT1-01-03  | unit | TestEmbeddings_PassesAuthHeader passes              |
    | TEST-INT1-01-04  | unit | TestEmbeddings_ProviderFailure passes               |
  Acceptance Criteria:
    AC-01-01: All 4 previously-hanging tests now pass in default test run
    AC-01-02: No //go:build integration tags remain in embeddings tests
    AC-01-03: Full test suite passes with zero failures

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
───────────────────────────────────────────────────────────
  BAC-01: All 4 embeddings tests restored to default run
  BAC-02: Full test suite passes (30+ packages, 0 failures)
  BAC-03: CHANGELOG.md updated

───────────────────────────────────────────────────────────
LEAD RESPONSE TO REVIEW REPORT
───────────────────────────────────────────────────────────
Reviewer Report ID:       REVIEW-BATCH-INT1-2026-05-03
Lead Decision:            [X] ACCEPT
Blueprint Version:        1.0
Lead Sign:                Craft Agent (Lead) — 2026-05-03T19:28:00Z
