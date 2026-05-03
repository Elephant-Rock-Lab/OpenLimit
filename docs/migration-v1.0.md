# Migration to v1.0

**What you'll learn:** How to upgrade OpenLimit from Phase 5B/6F to v1.0.0, including config changes, behavior changes, and Go API changes.

---

## Overview

v1.0.0 is a major milestone that introduces:

- **Unified governance** — All entry points (direct API, MCP server, A2A) now enforce the same governance pipeline
- **New provider adapters** — Google Gemini and Azure OpenAI
- **Enriched error responses** — Actionable messages with structured details
- **Operational headers** — `X-Provider`, `X-Cache`, `X-Cost-USD` on every response
- **OpenAPI spec** — Admin API fully documented in OpenAPI 3.0.3

Most deployments can upgrade with configuration changes only. Go API changes only affect teams that import OpenLimit as a library.

---

## Config Changes

### New provider types

Two new provider types are available in the `providers` section:

#### Google Gemini

```yaml
providers:
  gemini:
    type: gemini                           # NEW
    keys:
      - id: main
        env: GEMINI_API_KEY
    gemini_model_map:                      # REQUIRED for gemini type
      fast: "gemini-2.0-flash"
      smart: "gemini-2.5-pro"
```

The `gemini_model_map` field is **required** for Gemini providers. It maps logical model aliases to Gemini model IDs.

#### Azure OpenAI

```yaml
providers:
  azure:
    type: azure-openai                     # NEW
    keys:
      - id: eastus
        env: AZURE_OPENAI_API_KEY
    azure_resource: "my-openai-resource"   # REQUIRED for azure-openai type
    azure_api_version: "2024-12-01-preview" # optional, has default
```

The `azure_resource` field is **required** for Azure OpenAI providers. The gateway constructs deployment URLs from this value.

### Validation

The gateway validates provider configuration at startup. Missing required fields will prevent startup with a clear error message:

- `gemini_model_map is required for gemini provider type`
- `azure_resource is required for azure-openai provider type`

---

## Behavior Changes

### 1. Governance enforced on all paths

**Before (Phase 5B/6F):** MCP server and A2A paths bypassed some governance controls (rate limits, budgets, caching, usage logging).

**After (v1.0.0):** All paths go through the unified `ExecuteGoverned` pipeline. This means:

- MCP tool calls (`tools/call`) now enforce virtual key rate limits and budgets
- A2A tasks now enforce rate limits, budgets, and guardrails
- Usage from MCP and A2A is now logged and counted toward budgets
- Caching applies to all paths (including MCP server and A2A)

**Action required:** Review your virtual key budgets and rate limits. MCP and A2A workloads that previously bypassed limits will now consume them. You may need to increase limits for keys used by agent protocols.

### 2. Enriched provider error messages

**Before:** Provider errors were passed through as raw upstream messages.

**After:** Provider errors are enriched with actionable context via `EnrichProviderError`:

```json
{
  "error": {
    "type": "provider_error",
    "message": "openai: connection refused — check that the provider is reachable and your API key is valid",
    "request_id": "corr-123"
  }
}
```

**Action required:** If you parse error messages in automation, update your patterns. The messages are more descriptive but the `type` field remains stable.

### 3. Admin errors include request_id

**Before:** Admin API error responses did not include a `request_id`.

**After:** All admin API errors include `request_id` for correlation with logs:

```json
{
  "error": {
    "type": "invalid_request",
    "message": "name is required",
    "request_id": "corr-456"
  }
}
```

**Action required:** None. This is an additive change.

### 4. New response headers

All chat completion responses now include:

| Header | Description |
|---|---|
| `X-Provider` | Which provider handled the request (e.g., `openai`) |
| `X-Cache` | `HIT` or `MISS` |
| `X-Cost-USD` | Cost of the request in USD |

These headers were added in v1.0.0 and are present on all responses regardless of configuration.

**Action required:** None. If your HTTP client rejects unknown headers, update it to ignore unknown headers.

### 5. POST /admin/quickstart

New endpoint for single-step project and key creation:

```bash
curl -X POST http://localhost:8080/admin/quickstart \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json"
```

Returns `project_id`, `key`, `key_name`, and `project_name`. Useful for onboarding scripts and development environments.

### 6. gateway_errors_total metric

New Prometheus counter that tracks all gateway errors by type and source:

```
gateway_errors_total{type="rate_limit_exceeded", source="direct"} 42
```

Source values: `direct`, `admin`, `mcp`, `a2a`.

---

## Go API Changes

These changes only affect teams that import OpenLimit as a Go library. If you only use the gateway binary, skip this section.

### 1. writeAdminError signature change

**Before:**

```go
func writeAdminError(w http.ResponseWriter, status int, typ string, message string)
```

**After:**

```go
func writeAdminError(w http.ResponseWriter, r *http.Request, status int, typ string, message string)
```

The `*http.Request` parameter is required to extract the `request_id` from the request context. Passing `nil` is safe (produces empty request_id, no panic).

### 2. executePlanSingle removed

**Before:** The `executePlanSingle()` function provided a shortcut for chat completions that bypassed the governance pipeline.

**After:** All calls go through `ExecuteGoverned()`. The ungoverned shortcut has been removed.

If you were calling `executePlanSingle()` directly, replace it with `ExecuteGoverned()`.

### 3. ExecuteForMCP identity parameter

**Before:**

```go
func (h *Handler) ExecuteForMCP(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)
```

**After:**

```go
func (h *Handler) ExecuteForMCP(ctx context.Context, req ChatCompletionRequest, identity any) (*ChatCompletionResponse, error)
```

The `identity` parameter passes the caller identity through the governance pipeline for proper rate limiting, budget enforcement, and usage attribution. This is the `IdentityProvider` interface implementation.

### 4. IdentityProvider interface

New interface for cross-package identity passing:

```go
type IdentityProvider interface {
    GetIdentity() any
}
```

Used by MCP server and A2A to pass caller identity into the governance pipeline.

### 5. GovernanceBlockedError interface

New error interface for detecting governance blocks in A2A:

```go
type GovernanceBlockedError interface {
    error
    GovernanceBlocked() bool
}
```

Enables A2A to distinguish governance rejections (rate limit, budget, guardrail) from provider errors.

---

## Upgrade Checklist

### Before upgrading

- [ ] Review virtual key budgets — MCP/A2A paths now consume limits
- [ ] Check for any code that calls `executePlanSingle()` (library users only)
- [ ] Verify error message parsing logic handles enriched messages

### Upgrade steps

1. Update the binary or Docker image to v1.0.0
2. Add any new provider configurations (Gemini, Azure OpenAI) if needed
3. Review virtual key limits and adjust if MCP/A2A workloads need more headroom
4. Restart the gateway
5. Verify health at `/health` and `/ready`
6. Test a sample request through each path (direct, MCP, A2A)

### After upgrading

- [ ] Verify governance is enforced on all paths (check `gateway_errors_total` by source)
- [ ] Confirm response headers include `X-Provider`, `X-Cache`, `X-Cost-USD`
- [ ] Review admin errors include `request_id`
- [ ] Import the Grafana dashboard update (includes new metrics)

---

## Rollback

If you need to roll back:

1. Revert to the previous binary or Docker image
2. Remove any new provider type configurations (Gemini, Azure OpenAI)
3. No database schema changes in v1.0.0 — rollback is safe

---

## Next steps

- **[Configuration](configuration.md)** — New provider types and all config options
- **[Governance](governance.md)** — Updated governance pipeline
- **[Agent Protocols](agent-protocols.md)** — Governance on MCP and A2A paths
- **[API Reference](api-reference.md)** — New error types and response headers
- **[Security](security.md)** — KMS encryption setup
