# API Reference

**What you'll learn:** How to use the OpenLimit API — link to the OpenAPI spec, the complete error reference, and operational response headers.

---

## OpenAPI Specification

The admin API is documented in OpenAPI 3.0.3 format at:

```
docs/openapi/admin-api.yaml
```

This spec covers all 15 admin API endpoints with request/response schemas, authentication requirements, and error responses.

---

## Chat Completions API

OpenLimit exposes an OpenAI-compatible API at `POST /v1/chat/completions`. Any client that works with OpenAI will work with OpenLimit — just change the base URL and API key.

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer gw-your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "fast",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

### Supported parameters

| Parameter | Description |
|---|---|
| `model` | Logical model alias (required) |
| `messages` | Array of message objects (required) |
| `stream` | `true` for SSE streaming |
| `temperature` | Sampling temperature |
| `max_tokens` | Maximum completion tokens |
| `tools` | Array of tool definitions (merged with MCP tools) |
| `tool_choice` | `"none"`, `"auto"`, `"required"`, or specific tool |

### Response headers

| Header | Description | Example |
|---|---|---|
| `X-Request-ID` | Correlation ID for every request | `corr-a1b2c3d4` |
| `X-Provider` | Provider that handled the request | `openai` |
| `X-Cache` | Cache status | `HIT` or `MISS` |
| `X-Cost-USD` | Cost of the request in USD | `0.00123` |
| `traceparent` | W3C trace correlation (when tracing enabled) | `00-4bf92f...-00f067...-01` |

---

## Model Listing

```bash
curl http://localhost:8080/v1/models
```

Returns the list of logical model aliases configured in `gateway.yaml`.

---

## Health Endpoints

### Liveness

```bash
curl http://localhost:8080/health
```

```json
{"status":"ok","version":"dev","timestamp":"..."}
```

### Readiness

```bash
curl http://localhost:8080/ready
```

Returns detailed status including database, Redis, KMS, and OIDC connectivity:

```json
{
  "status": "ok",
  "database": true,
  "redis": true,
  "kms.ready": true,
  "oidc.ready": true
}
```

---

## Admin API

All admin endpoints require `Authorization: Bearer <admin-token>` unless OIDC is configured.

| Endpoint | Method | Description |
|---|---|---|
| `/admin/quickstart` | POST | One-step project + key creation |
| `/admin/projects` | POST | Create project `{name}` |
| `/admin/projects` | GET | List all projects |
| `/admin/projects/{id}` | DELETE | Delete project (cascades keys) |
| `/admin/keys` | POST | Create virtual key |
| `/admin/keys?project_id=` | GET | List keys (filterable) |
| `/admin/keys/{id}` | DELETE | Revoke key |
| `/admin/tools` | GET | List MCP client tools and server status |
| `/admin/mcp/tools` | GET | List MCP server exposed tools |
| `/admin/usage` | GET | Query usage logs |
| `/admin/usage/summary` | GET | Aggregated usage summary |
| `/admin/audit` | GET | Query audit event logs (with filters) |
| `/admin/users` | POST | Create admin user (RBAC required) |
| `/admin/users` | GET | List admin users (RBAC required) |
| `/admin/users/{id}` | DELETE | Soft-delete admin user (RBAC required) |
| `/admin/users/{id}/role` | PUT | Update user role (RBAC required) |

---

## Error Reference

All error responses follow the OpenAI-compatible format with `type` (not `code`) for SDK compatibility:

```json
{
  "error": {
    "message": "Human-readable description",
    "type": "error_type",
    "details": {},
    "stage": "pipeline_stage",
    "request_id": "corr-123"
  }
}
```

The `details` and `stage` fields are optional (`omitempty`) and absent when unused.

### Error types

| HTTP Status | Type | Meaning | Example Message | Recommended Action |
|---|---|---|---|---|
| 400 | `invalid_request` | Malformed or missing request parameters | "model is required" | Check request body and required fields |
| 400 | `model_not_found` | Requested model alias does not exist in the registry | "model 'unknown' not found" | Verify model alias in gateway config |
| 400 | `tool_merge_error` | Failed to merge MCP tools into request | "tool name collision" | Check tool_prefix and conflict strategy |
| 400 | `method_not_allowed` | HTTP method not supported for endpoint | "method not allowed" | Use correct HTTP method |
| 401 | `auth_error` | Invalid or missing virtual key | "invalid api key" | Check Authorization header |
| 401 | `unauthorized` | Admin auth failure (missing/invalid token) | "missing authorization header" | Provide valid Bearer token |
| 403 | `forbidden` | Insufficient RBAC role permissions | "insufficient permissions" | Request role elevation from admin |
| 403 | `residency_denied` | No providers in requested data residency region | "no providers in region 'eu'" | Configure region with matching residency tag |
| 403 | `model_not_allowed` | Virtual key does not have access to this model | "model 'gpt-4' not allowed" | Update key's allowed_models list |
| 429 | `rate_limit_exceeded` | Per-key RPM/TPM limit reached | "rate limit exceeded" | Retry after cooldown or increase key RPM/TPM |
| 429 | `budget_exceeded` | Per-key spend cap reached | "budget exceeded" | Increase budget_limit_usd or wait for reset |
| 429 | `guardrail_block` | Content blocked by guardrail pipeline | "PII detected in request" | Modify content or adjust guardrail config |
| 500 | `mcp_error` | MCP tool execution failed | "tool call timeout" | Check MCP server connectivity |
| 500 | `streaming_unsupported` | Response writer does not support streaming | "response writer does not support streaming" | Check reverse proxy configuration |
| 502 | `provider_error` | Upstream provider returned an error | "openai: connection refused" | Check provider availability and API key |

### Guardrail block stages

When `type` is `guardrail_block`, the `stage` field identifies which guardrail triggered:

| Stage | Direction | Description |
|---|---|---|
| `pii` | input/output | PII detection (credit card, SSN, email, phone, IP) |
| `regex` | input/output | Regex pattern matching |
| `keyword` | input/output | Keyword blocklist (case-insensitive) |
| `length` | input/output | Character count limits |
| `webhook` | input/output | External moderation webhook |
| `json_schema` | output only | JSON Schema validation |

### Error metric

All gateway errors are tracked by the `gateway_errors_total` Prometheus counter:

```
gateway_errors_total{type="rate_limit_exceeded", source="direct"} 42
```

**Source label values:** `direct` (chat completions), `admin` (admin API), `mcp` (MCP tool calls), `a2a` (A2A protocol).

---

## Next steps

- **[Getting Started](getting-started.md)** — Make your first request
- **[Governance](governance.md)** — Create and manage virtual keys
- **[Agent Protocols](agent-protocols.md)** — A2A JSON-RPC methods
- **[Observability](observability.md)** — Track errors with Prometheus
