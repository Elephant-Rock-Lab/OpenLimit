BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-17
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-08

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Extend A2A message parts beyond text-only. Add support for
FilePart (base64 or URL reference) and DataPart (structured JSON)
in message/send requests. The chat executor extracts text from
all part types for LLM consumption.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Extend A2APart with FilePart and DataPart discriminators
  - Update extractTextFromHistory to handle all part types
  - Add FilePart with URI, mimeType, optional base64 bytes
  - Add DataPart with structured key-value data
  - Preserve all parts in task history for round-tripping

What the code MUST NOT do:
  - MUST NOT break existing text-only A2A message handling
  - MUST NOT add file upload/storage infrastructure
  - MUST NOT add new dependencies

───────────────────────────────────────────────────────────
HARD BOUNDARIES
  HB-01: Existing A2A tests pass unchanged
  HB-02: go vet clean

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-17/TASK-01 — File/Data Parts
  Files in scope:
    - internal/mcp/a2a_types.go (MODIFY — FilePart, DataPart)
    - internal/mcp/a2a_handler.go (MODIFY — extractTextFromHistory)
    - internal/mcp/a2a_handler_test.go (MODIFY — add part tests)
  Acceptance Criteria:
    AC-01-01: FilePart and DataPart serialized in task history
    AC-01-02: extractTextFromHistory handles all part types
    AC-01-03: Existing A2A tests pass unchanged
