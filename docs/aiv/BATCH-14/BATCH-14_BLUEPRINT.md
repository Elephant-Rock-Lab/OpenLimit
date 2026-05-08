BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-14
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-08

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Create a TypeScript SDK package that wraps the OpenLimit Gateway
OpenAI-compatible API surface: chat completions (streaming + non-streaming),
embeddings, models listing, and health checks. The SDK follows the OpenAI
Node.js SDK pattern for familiarity.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Provide OpenLimitClient class with configurable base URL and API key
  - Support chat completions (streaming via SSE, non-streaming)
  - Support embeddings creation
  - Support models listing
  - Support health/ready checks
  - Export TypeScript types for all request/response shapes
  - Include README with usage examples
  - Build to ESM + CJS via tsup

What the code MUST NOT do:
  - MUST NOT require runtime dependencies beyond Node.js built-ins
  - MUST NOT bundle an HTTP client library (use native fetch)
  - MUST NOT depend on the OpenAI SDK (standalone)

───────────────────────────────────────────────────────────
LINT COMMAND
  cd sdks/typescript && npm run lint

───────────────────────────────────────────────────────────
HARD BOUNDARIES
  HB-01: Zero runtime dependencies
  HB-02: TypeScript strict mode

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-14/TASK-01 — SDK Core Implementation
  Description: Create the TypeScript SDK package with client class,
    types, streaming support, and build tooling.
  Files in scope:
    - sdks/typescript/package.json (CREATE)
    - sdks/typescript/tsconfig.json (CREATE)
    - sdks/typescript/src/index.ts (CREATE — main entry)
    - sdks/typescript/src/client.ts (CREATE — OpenLimitClient)
    - sdks/typescript/src/types.ts (CREATE — all types)
    - sdks/typescript/src/streaming.ts (CREATE — SSE parser)
    - sdks/typescript/src/errors.ts (CREATE — error types)
    - sdks/typescript/README.md (CREATE)
    - sdks/typescript/.gitignore (CREATE)
  Depends on: None
  Required Tests:
    | Test ID          | Type | Pass Criteria                                  |
    |:-----------------|:-----|:-----------------------------------------------|
    | TEST-14-01-01    | unit | Non-streaming chat completion returns response   |
    | TEST-14-01-02    | unit | Streaming chat completion yields chunks          |
    | TEST-14-01-03    | unit | Embeddings returns vector data                   |
    | TEST-14-01-04    | unit | Models listing returns model list                |
    | TEST-14-01-05    | unit | Health check returns status                      |
    | TEST-14-01-06    | unit | Error handling for non-2xx responses             |
  Acceptance Criteria:
    AC-01-01: SDK builds successfully with npm run build
    AC-01-02: All 6 unit tests pass
    AC-01-03: Zero runtime dependencies
    AC-01-04: README has complete usage examples

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
  BAC-01: TypeScript SDK package builds and tests pass
  BAC-02: Zero runtime dependencies, strict TypeScript
  BAC-03: README complete with examples
  BAC-04: CHANGELOG updated, version tagged

LEAD RESPONSE TO REVIEW REPORT
  Reviewer Report ID:       REVIEW-BATCH-14-2026-05-08
  Lead Decision:            [X] ACCEPT
  Blueprint Version:        1.0
  Lead Sign:                Craft Agent (Lead) — 2026-05-08T14:00:00Z
