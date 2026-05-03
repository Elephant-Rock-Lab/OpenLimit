BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-08
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
Add configuration hot-reload so that changes to gateway.yaml are detected and
applied at runtime without restarting the gateway process. Reloadable fields
include logging level, guardrails configuration, routing defaults, provider
keys, and model routes. Non-reloadable fields (server port, database URL,
Redis address, KMS config) require a restart.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Watch the config file (gateway.yaml) for changes using filesystem events
  - On change, reload and validate the new config
  - Apply reloadable fields atomically via a callback mechanism
  - Log config reload events (success or validation failure)
  - Skip reload if validation fails (keep current config)
  - Support SIGHUP as an alternative trigger for manual reload
  - Debounce rapid file changes (within 1 second window)

What the code MUST NOT do:
  - MUST NOT reload server port, database URL, Redis address, or KMS config
  - MUST NOT restart the HTTP server or close existing connections
  - MUST NOT add new external dependencies (use stdlib os/signal, no fsnotify)
  - MUST NOT change the existing config.Load() function signature
  - MUST NOT block the main goroutine on file watching

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  Lint command:  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: The hot-reload watcher MUST NOT modify the existing config.Load()
         function. It must use Load() internally but not change its signature.
  HB-02: Non-reloadable fields (server host/port, database URL, Redis addr,
         KMS config) MUST NOT be applied. Only reloadable fields are merged.
  HB-03: If the new config fails validation, the running config MUST remain
         unchanged and a warning MUST be logged.
  HB-04: The watcher MUST be stoppable via context cancellation and MUST be
         started and stopped in main.go alongside the HTTP server.

───────────────────────────────────────────────────────────
DATA MODELS / SCHEMA
───────────────────────────────────────────────────────────
Existing (internal/config/config.go):
  - Config struct with all fields
  - Default() function
  - Load(path) (Config, error)

Existing (internal/config/validate.go):
  - Validate(cfg Config) error

Existing (internal/config/loader.go):
  - Load(path) reads YAML, normalizes, validates

New types (internal/config/watcher.go):
  - Watcher struct:
      path       string           // config file path
      current    atomic.Value     // stores *Config (current validated config)
      onChange   func(old, new Config)
      logger     *slog.Logger
      cancel     context.CancelFunc
      debounceMu sync.Mutex
      lastReload time.Time
  - ReloadableConfig struct — fields that CAN be hot-reloaded:
      Logging    LoggingConfig
      Guardrails GuardrailsConfig
      Routing    RoutingConfig
      Providers  map[string]ProviderConfig
      Models     map[string]ModelConfig
      Billing    BillingConfig

───────────────────────────────────────────────────────────
AUTHORITY RULES
───────────────────────────────────────────────────────────
  - Config reload is authoritative — once applied, the new values replace the old
  - Validation failure prevents application — the running config is the source of truth
  - Only the watcher goroutine may trigger reloads; no external trigger beyond SIGHUP
  - Reload events are logged at INFO level; failures at WARN level

───────────────────────────────────────────────────────────
DEPENDENCY MAP
───────────────────────────────────────────────────────────
  - internal/config/config.go (existing — Config struct)
  - internal/config/loader.go (existing — Load function)
  - internal/config/validate.go (existing — Validate function)
  - cmd/gateway/main.go (existing — startup/shutdown wiring)

───────────────────────────────────────────────────────────
TEST BASELINE
───────────────────────────────────────────────────────────
  Baseline at Blueprint issuance:  385 tests
  Expected delta (all Tasks):      +8 new tests
  Expected total at Batch close:   393

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-08/TASK-01 — Config Watcher Implementation
  Description:      Create the config file watcher that detects changes to
                    gateway.yaml, reloads, validates, and applies reloadable
                    fields via a callback.
  Files in scope:
    - internal/config/watcher.go        (NEW — watcher implementation)
    - internal/config/watcher_test.go   (NEW — watcher unit tests)
  Depends on:       None
  Required Tests:
    | Test ID          | Type   | Pass Criteria                                              |
    |:-----------------|:------|:-----------------------------------------------------------|
    | TEST-08-01-01    | unit  | Watcher detects file modification and reloads config        |
    | TEST-08-01-02    | unit  | Watcher skips reload when validation fails                  |
    | TEST-08-01-03    | unit  | Watcher debounces rapid changes (within 1s window)          |
    | TEST-08-01-04    | unit  | ReloadableFields returns only safe-to-reload fields          |
    | TEST-08-01-05    | unit  | MergeReloadable applies only reloadable fields to target     |
  Acceptance Criteria:
    AC-01-01: Watcher polls file modification time and triggers reload on change
    AC-01-02: Validation failure prevents config application, logs warning
    AC-01-03: Debounce prevents multiple reloads within 1 second
    AC-01-04: ReloadableFields extracts safe-to-reload subset of Config
    AC-01-05: Close stops the watcher goroutine

TASK-02: BATCH-08/TASK-02 — Wire Watcher into main.go
  Description:      Start the config watcher in main.go after server startup.
                    Add SIGHUP signal handler for manual reload trigger.
                    Wire the onChange callback to log the reload event.
  Files in scope:
    - cmd/gateway/main.go               (MODIFY — start/stop watcher)
    - internal/server/server.go          (MODIFY — accept config reload callback if needed)
  Depends on:       TASK-01
  Required Tests:
    | Test ID          | Type   | Pass Criteria                                          |
    |:-----------------|:------|:-------------------------------------------------------|
    | TEST-08-02-01    | unit  | main.go starts watcher when config path exists           |
    | TEST-08-02-02    | unit  | SIGHUP triggers a config reload                          |
    | TEST-08-02-03    | unit  | Watcher is stopped on graceful shutdown                   |
  Acceptance Criteria:
    AC-02-01: Watcher started in main.go after config load
    AC-02-02: SIGHUP triggers manual reload
    AC-02-03: Watcher.Close() called during shutdown sequence

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
───────────────────────────────────────────────────────────
  BAC-01: Config file changes are detected and applied without restart
  BAC-02: Non-reloadable fields are never changed at runtime
  BAC-03: CHANGELOG.md updated with BATCH-08 entry.
  BAC-04: All documents archived.

───────────────────────────────────────────────────────────
LEAD RESPONSE TO REVIEW REPORT
───────────────────────────────────────────────────────────

Reviewer Report ID:       REVIEW-BATCH-08-2026-05-03
Review Cycle:             1
Lead Decision:            [X] ACCEPT

Blueprint Version after response: 1.0
Lead Sign:                Craft Agent (Lead) — 2026-05-03T18:42:00Z
