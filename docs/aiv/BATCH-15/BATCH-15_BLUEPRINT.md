BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-15
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-08

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Create a Python SDK that mirrors the TypeScript SDK API surface:
chat completions (streaming + non-streaming), embeddings, models,
health checks. Uses only stdlib (http.client / json) — zero deps.

───────────────────────────────────────────────────────────
SCOPE STATEMENT
───────────────────────────────────────────────────────────
What the code MUST do:
  - Provide OpenLimitClient class with configurable base URL and API key
  - Support chat completions (streaming via generator, non-streaming)
  - Support embeddings creation
  - Support models listing
  - Support health checks
  - Full type annotations (Python 3.10+)
  - pyproject.toml with setuptools build
  - README with usage examples

What the code MUST NOT do:
  - MUST NOT require third-party dependencies (no requests, httpx, etc.)
  - MUST NOT bundle additional files beyond the package

───────────────────────────────────────────────────────────
HARD BOUNDARIES
  HB-01: Zero third-party runtime dependencies
  HB-02: Python 3.10+ type hints

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: BATCH-15/TASK-01 — Python SDK Core Implementation
  Files in scope:
    - sdks/python/pyproject.toml (CREATE)
    - sdks/python/src/openlimit/__init__.py (CREATE)
    - sdks/python/src/openlimit/client.py (CREATE)
    - sdks/python/src/openlimit/types.py (CREATE)
    - sdks/python/src/openlimit/errors.py (CREATE)
    - sdks/python/src/openlimit/streaming.py (CREATE)
    - sdks/python/tests/test_client.py (CREATE)
    - sdks/python/README.md (CREATE)
  Acceptance Criteria:
    AC-01-01: SDK installs with pip install -e .
    AC-01-02: All unit tests pass
    AC-01-03: Zero third-party dependencies
    AC-01-04: README complete with examples
