BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-RP1
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
Prepare the v1.1 codebase for release: fix the flaky embeddings test,
initialize git, bump version to v1.1.0, create per-batch commits,
create .gitignore, and archive all AIV documents under docs/aiv/.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Tag TestEmbeddings_ValidResponse as integration-only (//go:build integration)
  - Initialize git repository with proper .gitignore
  - Bump version from "dev" to "v1.1.0"
  - Create one commit per v1.1 batch following AIV §8.3
  - Create git tag v1.1.0
  - Copy all AIV batch documents to docs/aiv/BATCH-*/ directories

What the code MUST NOT do:
  - MUST NOT delete or alter passing tests
  - MUST NOT change any production logic
  - MUST NOT modify the embeddings handler implementation

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: No production code logic may be changed — only test tags and version strings.
  HB-02: All existing passing tests MUST continue to pass after changes.

───────────────────────────────────────────────────────────
DATA MODELS / SCHEMA
───────────────────────────────────────────────────────────
N/A — no schema changes.

───────────────────────────────────────────────────────────
AUTHORITY RULES
───────────────────────────────────────────────────────────
  - Git commits follow AIV §8.3 format: one commit per role action.
  - Version is set once and tagged.

───────────────────────────────────────────────────────────
DEPENDENCY MAP
───────────────────────────────────────────────────────────
  - internal/api/openai/embeddings_test.go (existing — flaky test)
  - pkg/version/version.go (existing — version string)
  - All AIV plan documents in sessions/260503-soft-chrome/plans/

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
  Baseline at Blueprint issuance:  ~416 tests (1 skipped/flaky)
  Expected delta:                  0 new tests, 1 test moved to integration tag
  Expected total at Batch close:   ~416 (same count, 1 excluded by build tag)

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-RP1/TASK-01 — Fix Flaky Test
  Description:      Add //go:build integration tag to TestEmbeddings_ValidResponse
                    so it only runs with -tags=integration. Verify full suite passes.
  Files in scope:
    - internal/api/openai/embeddings_test.go  (MODIFY — add build tag)
  Depends on:       None
  Required Tests:
    | Test ID          | Type | Pass Criteria                                        |
    |:-----------------|:-----|:-----------------------------------------------------|
    | TEST-RP1-01-01   | unit | Full test suite passes (zero failures, zero timeouts) |
  Acceptance Criteria:
    AC-01-01: TestEmbeddings_ValidResponse excluded from default test run
    AC-01-02: go vet ./... produces zero warnings
    AC-01-03: All other embeddings tests still run and pass

TASK-02: BATCH-RP1/TASK-02 — Git Init + Version Bump + Commits + Archive
  Description:      Initialize git, create .gitignore, bump version to v1.1.0,
                    make initial commit, create per-batch commits, tag v1.1.0,
                    copy AIV documents to docs/aiv/ directories.
  Files in scope:
    - .gitignore                              (NEW)
    - pkg/version/version.go                  (MODIFY — version string)
    - docs/aiv/BATCH-06/*                     (NEW — copied from plans/)
    - docs/aiv/BATCH-08/*                     (NEW — copied from plans/)
    - docs/aiv/BATCH-09/*                     (NEW — copied from plans/)
    - docs/aiv/BATCH-10/*                     (NEW — copied from plans/)
    - docs/aiv/BATCH-11/*                     (NEW — copied from plans/)
    - docs/aiv/BATCH-12/*                     (NEW — copied from plans/)
    - docs/aiv/BATCH-13/*                     (NEW — copied from plans/)
  Depends on:       TASK-01
  Required Tests:
    | Test ID          | Type   | Pass Criteria                              |
    |:-----------------|:-------|:-------------------------------------------|
    | TEST-RP1-02-01   | manual | git log shows per-batch commits            |
    | TEST-RP1-02-02   | manual | git tag v1.1.0 exists                      |
    | TEST-RP1-02-03   | manual | docs/aiv/ contains all batch documents     |
  Acceptance Criteria:
    AC-02-01: .gitignore exists and excludes standard Go artifacts
    AC-02-02: pkg/version/version.go contains "v1.1.0"
    AC-02-03: One commit per v1.1 batch in git log
    AC-02-04: Tag v1.1.0 exists
    AC-02-05: All AIV documents under docs/aiv/BATCH-*/

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
───────────────────────────────────────────────────────────
  BAC-01: Full test suite passes with zero failures
  BAC-02: Version is v1.1.0
  BAC-03: CHANGELOG.md already updated (confirmed in v1.1 batches)
  BAC-04: All AIV documents archived under docs/aiv/

───────────────────────────────────────────────────────────
LEAD RESPONSE TO REVIEW REPORT
───────────────────────────────────────────────────────────

Reviewer Report ID:       REVIEW-BATCH-RP1-2026-05-03
Review Cycle:             1
Lead Decision:            [X] ACCEPT

Blueprint Version after response: 1.0
Lead Sign:                Craft Agent (Lead) — 2026-05-03T19:15:00Z
