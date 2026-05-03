# Governance

**What you'll learn:** How to control access to AI providers using virtual keys, projects, budgets, rate limits, guardrails, RBAC, OIDC SSO, and audit logs.

All governance features are opt-in. The gateway works as an open proxy when `auth.enabled: false`.

---

## Virtual API Keys

Virtual keys (`gw-` prefix) decouple your provider API keys from consumer access. Provider keys never leave the gateway — consumers authenticate with virtual keys that have scoped permissions.

### Key properties

| Property | Description |
|---|---|
| `name` | Human-readable identifier |
| `project_id` | Owning project |
| `allowed_models` | List of logical model names (glob patterns: `gpt-4*`) |
| `allowed_providers` | List of provider names the key can use |
| `allowed_tools` | List of MCP tool patterns (`weather.*`, `github.create_issue`) |
| `rpm_limit` | Requests per minute |
| `tpm_limit` | Tokens per minute |
| `budget_limit_usd` | Spend cap |
| `budget_period` | `daily` or `monthly` |
| `allow_mcp_server` | Expose this key as an MCP tool |
| `mcp_tool_name` | Custom name for MCP tool (optional) |

### Key management

```bash
# Create a key
curl -X POST http://localhost:8080/admin/keys \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "project_id": "proj_1",
    "name": "dev-key",
    "allowed_models": ["fast", "claude"],
    "rpm_limit": 60,
    "budget_limit_usd": 50.0,
    "budget_period": "monthly"
  }'

# List keys
curl "http://localhost:8080/admin/keys?project_id=proj_1" \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Revoke a key
curl -X DELETE http://localhost:8080/admin/keys/<key-id> \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

The full key value is returned only once during creation. Keys are bcrypt-hashed at rest.

### Quickstart endpoint

Create a project and key in a single step:

```bash
curl -X POST http://localhost:8080/admin/quickstart \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json"
```

Returns `project_id`, `key`, `key_name`, and `project_name`. Optionally pass `{"rpm_limit": 10}` to set a custom rate limit.

---

## Projects

Projects group keys for multi-tenancy. Each key belongs to one project. Deleting a project cascades to all its keys.

```bash
# Create
curl -X POST http://localhost:8080/admin/projects \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"name": "my-app"}'

# List
curl http://localhost:8080/admin/projects \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Delete (cascades keys)
curl -X DELETE http://localhost:8080/admin/projects/<id> \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

---

## Rate Limiting

Per-key RPM (requests per minute) and TPM (tokens per minute) limits using token bucket algorithm.

- **Without Redis:** Local token bucket — limits are per-instance. Good for single-gateway deployments.
- **With Redis:** Shared sliding window — accurate across multiple pods. See [Configuration](configuration.md#redis).

Rate-limited requests receive HTTP 429 with `type: rate_limit_exceeded`:

```json
{
  "error": {
    "type": "rate_limit_exceeded",
    "message": "rate limit exceeded",
    "request_id": "corr-123"
  }
}
```

---

## Budgets

Per-key spend caps with daily or monthly reset periods. Cost is calculated using the `billing.prices` configuration.

Budget-exceeded requests receive HTTP 403 with `type: budget_exceeded`:

```json
{
  "error": {
    "type": "budget_exceeded",
    "message": "budget exceeded",
    "request_id": "corr-456"
  }
}
```

---

## Guardrails

Configurable content safety pipeline with pass/block/redact actions. Guardrails run per-model with input/output toggles.

### Pipeline order

1. **Auth + rate limit** — verify key and quotas
2. **Input guardrails** — inspect request content
3. **Cache lookup** — check exact and semantic cache
4. **Provider call** — forward to upstream
5. **Output guardrails** — inspect response content
6. **Cache store** — save response

First `Block` short-circuits the pipeline. Redacted content is cached in redacted form.

### Guardrail stages

| Stage | Direction | Description |
|---|---|---|
| `pii` | input/output | Detects SSN, credit card, email, phone, IP. Configurable action: `redact` or `block` |
| `regex` | input/output | Arbitrary regex patterns with block/redact/log actions |
| `keyword` | input/output | Basic keyword blocklist (case-insensitive substring match) |
| `length` | input/output | Min/max character limits |
| `webhook` | input/output | HTTP POST to external service. 250ms default timeout |
| `json_schema` | output only | Validates JSON response against a JSON Schema |

### Configuration

```yaml
guardrails:
  enabled: true
  input:
    - type: pii
      config:
        types: [credit_card, ssn, email, phone]
        action: redact
    - type: keyword
      config:
        blocklist: ["ignore previous instructions"]
        action: block
    - type: regex
      config:
        patterns: ["\\b\\d{3}-\\d{2}-\\d{4}\\b"]
        action: block
    - type: length
      config:
        max_chars: 10000
        action: block
  output:
    - type: webhook
      config:
        url: "http://localhost:8888/moderate"
        timeout_ms: 250
        block_on_error: true
        block_on_timeout: true
    - type: json_schema
      config:
        schema: '{"type":"object","required":["answer"]}'
  models:
    fast:
      input: true
      output: true
    smart:
      input: true
      output: false
```

### Custom webhook guardrails

The webhook stage sends a POST to an external service with the request or response content. The service returns `{ "allowed": true/false }`:

- `block_on_error: true` — block if the webhook returns an error or times out
- `block_on_timeout: true` — block if the request exceeds `timeout_ms`
- Default timeout: 250ms

### Streaming behavior

- **Input guardrails** apply to streaming requests (content is available before the provider call)
- **Output guardrails** cannot inspect streaming responses — they only apply to non-streaming requests

### Guardrail block response

```json
{
  "error": {
    "type": "guardrail_block",
    "message": "PII detected in request",
    "stage": "pii",
    "request_id": "..."
  }
}
```

---

## RBAC

Three hardcoded roles for admin API access. RBAC is fully opt-in (`admin.rbac_enabled: false` by default).

| Role | Create/Delete Projects | Create/Revoke Keys | View Usage/Audit | Manage Users |
|---|---|---|---|---|
| `admin` | ✅ | ✅ | ✅ | ✅ |
| `editor` | ❌ | ✅ | ✅ | ❌ |
| `viewer` | ❌ | ❌ | ✅ | ❌ |

```yaml
admin:
  enabled: true
  bearer_token: "your-admin-token"
  rbac_enabled: true
```

### User management

```bash
# Create user
curl -X POST http://localhost:8080/admin/users \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"subject":"oidc-user-123","email":"admin@example.com","role":"admin"}'

# List users
curl http://localhost:8080/admin/users \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Update role
curl -X PUT http://localhost:8080/admin/users/<id>/role \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"role":"editor"}'

# Delete user (soft-delete)
curl -X DELETE http://localhost:8080/admin/users/<id> \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

The static `bearer_token` serves as a bootstrap mechanism for creating the first admin user.

---

## OIDC SSO

OpenID Connect authentication via JWT validation. Works with Okta, Azure AD, Keycloak, and Google Workspace.

```yaml
admin:
  enabled: true
  bearer_token: "your-admin-token"    # keep for bootstrap
  rbac_enabled: true
  oidc:
    enabled: true
    issuer: "https://dev-123.okta.com/oauth2/default"
    audience: "openlimit-admin"
    default_role: "viewer"
```

The gateway validates JWT access tokens — it does not implement the OAuth2 authorization code flow. The admin UI or CLI obtains the token from the IdP and sends it as a `Bearer` token.

**Lookup order for OIDC users:** `sub` claim → `email` claim → auto-provision with `default_role`.

**Known behavior:** Role changes take effect on the next request, not the next token. The gateway looks up the role from the database on every request.

---

## Audit Logs

All admin mutations and security events are recorded in the `audit_logs` Postgres table. Records are immutable.

### Query audit logs

```bash
# All recent events
curl -s http://localhost:8080/admin/audit \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Filter by event type
curl -s "http://localhost:8080/admin/audit?event_type=key.create&limit=50" \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Filter by time range
curl -s "http://localhost:8080/admin/audit?since=2025-01-01T00:00:00Z&limit=200" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

### Audited event types

| Event Type | Trigger |
|---|---|
| `project.create` | Admin API |
| `project.delete` | Admin API |
| `key.create` | Admin API |
| `key.revoke` | Admin API |
| `provider_key.decrypt` | System (startup) |
| `auth.failure` | Failed authentication |
| `authorization.denied` | Insufficient RBAC role |
| `guardrail.block` | Content blocked |
| `user.create` | Admin API |
| `user.delete` | Admin API |
| `user.update_role` | Admin API |
| `oidc.auth_success` | Successful OIDC validation |
| `oidc.auth_failure` | Failed OIDC validation |

### Audit log schema

| Column | Type | Description |
|---|---|---|
| `id` | BIGSERIAL | Monotonic sequence number |
| `timestamp` | TIMESTAMPTZ | Event time (UTC) |
| `event_type` | TEXT | Event category |
| `actor` | TEXT | Who performed the action |
| `action` | TEXT | What was done |
| `resource` | TEXT | What was affected |
| `outcome` | TEXT | `success`, `denied`, `error` |
| `request_id` | TEXT | Correlation ID |
| `metadata` | JSONB | Free-form event details |

---

## Next steps

- **[Configuration](configuration.md)** — Full `gateway.yaml` reference
- **[Agent Protocols](agent-protocols.md)** — Tool governance with MCP
- **[Security](security.md)** — Encrypt provider keys with KMS
- **[API Reference](api-reference.md)** — Admin API endpoints and error types
