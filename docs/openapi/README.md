# OpenLimit Admin API — OpenAPI Specification

This directory contains the hand-authored OpenAPI 3.0 specification for the OpenLimit Admin API.

## File

| File | Description |
|---|---|
| `admin-api.yaml` | OpenAPI 3.0.3 spec covering all admin endpoints |

## Usage

### Swagger Editor (online)

1. Open [Swagger Editor](https://editor.swagger.io/)
2. Paste the contents of `admin-api.yaml` into the editor
3. The rendered documentation appears in the right panel

### Redoc

```bash
npx redoc-cli bundle admin-api.yaml -o admin-api.html
open admin-api.html
```

Or serve directly:

```bash
npx redoc-cli serve admin-api.yaml --watch
```

### Swagger UI (Docker)

```bash
docker run -p 8081:8080 \
  -e SWAGGER_JSON=/spec/admin-api.yaml \
  -v $(pwd)/admin-api.yaml:/spec/admin-api.yaml \
  swaggerapi/swagger-ui
```

Open http://localhost:8081.

### SDK Generation

Generate typed client SDKs from the spec:

**Go:**
```bash
npx openapi-generator-cli generate \
  -i admin-api.yaml \
  -g go \
  -o ./sdk/go
```

**TypeScript (fetch):**
```bash
npx openapi-generator-cli generate \
  -i admin-api.yaml \
  -g typescript-fetch \
  -o ./sdk/ts
```

**Python:**
```bash
npx openapi-generator-cli generate \
  -i admin-api.yaml \
  -g python \
  -o ./sdk/python
```

### Validation

Validate the spec for syntax and schema errors:

```bash
npx openapi-generator-cli validate -i admin-api.yaml
```

Or with `swagger-cli`:

```bash
npx swagger-cli validate admin-api.yaml
```

## Authentication

All endpoints require a Bearer token in the `Authorization` header:

```
Authorization: Bearer <token>
```

The token can be either:
- **Static bearer token** — configured via `admin.bearer_token` or `ADMIN_TOKEN` env var
- **OIDC JWT** — when `admin.oidc.enabled: true`, any valid JWT from the configured IdP

## Endpoint Coverage

The spec covers all 15 admin endpoint patterns registered in `internal/admin/handler.go`:

| Method | Path | Operation |
|---|---|---|
| POST | `/admin/projects` | `createProject` |
| GET | `/admin/projects` | `listProjects` |
| DELETE | `/admin/projects/{id}` | `deleteProject` |
| POST | `/admin/keys` | `createKey` |
| GET | `/admin/keys` | `listKeys` |
| DELETE | `/admin/keys/{id}` | `revokeKey` |
| GET | `/admin/usage` | `getUsage` |
| GET | `/admin/usage/summary` | `getUsageSummary` |
| GET | `/admin/audit` | `queryAuditLogs` |
| POST | `/admin/users` | `createUser` |
| GET | `/admin/users` | `listUsers` |
| DELETE | `/admin/users/{id}` | `deleteUser` |
| PUT | `/admin/users/{id}/role` | `updateUserRole` |
| GET | `/admin/tools` | `getMCPTools` |
| GET | `/admin/mcp/tools` | `getMCPServerTools` |

## Maintenance

This spec is **hand-authored** and is the documentation source of truth. When admin endpoints change:

1. Update `admin-api.yaml` to reflect the change
2. Run validation to confirm the spec is still valid
3. Re-generate SDKs if applicable
