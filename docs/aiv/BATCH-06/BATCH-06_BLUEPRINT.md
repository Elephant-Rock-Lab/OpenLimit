BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-06
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
Enable multi-instance A2A SSE streaming via Redis Pub/Sub so that A2A task
notifications propagate across gateway instances. A client subscribed to
task updates on Instance A receives events even when the task is executed
on Instance B.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Add a Redis-backed notification bridge (RedisTaskBridge) that publishes
    task updates to a Redis channel and subscribes to relay them locally
  - When Redis is enabled, the A2AHandler's notify() method publishes to
    the Redis bridge in addition to the existing in-memory TaskNotifier
  - When Redis is enabled, the SSE endpoint (handleTaskStream) receives
    events from both the local notifier and the Redis bridge
  - Graceful degradation: when Redis is unavailable, fall back to single-instance
    behavior (existing in-memory TaskNotifier only)
  - The Redis channel name MUST use a configurable prefix (default: "openlimit:a2a:")
  - The bridge MUST reconnect on Redis connection loss with exponential backoff

What the code MUST NOT do:
  - MUST NOT change the A2A JSON-RPC API or SSE event format
  - MUST NOT require Redis (Redis disabled = existing single-instance behavior unchanged)
  - MUST NOT add new external dependencies (use existing go-redis/v9)
  - MUST NOT change TaskNotifier's existing interface or break existing tests
  - MUST NOT modify the TaskStore interface or implementations

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  Lint command:  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: The existing TaskNotifier MUST NOT be removed or have its method
         signatures changed. The Redis bridge is additive alongside it.
  HB-02: When Redis is nil or unhealthy, the system MUST behave identically
         to the current single-instance implementation.
  HB-03: Task update messages published to Redis MUST be serialized as JSON
         and MUST include the full A2ATask JSON (same as SSE event payload).
  HB-04: The Redis bridge subscriber goroutine MUST be started in
         NewA2AHandler (when Redis is available) and stopped in Shutdown().

───────────────────────────────────────────────────────────
DATA MODELS / SCHEMA
───────────────────────────────────────────────────────────
Existing types (internal/mcp/a2a_types.go):
  - A2ATask: { ID, ContextID, Status, StatusMessage, History, Artifacts,
               Metadata, Model, CreatedAt, UpdatedAt }
  - TaskState: "submitted" | "working" | "completed" | "failed" | "canceled"

Existing types (internal/mcp/a2a_task_notifier.go):
  - TaskNotifier: { mu sync.Mutex, watchers map[string][]chan *A2ATask, closed bool }
  - Methods: Subscribe(taskID) chan *A2ATask, Notify(task *A2ATask),
             Unsubscribe(taskID, chan), Close()

Existing types (internal/redis/client.go):
  - Client wraps *goredis.Client, has Client() *goredis.Client accessor
  - Healthy() bool check

Redis channel format:
  - Channel: "openlimit:a2a:task_updates" (configurable prefix)
  - Message payload: JSON-encoded A2ATask (same as SSE event data)

New types (internal/mcp/a2a_redis_bridge.go):
  - RedisTaskBridge struct:
      redisClient *goredis.Client (from redis.Client.Client())
      channel     string
      notifier    *TaskNotifier  // reference to existing notifier for relay
      logger      *slog.Logger
      subCancel   context.CancelFunc
      instanceID  string         // unique per gateway instance for loop prevention
      closed      atomic.Bool

───────────────────────────────────────────────────────────
AUTHORITY RULES
───────────────────────────────────────────────────────────
  - The Redis bridge has no authentication beyond Redis's own connection auth
  - Task updates from other instances are treated as equally authoritative
    as local updates (no source filtering)
  - The bridge does not enforce governance — governance is enforced at the
    point of task execution (A2AHandler.executeTask)

───────────────────────────────────────────────────────────
DEPENDENCY MAP
───────────────────────────────────────────────────────────
  - internal/redis/client.go (existing — go-redis/v9 wrapper)
  - internal/mcp/a2a_types.go (existing — A2ATask)
  - internal/mcp/a2a_task_notifier.go (existing — TaskNotifier)
  - internal/mcp/a2a_handler.go (existing — A2AHandler)
  - internal/server/server.go (existing — wiring)
  - internal/config/config.go (existing — RedisConfig)

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
  Baseline at Blueprint issuance:  376 existing tests
  Expected delta (all Tasks):      +8 new tests
  Expected total at Batch close:   384

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-06/TASK-01 — Redis Task Bridge
  Description:      Create the RedisTaskBridge type that publishes A2A task
                    updates to a Redis channel and subscribes to relay remote
                    updates to the local TaskNotifier.
  Files in scope:
    - internal/mcp/a2a_redis_bridge.go      (NEW — bridge implementation)
    - internal/mcp/a2a_redis_bridge_test.go (NEW — bridge unit tests)
  Depends on:       None
  Required Tests:
    | Test ID          | Type        | Pass Criteria                                                    |
    |:-----------------|:------------|:-----------------------------------------------------------------|
    | TEST-06-01-01    | unit        | Publish serializes A2ATask as JSON and calls Redis PUBLISH        |
    | TEST-06-01-02    | unit        | Subscribe relays received messages to local TaskNotifier           |
    | TEST-06-01-03    | unit        | Subscribe filters out messages from same instance (loop prevention)|
    | TEST-06-01-04    | unit        | Close stops subscriber and cleans up                               |
    | TEST-06-01-05    | unit        | Bridge degrades gracefully when Redis client is nil                 |
  Acceptance Criteria:
    AC-01-01: RedisTaskBridge.Publish marshals A2ATask to JSON and publishes to Redis channel
    AC-01-02: RedisTaskBridge.Subscribe receives remote updates and calls notifier.Notify
    AC-01-03: Messages from the same instance origin are NOT re-notified (loop prevention)
    AC-01-04: Close() cancels subscription context and is idempotent

TASK-02: BATCH-06/TASK-02 — Wire Bridge into A2AHandler
  Description:      Integrate the RedisTaskBridge into A2AHandler so that
                    notify() publishes to Redis (when available) and the SSE
                    handler receives cross-instance events. Wire the bridge
                    creation in server.go.
  Files in scope:
    - internal/mcp/a2a_handler.go           (MODIFY — accept optional bridge, call in notify())
    - internal/mcp/a2a_handler_test.go      (MODIFY — add bridge integration tests)
    - internal/server/server.go             (MODIFY — create bridge when Redis enabled)
  Depends on:       TASK-01
  Required Tests:
    | Test ID          | Type        | Pass Criteria                                                    |
    |:-----------------|:------------|:-----------------------------------------------------------------|
    | TEST-06-02-01    | unit        | notify() calls bridge.Publish when bridge is not nil              |
    | TEST-06-02-02    | unit        | notify() works correctly when bridge is nil (single-instance)     |
    | TEST-06-02-03    | unit        | SSE handler receives cross-instance task updates via bridge relay  |
  Acceptance Criteria:
    AC-02-01: A2AHandler has a bridge field that is nil when Redis is disabled
    AC-02-02: notify() publishes to both local notifier and Redis bridge
    AC-02-03: Shutdown() closes the bridge before closing the notifier
    AC-02-04: server.go creates RedisTaskBridge when Redis is enabled and A2A is enabled

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
───────────────────────────────────────────────────────────
  BAC-01: A2A SSE streaming works across multiple gateway instances via Redis Pub/Sub
  BAC-02: Single-instance behavior is unchanged when Redis is not configured
  BAC-03: CHANGELOG.md updated with BATCH-06 entry.
  BAC-04: All documents archived under /docs/aiv/BATCH-06/.

───────────────────────────────────────────────────────────
LEAD RESPONSE TO REVIEW REPORT
───────────────────────────────────────────────────────────

Reviewer Report ID:       REVIEW-BATCH-06-2026-05-03
Review Cycle:             1
Lead Decision:            [X] ACCEPT   [ ] ACCEPT WITH MODIFICATIONS   [ ] REJECT

If ACCEPT WITH MODIFICATIONS — list each Reviewer flag acted on:
  N/A — zero flags raised

If REJECT — reason and next action:
  N/A

Blueprint Version after response: 1.0
Lead Sign:                Craft Agent (Lead) — 2026-05-03T15:11:00Z

═══════════════════════════════════════════════════════════
