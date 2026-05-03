# Observability

**What you'll learn:** How to monitor OpenLimit using Prometheus metrics, OpenTelemetry tracing, Grafana dashboards, and structured logging. All telemetry is opt-in with zero overhead when disabled.

---

## Prometheus Metrics

### Enable metrics

```yaml
telemetry:
  metrics:
    enabled: true
```

Scrape from `GET /metrics`:

```bash
curl http://localhost:8080/metrics
```

### Metrics reference

#### Request metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `gateway_requests_total` | Counter | model, provider, status, stream | Total requests processed |
| `gateway_request_duration_seconds` | Histogram | model, provider, stream | End-to-end request latency |
| `gateway_active_requests` | Gauge | — | Currently in-flight requests |
| `gateway_errors_total` | Counter | type, source | Errors by type and API source (`direct`, `admin`, `mcp`, `a2a`) |

#### Provider metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `gateway_provider_call_duration_seconds` | Histogram | provider, model | Per-provider call latency |
| `gateway_provider_errors_total` | Counter | provider, model, error_type | Provider call failures |
| `gateway_provider_region_duration_seconds` | Histogram | provider, model, region | Per-region provider call latency |
| `gateway_retries_total` | Counter | provider, model | Retry attempts |
| `gateway_fallbacks_total` | Counter | from_provider, to_provider | Fallback invocations |
| `gateway_circuit_breaker_rejections_total` | Counter | provider, model | Requests blocked by circuit breaker |

#### Token and cost metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `gateway_tokens_total` | Counter | model, provider, direction | Token usage (prompt/completion) |
| `gateway_cost_dollars_total` | Counter | model, provider | Cumulative cost in USD |

#### Cache metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `gateway_cache_hits_total` | Counter | model | Cache hit count |
| `gateway_cache_misses_total` | Counter | model | Cache miss count |

#### Governance metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `gateway_rate_limit_rejections_total` | Counter | key_prefix, project_id | Rate limit rejections |
| `gateway_budget_rejections_total` | Counter | key_prefix, project_id | Budget limit rejections |
| `gateway_guardrail_blocks_total` | Counter | stage, direction, model | Guardrail blocks |
| `gateway_guardrail_redactions_total` | Counter | stage, direction, model | Content redactions |
| `gateway_guardrail_duration_seconds` | Histogram | stage, direction | Guardrail check latency |

#### Infrastructure metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `gateway_redis_healthy` | Gauge | — | Redis connectivity (1=healthy, 0=degraded) |
| `gateway_audit_events_total` | Counter | event_type, outcome | Audit events recorded |
| `gateway_kms_operations_total` | Counter | operation, status | KMS encrypt/decrypt operations |
| `gateway_oidc_auth_total` | Counter | status | OIDC authentication attempts |
| `gateway_rbac_checks_total` | Counter | role, action, result | RBAC permission checks |

#### Routing metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `gateway_residency_filter_total` | Counter | result | Data residency filter decisions (allowed/denied) |

#### Agent protocol metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `mcp_server_connections` | Gauge | — | Active MCP server connections |
| `mcp_tool_calls_total` | Counter | — | Total MCP tool calls |
| `mcp_tool_call_duration_seconds` | Histogram | — | MCP tool call latency |
| `mcp_max_rounds_exceeded_total` | Counter | — | Agent loop max rounds reached |
| `gateway_a2a_tasks_created_total` | Counter | — | Total A2A tasks created |
| `gateway_a2a_task_completions_total` | Counter | status | A2A tasks completed by status |
| `gateway_a2a_task_duration_seconds` | Histogram | status, model | A2A task execution duration |

### Important notes

- **Without Redis**, rate limit and budget rejection metrics are per-instance. Enable Redis for shared state.
- The `gateway_provider_region_duration_seconds` histogram multiplies provider×model by regions. Use Prometheus recording rules for high-cardinality deployments (>100 models with >5 regions).
- Prometheus is the only supported metrics backend.

---

## OpenTelemetry Tracing

### Enable tracing

```yaml
telemetry:
  tracing:
    enabled: true
    endpoint: "localhost:4317"    # OTLP gRPC (Jaeger, Tempo, etc.)
    service_name: "openlimit"
    sample_rate: 0.1             # 10% of traces
```

### Trace correlation

When tracing is enabled, every response includes a `traceparent` header:

```
traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
```

Use this to correlate gateway requests with upstream traces in Jaeger, Grafana Tempo, or any OTLP-compatible backend.

Every response also includes `X-Request-ID` regardless of tracing status. This is a UUID generated per-request for log correlation.

### Supported backends

Any OTLP gRPC receiver:
- **Jaeger** (with OTLP collector)
- **Grafana Tempo**
- **SigNoz**
- **Honeycomb**
- **Lightstep**

---

## Grafana Dashboard

### Generate the dashboard

```bash
python scripts/generate-dashboard.py > deploy/grafana/openlimit-dashboard.json
```

### Run with monitoring stack

```bash
docker compose -f deploy/docker-compose.yml --profile monitoring up
```

This starts Prometheus + Grafana alongside the gateway. Grafana is available at `http://localhost:3000` (admin/admin).

Import the dashboard from `deploy/grafana/openlimit-dashboard.json` or let the provisioning configuration load it automatically.

### Dashboard panels

The generated dashboard includes:
- Request rate and latency (p50, p95, p99)
- Provider breakdown (requests per provider, per-model)
- Token usage and cost over time
- Cache hit rate
- Rate limit and budget rejections
- Guardrail blocks and redactions
- Circuit breaker status
- Redis health

---

## Prometheus Recording and Alert Rules

OpenLimit includes pre-built recording rules and alert rules for SLO monitoring. These are in `deploy/prometheus/`:

- **Recording rules** for frequently queried aggregations (request rate, error rate, p99 latency)
- **Alert rules** for common conditions (high error rate, circuit breaker open, Redis down, budget exceeded)

### Cardinality awareness

Key cardinality dimensions:
- `model` — bounded by your model registry size
- `provider` — bounded by number of configured providers
- `region` — bounded by region configuration
- `key_prefix` / `project_id` — grows with key count; consider aggregation

Avoid alerting on high-cardinality labels. Use recording rules to pre-aggregate.

---

## Structured Logging

OpenLimit uses Go's `slog` structured logger. All log entries include:

- `level` — DEBUG, INFO, WARN, ERROR
- `msg` — Human-readable message
- `request_id` — Correlation ID (when available)
- `provider` / `model` — Context for provider operations

Logs are written to stderr. Use `2>&1` to redirect or pipe to your log aggregator.

---

## Monitoring checklist

1. Enable `telemetry.metrics.enabled: true`
2. Configure Prometheus to scrape `GET /metrics`
3. Import the Grafana dashboard
4. Set up alert rules for SLOs
5. Enable tracing for debugging (start with `sample_rate: 0.1`)
6. Use `X-Request-ID` and `traceparent` headers for request tracing

---

## Next steps

- **[Configuration](configuration.md)** — Telemetry YAML reference
- **[Deployment](deployment.md)** — Docker Compose monitoring profile, Prometheus ServiceMonitor
- **[API Reference](api-reference.md)** — Response headers (`X-Request-ID`, `X-Provider`, `X-Cache`, `X-Cost-USD`)
- **[Governance](governance.md)** — Audit log queries
