BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-07
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-04

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Support multi-turn A2A conversations where message/send accepts a taskId
to continue an existing task. Messages are appended to the task history and
re-executed through the chat executor.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Accept optional taskId parameter in message/send JSON-RPC method
  - If taskId provided, load existing task, append new user message, re-execute
  - Maintain full conversation history in the task
  - Return existing task state immediately if task is already working
  - Preserve single-turn behavior when no taskId provided

What the code MUST NOT do:
  - MUST NOT change the A2A JSON-RPC protocol for single-turn
  - MUST NOT add stateful session management beyond the task
  - MUST NOT break existing A2A tests

───────────────────────────────────────────────────────────
LINT COMMAND
  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
  HB-01: Existing single-turn message/send must work identically
  HB-02: No new dependencies

───────────────────────────────────────────────────────────
TEST BASELINE
  Baseline: ~398 tests
  Expected delta: +4 new tests
  Expected total: ~402

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-07/TASK-01 — Multi-Turn message/send
  Description: Add taskId support to the message/send handler. When taskId
    is provided, load the task, append the new message to its history, and
    re-execute. The task store already supports updating tasks.
  Files in scope:
    - internal/mcp/a2a_handler.go (MODIFY — handleMessageSend)
    - internal/mcp/a2a_handler_test.go (MODIFY — add multi-turn tests)
    - internal/mcp/a2a_types.go (MODIFY — add Message field to task)
  Depends on: None
  Required Tests:
    | Test ID          | Type | Pass Criteria                                  |
    |:-----------------|:-----|:-----------------------------------------------|
    | TEST-07-01-01    | unit | message/send with taskId appends to existing    |
    | TEST-07-01-02    | unit | message/send without taskId works as before     |
    | TEST-07-01-03    | unit | message/send with invalid taskId returns error  |
    | TEST-07-01-04    | unit | Multi-turn maintains full conversation history   |
  Acceptance Criteria:
    AC-01-01: taskId parameter accepted in message/send
    AC-01-02: Existing single-turn tests pass unchanged
    AC-01-03: Full conversation history stored in task

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
  BAC-01: Multi-turn message/send works with taskId
  BAC-02: Single-turn message/send unchanged
  BAC-03: go vet clean, all tests pass
  BAC-04: CHANGELOG updated

LEAD RESPONSE TO REVIEW REPORT
  Reviewer Report ID:       REVIEW-BATCH-07-2026-05-04
  Lead Decision:            [X] ACCEPT
  Blueprint Version:        1.0
  Lead Sign:                Craft Agent (Lead) — 2026-05-04T14:00:00Z
