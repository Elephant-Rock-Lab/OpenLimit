BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-13
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-03

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Add Redis Cluster support so the gateway can connect to a Redis Cluster
instead of a single Redis instance. Configured via a `cluster` flag in the
Redis config section.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
MUST:
  - Add `cluster: bool` to RedisConfig
  - NewClusterClient() creates a cluster client when cluster=true
  - Existing NewClient() unchanged for single-instance mode
  - Graceful fallback when cluster unavailable

MUST NOT:
  - Change existing NewClient() behavior
  - Add new dependencies

───────────────────────────────────────────────────────────
HARD BOUNDARIES
  HB-01: cluster=false (default) MUST produce identical behavior to current code.

───────────────────────────────────────────────────────────
LINT COMMAND
  go vet ./...

───────────────────────────────────────────────────────────
TEST BASELINE
  Baseline: 412 | Delta: +4 | Total: 416

───────────────────────────────────────────────────────────
TASK LIST

TASK-01: BATCH-13/TASK-01 — Redis Cluster Client
  Files in scope:
    - internal/redis/client.go      (MODIFY — add cluster support)
    - internal/redis/client_test.go (MODIFY — add cluster tests)
  Tests:
    | TEST-13-01-01 | unit | NewClient returns nil when addr empty (unchanged) |
    | TEST-13-01-02 | unit | Cluster config creates ClusterClient |
    | TEST-13-01-03 | unit | Non-cluster config creates regular Client |
    | TEST-13-01-04 | unit | ClusterClient has Healthy() check |

LEAD RESPONSE: [X] ACCEPT — v1.0 — Craft Agent (Lead) — 2026-05-03T19:12:00Z
