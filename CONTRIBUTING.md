# Contributing to OpenLimit

Thank you for your interest in contributing to OpenLimit! This guide covers everything you need to get started.

## Development Setup

### Prerequisites

- **Go 1.25+** — Install from [golang.org](https://golang.org/dl/)
- **Docker** — For running Postgres with pgvector (test dependency)
- **Make** — Build automation

### Quick Start

```bash
# Clone the repository
git clone https://github.com/nicholasgasior/openlimit.git
cd openlimit

# Run the gateway locally
make run

# Run the full test suite
make test

# Build binaries
make build
```

### Common Make Targets

| Target        | Description                          |
|---------------|--------------------------------------|
| `make run`    | Start the gateway locally            |
| `make test`   | Run all tests                        |
| `make build`  | Build all binaries                   |
| `make lint`   | Run go vet and gofmt checks          |
| `make docker` | Build Docker image                   |

## AIV Framework

OpenLimit is developed using the **AIV (AI-Verified Implementation) framework v5.1**. This framework ensures every change is planned, verified, and documented through a structured batch-and-task system.

For details, see [`AIV_FRAMEWORK_v5.1.md`](docs/aiv/AIV_FRAMEWORK_v5.1.md).

## How to Propose Changes

1. **Open an Issue** — Describe the problem or enhancement with clear acceptance criteria.
2. **Blueprint Review** — Maintainers will create or review a batch blueprint that maps the work.
3. **Implementation** — Changes are implemented following the task sequence in the blueprint.
4. **Verification** — All tests must pass. The CI pipeline validates build, lint, and test.
5. **Merge** — A maintainer reviews and merges the PR.

### Pull Request Checklist

- [ ] All tests pass (`make test`)
- [ ] Code is formatted (`gofmt -w .`)
- [ ] No vet issues (`go vet ./...`)
- [ ] CHANGELOG.md updated if applicable
- [ ] Documentation updated if applicable

## Code Style

- Run `gofmt` on all Go files before committing.
- Run `go vet ./...` and resolve all warnings.
- Follow standard [Go code review conventions](https://go.dev/wiki/CodeReviewComments).
- Package names should be lowercase, single-word, and descriptive.
- Error messages should not be capitalized or end with punctuation.

## Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run a specific package
go test ./internal/gateway/...

# Run with race detector
go test -race ./...
```

Tests that require Postgres will use the Docker service container configured in CI. For local development, ensure a Postgres instance with pgvector is available or mock-based tests will be used.

## Reporting Issues

When filing a bug report, please include:

- Go version (`go version`)
- OpenLimit version or commit hash
- Steps to reproduce
- Expected vs. actual behavior
- Relevant log output or error messages

## License

By contributing to OpenLimit, you agree that your contributions will be licensed under the [MIT License](LICENSE).
