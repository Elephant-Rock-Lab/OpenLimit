BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-16
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-08

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Add a Go plugin interface that allows loading custom middleware,
guardrail stages, and provider adapters as compiled .so plugins
(at runtime on Linux) or as compiled-in extensions via a registry.

For Windows compatibility, implement a static registry approach
where plugins are registered at init time.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Define plugin interfaces: GuardrailPlugin, MiddlewarePlugin, ProviderPlugin
  - Create a PluginRegistry for registration and lookup
  - Support config-driven plugin loading via gateway.yaml
  - Integration point: guardrail pipeline loads GuardrailPlugin stages
  - Example plugin: header-injector middleware

What the code MUST NOT do:
  - MUST NOT use Go plugin package (not supported on Windows)
  - MUST NOT break existing guardrail or middleware functionality
  - MUST NOT add external dependencies

───────────────────────────────────────────────────────────
HARD BOUNDARIES
  HB-01: No Go plugin .so loading (Windows-incompatible)
  HB-02: go vet clean, all existing tests pass

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-16/TASK-01 — Plugin Interface + Registry
  Files in scope:
    - internal/plugins/interfaces.go (CREATE)
    - internal/plugins/registry.go (CREATE)
    - internal/plugins/registry_test.go (CREATE)
    - internal/plugins/header_injector.go (CREATE — example)
    - internal/guardrails/pipeline.go (MODIFY — plugin stage support)
    - internal/config/config.go (MODIFY — plugins config section)
  Acceptance Criteria:
    AC-01-01: Plugin interfaces defined (GuardrailPlugin, MiddlewarePlugin)
    AC-01-02: Registry supports Register/Lookup/List
    AC-01-03: Example header-injector plugin works
    AC-01-04: Guardrail pipeline can load plugin stages
