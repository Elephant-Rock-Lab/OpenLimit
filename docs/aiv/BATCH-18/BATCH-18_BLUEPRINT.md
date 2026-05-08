BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-18
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-08

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Add multi-tenant OIDC support so the gateway can validate tokens
from multiple OIDC providers (e.g., Auth0, Keycloak, Cognito)
simultaneously, with per-provider audience and claims mapping.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Support multiple OIDC providers in config
  - Per-provider: issuer URL, audience, claims mapping
  - Token validation against the correct provider based on issuer
  - Provider lookup cache with TTL-based refresh
  - Backward-compatible with single OIDC config

What the code MUST NOT do:
  - MUST NOT break existing single-OIDC configuration
  - MUST NOT add external dependencies
  - MUST NOT require provider-specific SDKs

───────────────────────────────────────────────────────────
HARD BOUNDARIES
  HB-01: Existing auth tests pass unchanged
  HB-02: go vet clean

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-18/TASK-01 — Multi-Tenant OIDC
  Files in scope:
    - internal/auth/oidc_multi.go (CREATE — MultiProvider)
    - internal/auth/oidc_multi_test.go (CREATE)
    - internal/config/config.go (MODIFY — OIDCProviders config)
  Acceptance Criteria:
    AC-01-01: Multiple OIDC providers configurable
    AC-01-02: Token validated against correct provider by issuer
    AC-01-03: Backward-compatible with single OIDC
