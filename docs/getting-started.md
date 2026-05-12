# Getting Started

**What you'll learn:** How to install OpenLimit, configure it with a provider, create your first virtual key, make a governed AI request, and add a guardrail — in under 5 minutes.

---

## Quick Setup (Recommended)

The fastest way to get OpenLimit running is with the interactive init wizard:

```bash
openlimit init
```

The wizard will guide you through:

1. **Provider configuration** — Enter your API key for OpenAI, Anthropic, Gemini, or any supported provider.
2. **Database setup** — Configure Postgres (or use the built-in SQLite for local testing).
3. **First key creation** — Generate a virtual key and project automatically.

That's it — the wizard generates your `gateway.yaml`, validates connectivity, and starts the gateway. You'll be making requests in under 60 seconds.

> **New to OpenLimit?** The init wizard handles everything below automatically. Skip to [Make your first request](#6-make-your-first-request) once the wizard completes.

---

## Advanced Setup

If you prefer full manual control or are deploying to production, follow the steps below.

---

## Prerequisites

- [Go 1.25+](https://go.dev/dl/) (or Docker)
- [Postgres](https://www.postgresql.org/) with the [pgvector extension](https://github.com/pgvector/pgvector) (optional, for semantic cache)
- At least one provider API key (OpenAI, Anthropic, Gemini, or Azure OpenAI)

---

## 1. Install

Clone and build:

```bash
git clone https://github.com/your-org/openlimit.git
cd openlimit
make build
```

Or use Docker:

```bash
docker pull ghcr.io/your-org/openlimit:latest
```

---

## 2. Configure

Create `configs/gateway.yaml` with your provider and database:

```yaml
server:
  host: "0.0.0.0"
  port: 8080

database:
  url: "postgres://openlimit:openlimit@localhost:5432/openlimit?sslmode=disable"

providers:
  openai:
    type: openai
    keys:
      - id: primary
        env: OPENAI_API_KEY
        weight: 1

models:
  fast:
    provider: openai
    model: gpt-4o-mini
  smart:
    provider: openai
    model: gpt-4o
    fallbacks:
      - fast
```

Set your provider key as an environment variable:

```bash
export OPENAI_API_KEY="sk-..."
```

See [Configuration](configuration.md) for the full `gateway.yaml` reference including all provider types, routing, caching, and Redis.

---

## 3. Start the gateway

### Local

```bash
make run
```

### Docker Compose (recommended)

```bash
docker compose -f deploy/docker-compose.yml up
```

This starts the gateway with Postgres. Add `--profile monitoring` for Prometheus + Grafana, or `--profile stateful` for Redis. See [Deployment](deployment.md) for all profiles.

---

## 4. Verify it's running

```bash
curl http://localhost:8080/health
```

```json
{"status":"ok","version":"dev","timestamp":"..."}
```

```bash
curl http://localhost:8080/ready
```

The `/ready` endpoint checks database connectivity and returns detailed status for each subsystem (KMS, OIDC, Redis).

---

## 5. Create a virtual key

Enable auth and admin in your config:

```yaml
auth:
  enabled: true

admin:
  enabled: true
  bearer_token: "your-admin-secret"
```

Restart the gateway, then create a project and key in one step:

```bash
curl -X POST http://localhost:8080/admin/quickstart \
  -H "Authorization: Bearer your-admin-secret" \
  -H "Content-Type: application/json"
```

Response:

```json
{
  "project_id": "proj_abc123",
  "key": "gw-a1b2c3d4e5f6...",
  "key_name": "quickstart",
  "project_name": "quickstart-2026-05-02"
}
```

Save the `key` value — it's shown only once.

For more control, create a project and key separately:

```bash
# Create project
curl -X POST http://localhost:8080/admin/projects \
  -H "Authorization: Bearer your-admin-secret" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-app"}'

# Create key with rate limits and budget
curl -X POST http://localhost:8080/admin/keys \
  -H "Authorization: Bearer your-admin-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "project_id": "<project-id>",
    "name": "dev-key",
    "allowed_models": ["fast", "smart"],
    "rpm_limit": 60,
    "budget_limit_usd": 50.0,
    "budget_period": "monthly"
  }'
```

See [Governance](governance.md) for full details on keys, projects, budgets, and rate limits.

---

## 6. Make your first request

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer gw-a1b2c3d4e5f6..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "fast",
    "messages": [{"role": "user", "content": "Say hello from OpenLimit"}]
  }'
```

Streaming:

```bash
curl -N http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer gw-a1b2c3d4e5f6..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "fast",
    "stream": true,
    "messages": [{"role": "user", "content": "Stream hello"}]
  }'
```

Response headers include:

| Header | Description |
|---|---|
| `X-Request-ID` | Correlation ID for every request |
| `X-Provider` | Which provider handled the request |
| `X-Cache` | `HIT` or `MISS` |
| `X-Cost-USD` | Cost of the request in USD |

---

## 7. Add your first guardrail

Guardrails are a content safety pipeline that inspects requests and responses. Add this to your config:

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
        blocklist: ["ignore previous instructions", "system prompt"]
        action: block
  output:
    - type: webhook
      config:
        url: "http://localhost:8888/moderate"
        timeout_ms: 250
        block_on_error: true
  models:
    fast:
      input: true
      output: true
```

Now requests containing PII will have it redacted, and known injection phrases will be blocked. See [Governance](governance.md) for all guardrail stages and configuration.

---

## 8. Check usage

```bash
# Per-project usage
curl "http://localhost:8080/admin/usage?project_id=<project-id>" \
  -H "Authorization: Bearer your-admin-secret"

# Aggregated summary
curl "http://localhost:8080/admin/usage/summary?period=daily" \
  -H "Authorization: Bearer your-admin-secret"
```

---

## Next steps

- **[Configuration](configuration.md)** — Set up additional providers, semantic caching, Redis, and region-aware routing
- **[Governance](governance.md)** — Configure RBAC, OIDC SSO, audit logs, and advanced guardrails
- **[Agent Protocols](agent-protocols.md)** — Connect MCP tools or enable A2A agent-to-agent communication
- **[Observability](observability.md)** — Set up Prometheus, Grafana, and OpenTelemetry tracing
- **[Deployment](deployment.md)** — Deploy to Kubernetes with Helm
