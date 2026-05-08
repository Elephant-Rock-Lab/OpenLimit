# CODEBASE STATE

Last Updated:       [YYYY-MM-DD — must be a real date; placeholder is INVALID except during first bootstrap Batch]
Updated By:         [Lead Name / ID — via BATCH-NN Close]
Framework Version:  5.3

───────────────────────────────────────────────────────────
VERIFIED MODULE MAP
───────────────────────────────────────────────────────────
Verified paths and exports that future Batches can rely on.
Every entry here was confirmed by an Adaptation or manual audit.

  Module / Crate:     [e.g. kore_cap::keys]
  Actual export:      [e.g. ed25519_dalek::SigningKey]
  Verified in:        [BATCH-NN]
  Notes:              [e.g. "KeyPair was renamed in v0.3.0"]

[Repeat for each verified module]

───────────────────────────────────────────────────────────
ARCHITECTURAL DECISIONS
───────────────────────────────────────────────────────────
Decisions that constrain future work. Each entry explains WHY, not just WHAT.

  DEC-001:  [description — what was decided and why]
  Source:    [BATCH-NN / design doc ref]
  Active:    YES
  Overridden: [NO / YES — by DEC-NNN in BATCH-NN]

[Repeat for each decision]

───────────────────────────────────────────────────────────
KNOWN GOTCHAS
───────────────────────────────────────────────────────────
Things that surprised a previous Batch. Prevents re-surprise.

  GOTCHA-001: [description of the gotcha and its impact]
  Discovered:  [BATCH-NN]
  Status:      [OPEN / MITIGATED — describe mitigation]

[Repeat for each gotcha]

───────────────────────────────────────────────────────────
ADAPTATION LOG (ROLLING — LAST 10 BATCHES)
───────────────────────────────────────────────────────────
Consolidated from all Task Reports. New entries prepend.
Entries older than 10 Batches are archived to STATE_ARCHIVE.md.

[No entries yet — first Batch under v5.3]

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
Current total test count. Updated at every Batch Close.

  Last verified count: [N]
  Verified in:         [BATCH-NN / date]
  Breakdown:           [e.g. 142 unit / 23 integration / 4 e2e]

───────────────────────────────────────────────────────────
CARRY-FORWARD OBLIGATIONS
───────────────────────────────────────────────────────────
Deferred tests, known gaps, and promises from previous Batches
that are still outstanding. STATE.md is the sole authoritative
registry — all other references (Task Reports, Partial Sign-Offs,
Certificates) are reference fields that point here.

Entry format for deferred tests:
  DEFER-BATCH-NN-TASK-NN-TEST-NN: [description]
    Status:   [PENDING_LEAD_CONFIRMATION / OPEN / RESOLVED / VERIFIED_CLOSED / REJECTED]
    Source:    [Task Report ID]
    Promised:  [BATCH-NN]
    Resolved:  [BATCH-NN — only if RESOLVED or VERIFIED_CLOSED]

Entry format for known gaps:
  GAP-BATCH-NN: [description]
    Status:   [OPEN / CLOSED]

[No entries yet — first Batch under v5.3]

═══════════════════════════════════════════════════════════
