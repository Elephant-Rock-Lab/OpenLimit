# Release Process

This document describes the release process for OpenLimit.

## Pre-Release

- [ ] Update `CHANGELOG.md` — ensure the release date is correct and all entries are complete.
- [ ] Update `deploy/helm/openlimit/Chart.yaml` — bump `version` and `appVersion` to match the release.
- [ ] Update `deploy/helm/openlimit/values.yaml` — verify image tag defaults are correct.
- [ ] Verify `go.mod` Go version matches CI and is the intended minimum version.
- [ ] Run full test suite: `go test ./... -count=1`
- [ ] Validate OpenAPI spec: `npx @redocly/cli lint docs/openapi/admin-api.yaml`
- [ ] Spot-check 5 admin endpoints manually or via integration test:
  1. `GET /admin/models`
  2. `POST /admin/quickstart`
  3. `GET /admin/cache/stats`
  4. `GET /admin/health`
  5. `GET /admin/budgets`
- [ ] Verify README links are valid and doc pages render correctly.
- [ ] Confirm all AIV batch reports are archived under `docs/aiv/`.

## Release

- [ ] Create an annotated git tag: `git tag -a v1.0.0 -m "Release v1.0.0"`
- [ ] Push the tag: `git push origin v1.0.0`
- [ ] Build release binaries: `make build`
- [ ] Build and push Docker image:
  ```bash
  docker build -t openlimit/openlimit:1.0.0 .
  docker push openlimit/openlimit:1.0.0
  ```
- [ ] Create a GitHub Release with:
  - Tag: `v1.0.0`
  - Title: `v1.0.0`
  - Body: Copy the changelog entry for this version.
  - Attachments: Built binaries (optional)

## Post-Release

- [ ] Announce the release (GitHub Discussions, Discord, etc.).
- [ ] Add a new `[Unreleased]` section at the top of `CHANGELOG.md`:
  ```markdown
  ## [Unreleased]

  ### Added
  ### Changed
  ### Deprecated
  ### Removed
  ### Fixed
  ### Security
  ```
- [ ] Update any dependent documentation or downstream references.
