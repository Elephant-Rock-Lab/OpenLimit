BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-FX1
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-04
Review SLA:               30 min
Execution SLA per Task:   60 min
Task Sequencing:          Sequential (TASK-03 depends on TASK-01, TASK-02)

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Fix the two critical bugs identified in COMP-RPT-2026-05-04:
1. Wire missing provider adapters (bedrock, vertex, groq, cohere, mistral) in server.go
2. Fix A2A Redis bridge for cluster mode (use UniversalClient)
Then version bump to v1.1.1 and tag.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Instantiate all 10 provider adapter types in server.go NewRuntime()
  - Accept all 10 provider types in config validation
  - Add config fields needed by bedrock (region) and vertex (project, region, publisher)
  - Change a2a_redis_bridge to use UniversalClient instead of *goredis.Client
  - Pass server.go redisClient.Universal() instead of .Standalone()
  - All existing tests continue to pass
  - go vet passes with zero warnings

What the code MUST NOT do:
  - MUST NOT change any provider adapter logic
  - MUST NOT change any test assertions
  - MUST NOT break existing provider configurations (openai, anthropic, gemini, azure)

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: No provider adapter source files may be modified (only wiring + config).
  HB-02: All existing tests must pass without modification to their assertions.
  HB-03: No new Go dependencies may be added.

───────────────────────────────────────────────────────────
DEPENDENCY MAP
───────────────────────────────────────────────────────────
  - internal/config/config.go (read for ProviderConfig structure)
  - internal/config/validate.go (read for supportedProviderTypes)
  - internal/server/server.go (read for adapter switch)
  - internal/mcp/a2a_redis_bridge.go (read for constructor signature)
  - internal/providers/{bedrock,vertex,groq,cohere,mistral}/adapter.go (read for New signatures)

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
  Baseline at Blueprint issuance: 378 existing tests
  Expected delta (all Tasks): +5 new tests
  Expected total at Batch close: 383

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-FX1/TASK-01 — Wire Missing Provider Adapters
  Description: Add 5 missing provider types to config validation, add config
    fields for bedrock/vertex, and add 5 switch cases in server.go to instantiate
    their adapters. Update gateway.example.yaml to document all 10 providers.
  Files in scope:
    - internal/config/config.go              (MODIFY — add Region, Project, Publisher fields)
    - internal/config/validate.go            (MODIFY — add 5 provider types + validation rules)
    - internal/server/server.go              (MODIFY — add 5 adapter switch cases)
    - internal/server/server_test.go         (MODIFY — add adapter wiring test)
    - configs/gateway.example.yaml           (MODIFY — uncomment bedrock/vertex/groq/cohere/mistral)
  Depends on: None
  Required Tests:
    | Test ID          | Type | Pass Criteria                                    |
    |:-----------------|:-----|:-------------------------------------------------|
    | TEST-FX1-01-01   | unit | Config validation accepts all 10 provider types   |
    | TEST-FX1-01-02   | unit | Config validation rejects vertex without project   |
    | TEST-FX1-01-03   | unit | NewRuntime creates adapter for each provider type  |
  Acceptance Criteria:
    AC-01-01: All 10 provider types pass config validation
    AC-01-02: server.go creates adapters for bedrock, vertex, groq, cohere, mistral
    AC-01-03: gateway.example.yaml documents all 10 providers with correct config

TASK-02: BATCH-FX1/TASK-02 — Fix A2A Redis Bridge for Cluster Mode
  Description: Change a2a_redis_bridge.go to accept UniversalClient interface
    instead of *goredis.Client. Update server.go to pass Universal() instead
    of Standalone(). Update bridge tests to match.
  Files in scope:
    - internal/mcp/a2a_redis_bridge.go       (MODIFY — change parameter type)
    - internal/mcp/a2a_redis_bridge_test.go   (MODIFY — update mock)
    - internal/server/server.go               (MODIFY — line 442: .Standalone() → .Universal())
  Depends on: None
  Required Tests:
    | Test ID          | Type | Pass Criteria                                        |
    |:-----------------|:-----|:-----------------------------------------------------|
    | TEST-FX1-02-01   | unit | Bridge publishes via UniversalClient interface         |
    | TEST-FX1-02-02   | unit | Existing bridge tests pass with updated signature      |
  Acceptance Criteria:
    AC-02-01: NewRedisTaskBridge accepts UniversalClient (not *goredis.Client)
    AC-02-02: server.go passes redisClient.Universal()
    AC-02-03: All existing bridge tests pass

TASK-03: BATCH-FX1/TASK-03 — Version Bump + CHANGELOG + Tag
  Description: Bump version to v1.1.1, update CHANGELOG, commit, tag.
  Files in scope:
    - pkg/version/version.go                  (MODIFY — v1.1.1)
    - CHANGELOG.md                            (MODIFY — add v1.1.1 section)
  Depends on: TASK-01, TASK-02
  Required Tests:
    | Test ID          | Type | Pass Criteria                        |
    |:-----------------|:-----|:-------------------------------------|
    | TEST-FX1-03-01   | unit | go vet passes, full suite green       |
    | TEST-FX1-03-02   | unit | Tag v1.1.1 exists in git              |
  Acceptance Criteria:
    AC-03-01: pkg/version/version.go = "v1.1.1"
    AC-03-02: CHANGELOG.md has v1.1.1 entry
    AC-03-03: Git tag v1.1.1 exists

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
───────────────────────────────────────────────────────────
  BAC-01: All 10 provider types wired and validated
  BAC-02: A2A Redis bridge accepts UniversalClient
  BAC-03: go vet zero warnings, 30+ packages pass, 0 failures
  BAC-04: CHANGELOG.md updated, tag v1.1.1 exists
  BAC-05: All AIV documents archived to docs/aiv/BATCH-FX1/

───────────────────────────────────────────────────────────
LEAD RESPONSE TO REVIEW REPORT
───────────────────────────────────────────────────────────
Reviewer Report ID:       REVIEW-BATCH-FX1-2026-05-04
Lead Decision:            [X] ACCEPT
Blueprint Version:        1.0
Lead Sign:                Craft Agent (Lead) — 2026-05-04T12:00:00Z
