# Migration Guide: v1.0 → v1.1

This guide covers upgrading from OpenLimit v1.0 to v1.1.

## New Features

### Multi-Instance A2A SSE (Redis Pub/Sub)

A2A SSE streaming now works across multiple gateway instances via Redis Pub/Sub. When Redis is configured, task status changes are broadcast to all instances, allowing any instance to serve SSE watchers.

**Config changes:** No new fields required — uses existing `redis` config. The A2A Redis bridge activates automatically when Redis is healthy.

### Config Hot-Reload

Gateway config can be reloaded without restart. File polling watches `gateway.yaml` for changes. SIGHUP also triggers reload.

**Reloadable fields:**
- `logging`, `guardrails`, `routing`, `providers`, `models`, `billing`

**Non-reloadable fields (require restart):**
- `server`, `database`, `redis`, `kms`, `auth`, `admin`, `telemetry`

### Admin Dashboard SPA

Built-in dashboard at `GET /admin/ui/` with dark theme. Sections: Overview, Keys, Usage Analytics, Providers, Request Log. Uses the same bearer token auth as the admin API.

### Prompt Template Management

CRUD API for prompt templates:
- `POST /admin/prompts` — Create
- `GET /admin/prompts` — List
- `GET /admin/prompts/{id}` — Get
- `PUT /admin/prompts/{id}` — Update
- `DELETE /admin/prompts/{id}` — Delete

### Webhook mTLS

Guardrail webhooks now support mutual TLS authentication. Configure in guardrail stage config:

```yaml
guardrails:
  output:
    - type: webhook
      config:
        url: "https://moderation.internal:8888/check"
        tls_cert_file: "/etc/openlimit/client.pem"
        tls_key_file: "/etc/openlimit/client-key.pem"
        tls_ca_file: "/etc/openlimit/ca.pem"
```

### Provider Health Dashboard

Admin endpoints for provider and model health:
- `GET /admin/health/providers` — Per-provider circuit breaker state
- `GET /admin/health/models` — Per-model health metrics

### Redis Cluster Support

Redis Cluster mode for multi-instance deployments:

```yaml
redis:
  enabled: true
  addr: "redis-cluster:6379"
  cluster: true
```

## New Provider Adapters (v1.1.1)

The following providers are now supported:
- **AWS Bedrock** — Set `type: bedrock`, `region: us-east-1`
- **Google Vertex AI** — Set `type: vertex`, `project: my-project`, `region: us-central1`
- **Groq** — Set `type: groq`
- **Cohere** — Set `type: cohere`
- **Mistral** — Set `type: mistral`

## Database Migration

v1.1 adds migration `008_prompt_templates.sql`. Run automatically on gateway start, or manually:

```bash
./openlimit-gateway --migrate-only
```

## Breaking Changes

None. v1.1 is backward-compatible with v1.0 configurations.

## Removed Limitations

- A2A SSE now works multi-instance (was single-instance only)
- Redis Cluster now supported (was single-node only)
