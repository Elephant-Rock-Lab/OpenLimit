BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-11
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
Add mutual TLS (mTLS) support for outbound webhook calls made by the guardrails
pipeline. When configured, the webhook client presents a client certificate to
the webhook server for authentication.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Accept TLS config in guardrails webhook stage config (cert_file, key_file, ca_file)
  - Create HTTP client with TLS client certificates when configured
  - Fall back to default HTTP client when no TLS configured

What the code MUST NOT do:
  - MUST NOT change the guardrails pipeline interface
  - MUST NOT require TLS configuration (backward compatible)
  - MUST NOT add new dependencies

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: Existing webhook calls without TLS config MUST work identically.
  HB-02: Invalid TLS files MUST be logged as errors, not crash the server.

───────────────────────────────────────────────────────────
DATA MODELS
───────────────────────────────────────────────────────────
GuardrailStageConfig (existing):
  Config map[string]any — add optional keys: cert_file, key_file, ca_file

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
  Baseline: 408 | Delta: +4 | Total: 412

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-11/TASK-01 — Webhook mTLS Client
  Files in scope:
    - internal/guardrails/webhook.go       (MODIFY — add TLS client support)
    - internal/guardrails/webhook_test.go  (NEW — TLS tests)
  Required Tests:
    | TEST-11-01-01    | unit | Default client used when no TLS config |
    | TEST-11-01-02    | unit | TLS client created from cert/key files |
    | TEST-11-01-03    | unit | Invalid cert path returns error |
    | TEST-11-01-04    | unit | CA file is optionally loaded |
  Acceptance Criteria:
    AC-01-01: Webhook loads TLS config from stage config
    AC-01-02: Falls back to default when not configured

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
  BAC-01: Webhook calls support mTLS when configured
  BAC-02: CHANGELOG.md updated
  BAC-03: Documents archived

LEAD RESPONSE: [X] ACCEPT — v1.0 — Craft Agent (Lead) — 2026-05-03T19:07:00Z
