BATCH BLUEPRINT
═══════════════════════════════════════════════════════════

Batch ID:                 BATCH-PL1
Blueprint Version:        1.0
Cycle Mode:               STANDARD
Lead Programmer:          Craft Agent (Lead)
Date Issued:              2026-05-03

───────────────────────────────────────────────────────────
BATCH GOAL
───────────────────────────────────────────────────────────
Polish the v1.1.0 release: update roadmap to reflect actual state,
enhance CI/CD with release automation, update README for v1.1 features,
and add docker-compose profiles for new features (Redis cluster, dashboard).

───────────────────────────────────────────────────────────
TASK LIST
───────────────────────────────────────────────────────────

TASK-01: ROADMAP.md — Update to reflect v1.1.0 completion
  - Mark v1.0 and v1.1 batches as ✅ Complete
  - Update version matrix
  - Update stats (LOC, tests, packages, providers)
  - Remove stale TD items

TASK-02: README.md — Add v1.1 features
  - Add new providers (Bedrock, Vertex, Groq, Cohere, Mistral) to feature table
  - Add v1.1 features: config hot-reload, admin dashboard, prompt management,
    webhook mTLS, provider health, Redis cluster
  - Update architecture diagram with new packages
  - Update provider list in routing feature

TASK-03: CI/CD — Add release workflow and Docker multi-arch build
  - Add .github/workflows/release.yml: triggered on v* tags
  - Goreleaser or manual Docker build + push to GHCR
  - Go build with -ldflags for version injection

TASK-04: Docker — Update docker-compose with v1.1 profiles
  - Add redis-cluster profile using redis/redis-stack-server
  - Add dashboard note to gateway service
  - Add pgvector to postgres image for semantic cache

TASK-05: Makefile — Add version-aware build targets
  - Add `make release` with ldflags
  - Add `make docker-push`
  - Add `make test-integration` for -tags=integration

───────────────────────────────────────────────────────────
LINT COMMAND
───────────────────────────────────────────────────────────
  go vet ./...

───────────────────────────────────────────────────────────
HARD BOUNDARIES
───────────────────────────────────────────────────────────
  HB-01: No production Go code changes (docs/CI/Makefile only)
  HB-02: Existing tests must continue to pass

───────────────────────────────────────────────────────────
BATCH-LEVEL ACCEPTANCE CRITERIA
───────────────────────────────────────────────────────────
  BAC-01: ROADMAP.md reflects v1.1.0 as released
  BAC-02: README.md lists all v1.1 features
  BAC-03: CI has lint + test + build + release workflows
  BAC-04: docker-compose.yml has Redis cluster and pgvector
  BAC-05: Makefile has release/docker-push/test-integration targets
  BAC-06: go vet passes, full test suite green
