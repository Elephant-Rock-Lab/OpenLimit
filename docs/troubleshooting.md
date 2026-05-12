# Troubleshooting Guide

Common errors, their causes, and solutions. For setup help, see [Getting Started](getting-started.md). For API details, see [API Reference](api-reference.md).

---

## Authentication Errors

### Missing authorization header

| Field | Value |
|---|---|
| **Symptom** | `{"error":{"message":"missing or invalid authorization header","type":"auth_error"}}` with HTTP 401 |
| **Cause** | The request does not include an `Authorization: Bearer <key>` header, or the header value is malformed. |
| **Solution** | Add the header: `Authorization: Bearer gw-your-key-here`. Ensure there is a space after `Bearer`. Keys must start with `gw-`. |

### Invalid virtual key

| Field | Value |
|---|---|
| **Symptom** | `{"error":{"message":"invalid virtual key","type":"auth_error"}}` with HTTP 401 |
| **Cause** | The key does not exist, has been revoked, or does not match any active key in the database. |
| **Solution** | Verify the key value is correct (check for typos, truncation). Create a new key via the dashboard or `POST /admin/keys`. If the key was revoked, generate a replacement. See [Getting Started §5](getting-started.md) for key creation. |

### Virtual key has expired

| Field | Value |
|---|---|
| **Symptom** | `{"error":{"message":"virtual key has expired","type":"auth_error"}}` with HTTP 401 |
| **Cause** | The key was created with an `expires_at` timestamp that has passed. |
| **Solution** | Create a new key. Set a longer expiry or omit `expires_at` for a key that never expires. |

### Budget exceeded

| Field | Value |
|---|---|
| **Symptom** | `{"error":{"message":"budget exceeded: $X.XX monthly limit reached","type":"auth_error"}}` with HTTP 401 |
| **Cause** | The total spend for this key in the current billing period has reached the `budget_limit_usd` configured for the key. |
| **Solution** | Increase the budget via `PATCH /admin/keys/{id}` with a higher `budget_limit_usd`, or wait for the next billing period. Check usage with `GET /admin/usage?project_id=<id>`. |

---

## Provider Errors

### Invalid provider API key

| Field | Value |
|---|---|
| **Symptom** | HTTP 401 or 403 from upstream provider; gateway logs show `"provider returned error"`. |
| **Cause** | The provider API key (e.g., `OPENAI_API_KEY`) is missing, expired, or invalid. |
| **Solution** | Check the environment variable or `value` field in `gateway.yaml` under the provider's `keys` section. Verify the key is active in the provider's dashboard. **Never hardcode keys in config files committed to version control** — use `env:` to reference environment variables. |

### Model not found

| Field | Value |
|---|---|
| **Symptom** | `{"error":{"message":"model not found","type":"invalid_request_error"}}` with HTTP 404 |
| **Cause** | The requested model name is not defined in the `models` section of `gateway.yaml`. |
| **Solution** | Check the model name in your request matches a key in the `models` block. Use the `available_models` field in the error response to see valid model names. See [Configuration](configuration.md) for model setup. |

### Provider timeout

| Field | Value |
|---|---|
| **Symptom** | Request takes a long time, then returns `{"error":{"message":"provider timeout","type":"server_error"}}` with HTTP 504. |
| **Cause** | The upstream provider did not respond within the configured timeout. Network issues or provider outages can cause this. |
| **Solution** | Increase `provider.timeout` in `gateway.yaml` (default: 30s). Check the provider's status page. If using Azure or Vertex, verify regional endpoints are reachable. Configure fallback models for automatic failover. |

---

## Configuration Errors

### Invalid YAML config

| Field | Value |
|---|---|
| **Symptom** | Gateway fails to start with error: `invalid config: 1. ... 2. ...` |
| **Cause** | The `gateway.yaml` file has syntax errors, unknown fields, or values outside allowed ranges. |
| **Solution** | Read the numbered error messages — each identifies a specific field and constraint. Common issues: `server.port` outside 1–65535, missing `base_url` for `openai-compatible` providers, `routing.region_strategy` with invalid value. See [Configuration](configuration.md) for the full schema. |

### Unsupported provider type

| Field | Value |
|---|---|
| **Symptom** | `provider "X" has unsupported type "Y"` in startup error |
| **Cause** | The `type` field for a provider is not one of the supported values. |
| **Solution** | Use one of: `openai`, `openai-compatible`, `anthropic`, `gemini`, `azure-openai`, `bedrock`, `vertex`, `groq`, `cohere`, `mistral`. See [Configuration §Providers](configuration.md) for details. |

### Missing database URL

| Field | Value |
|---|---|
| **Symptom** | `auth is enabled but database.url is not configured` or `admin is enabled but database.url is not configured` |
| **Cause** | Auth or admin features require Postgres, but `database.url` is empty in the config. |
| **Solution** | Add a `database.url` to `gateway.yaml`: `postgres://user:pass@host:5432/dbname?sslmode=disable`. Or disable `auth.enabled` and `admin.enabled` if you don't need these features. |

---

## Dashboard Errors

### Connection error

| Field | Value |
|---|---|
| **Symptom** | Dashboard shows "Connection Error" page with message: "Cannot reach the gateway at `<origin>`. Is it running?" |
| **Cause** | The browser cannot reach the gateway server. The gateway is not running, or there is a network/firewall issue. |
| **Solution** | Ensure the gateway is running (`curl http://localhost:8080/health`). Check the port matches `server.port` in your config. If using Docker, ensure the port is published (`-p 8080:8080`). |

### Authentication cancelled

| Field | Value |
|---|---|
| **Symptom** | Dashboard shows "Authentication Required" with message: "Authentication was cancelled." |
| **Cause** | You clicked "Cancel" on the admin token prompt. |
| **Solution** | Click "Retry" and enter your admin bearer token. The token is set in `admin.bearer_token` in `gateway.yaml`. |

### Blank page / no data

| Field | Value |
|---|---|
| **Symptom** | Dashboard loads but panels show empty data. |
| **Cause** | No projects, keys, or usage data exist yet. This is expected on a fresh install. |
| **Solution** | Use the first-run overlay (appears automatically) to create a project and key. Or use `POST /admin/quickstart` to create both in one step. See [Getting Started §5](getting-started.md). |

---

## SDK Errors

### TypeError: Failed to fetch

| Field | Value |
|---|---|
| **Symptom** | JavaScript/TypeScript SDK throws `TypeError: Failed to fetch` |
| **Cause** | The SDK cannot reach the gateway. Common in Node.js when the gateway uses a self-signed TLS certificate, or the `baseURL` is wrong. |
| **Solution** | Verify the `baseURL` points to the gateway (default: `http://localhost:8080/v1`). For self-signed certs, set `NODE_TLS_REJECT_UNAUTHORIZED=0` in development only. Ensure the gateway is running and accessible. |

### Timeout errors

| Field | Value |
|---|---|
| **Symptom** | SDK request times out, often on large or streaming requests |
| **Cause** | The default SDK timeout is shorter than the gateway's response time, especially for complex multi-turn agent requests or MCP tool calls. |
| **Solution** | Increase the SDK timeout. For the TypeScript SDK: `new OpenLimit({ timeout: 60000 })`. For long-running agent workflows, consider setting `timeout: 120000` or higher. |

### Rate limit exceeded

| Field | Value |
|---|---|
| **Symptom** | HTTP 429 with `rate_limit_exceeded` error |
| **Cause** | The virtual key's `rpm_limit` or `tpm_limit` has been reached for the current window. |
| **Solution** | Reduce request frequency, or increase the key's limits via `PATCH /admin/keys/{id}`. Use exponential backoff with jitter in your SDK client. |
