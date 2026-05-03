BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-10
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
Add a prompt management system that allows admins to create, list, and apply
prompt templates stored in the database. Templates can be prepended to chat
completion messages before they reach the provider.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - CRUD API: POST/GET/PUT/DELETE /admin/prompts
  - Prompt template model: id, name, content, description, created_at, updated_at
  - Apply prompt template: virtual keys can reference a prompt_id that gets prepended
  - Database migration for prompt_templates table
  - Admin route registration

What the code MUST NOT do:
  - MUST NOT modify the chat completion request schema
  - MUST NOT add prompt caching or versioning (future scope)
  - MUST NOT add new dependencies

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: Prompt application MUST NOT alter the original request body — system messages are prepended.
  HB-02: Admin endpoints MUST require bearer token auth.

───────────────────────────────────────────────────────────
DATA MODELS
───────────────────────────────────────────────────────────
PromptTemplate:
  id          TEXT PRIMARY KEY
  name        TEXT NOT NULL UNIQUE
  content     TEXT NOT NULL
  description TEXT
  created_at  TIMESTAMP
  updated_at  TIMESTAMP

───────────────────────────────────────────────────────────
DEPENDENCY MAP
───────────────────────────────────────────────────────────
  - internal/admin/handler.go (existing — route registration)
  - internal/store/store.go (existing — DB access)
  - internal/migrate/migrate.go (existing — migrations)

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
  Baseline: 407 | Delta: +6 | Total: 413

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-10/TASK-01 — Prompt Templates Backend
  Description:      Add database migration, store layer, admin CRUD handlers,
                    and route registration for prompt templates.
  Files in scope:
    - internal/migrate/migrate.go       (MODIFY — add prompt_templates table)
    - internal/store/prompts.go         (NEW — DB operations)
    - internal/admin/prompts.go         (NEW — CRUD handlers)
    - internal/admin/handler.go         (MODIFY — register routes)
    - internal/admin/prompts_test.go    (NEW — handler tests)
  Depends on:       None
  Required Tests:
    | Test ID          | Type | Pass Criteria |
    |:-----------------|:-----|:--------------|
    | TEST-10-01-01    | unit | Create prompt returns 201 with valid JSON |
    | TEST-10-01-02    | unit | List prompts returns array |
    | TEST-10-01-03    | unit | Delete prompt removes entry |
    | TEST-10-01-04    | unit | Duplicate name returns 409 |
    | TEST-10-01-05    | unit | Prompt with empty name returns 400 |
    | TEST-10-01-06    | unit | Update prompt changes content |
  Acceptance Criteria:
    AC-01-01: CRUD endpoints for /admin/prompts are functional
    AC-01-02: Database table created via migration
    AC-01-03: Routes require admin bearer token

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
───────────────────────────────────────────────────────────
  BAC-01: Admin can create/list/update/delete prompt templates via API
  BAC-02: CHANGELOG.md updated
  BAC-03: All documents archived

───────────────────────────────────────────────────────────
LEAD RESPONSE TO REVIEW REPORT
───────────────────────────────────────────────────────────
Reviewer Report ID:       REVIEW-BATCH-10-2026-05-03
Lead Decision:            [X] ACCEPT
Blueprint Version:        1.0
Lead Sign:                Craft Agent (Lead) — 2026-05-03T19:00:00Z
