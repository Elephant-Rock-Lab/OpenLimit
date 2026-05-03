# Deployment

**What you'll learn:** How to deploy OpenLimit using Docker Compose (with profiles for Redis, monitoring, and the full stack) and the Helm chart for Kubernetes production deployments.

---

## Docker Compose

The primary Docker Compose file is at `deploy/docker-compose.yml`.

### Basic (Gateway + Postgres)

```bash
docker compose -f deploy/docker-compose.yml up
```

Starts the gateway with a Postgres database. This is the simplest setup and suitable for development or single-instance deployments.

### With monitoring (Prometheus + Grafana)

```bash
docker compose -f deploy/docker-compose.yml --profile monitoring up
```

Adds Prometheus and Grafana alongside the gateway:

- **Grafana:** `http://localhost:3000` (admin/admin)
- **Prometheus:** `http://localhost:9090`

Prometheus is pre-configured to scrape the gateway's `/metrics` endpoint. The Grafana dashboard is auto-provisioned from `deploy/grafana/openlimit-dashboard.json`.

### With Redis (shared state)

```bash
docker compose -f deploy/docker-compose.yml --profile stateful up
```

Adds Redis for shared state across multiple gateway instances:

- **Rate limiting:** Shared sliding window (accurate across pods)
- **Cache:** Shared Redis cache (replaces in-memory LRU)
- **Circuit breaker:** Shared state (failing upstreams skipped on all instances)

See [Configuration](configuration.md#redis) for Redis configuration details.

### Full stack

```bash
docker compose -f deploy/docker-compose.yml --profile monitoring --profile stateful up
```

Runs all services: gateway, Postgres, Redis, Prometheus, and Grafana.

### Available profiles

| Profile | Services | Use case |
|---|---|---|
| *(default)* | Gateway, Postgres | Development, single-instance |
| `monitoring` | Prometheus, Grafana | Observability |
| `stateful` | Redis | Horizontal scaling, shared state |
| `monitoring` + `stateful` | All of the above | Production-like |

### Docker Compose with custom config

Mount your own `gateway.yaml`:

```bash
docker compose -f deploy/docker-compose.yml up \
  -v $(pwd)/configs/gateway.yaml:/app/configs/gateway.yaml
```

Or use environment variables for secrets:

```bash
OPENAI_API_KEY=sk-xxx docker compose -f deploy/docker-compose.yml up
```

---

## Helm Chart (Kubernetes)

The Helm chart is at `deploy/helm/openlimit/`. See `deploy/helm/openlimit/README.md` for full parameter documentation.

### Install

```bash
helm install openlimit ./deploy/helm/openlimit \
  --set secrets.databaseUrl="postgres://user:pass@postgres:5432/openlimit" \
  --set secrets.openaiApiKey=sk-xxx \
  --set-file config.gateway=configs/gateway.example.yaml
```

### Chart features

| Resource | Description |
|---|---|
| **Deployment** | Configurable replicas, resource limits, security context |
| **ConfigMap** | Gateway configuration (raw YAML via `config.gateway`) |
| **Secret** | Sensitive values (database URL, API keys) |
| **Service** | ClusterIP by default, configurable type |
| **Ingress** | Optional, disabled by default |
| **HPA** | CPU/memory targets, custom Prometheus metrics |
| **ServiceMonitor** | Prometheus service discovery |
| **Init container** | Database migrations (`--migrate-only` flag) |

### Security defaults

The Helm chart applies security-first defaults:

- Non-root user (UID 1000)
- Read-only root filesystem
- No privilege escalation (`allowPrivilegeEscalation: false`)
- Secrets injected via `secretKeyRef` with `optional: true`
- Gateway starts without provider keys (returns errors for requests that need them)

### Configuration rolling restarts

The ConfigMap checksum is included as a pod annotation. Changing `config.gateway` triggers a rolling restart automatically:

```bash
helm upgrade openlimit ./deploy/helm/openlimit \
  --set-file config.gateway=configs/gateway-new.yaml
```

### No bundled dependencies

The chart installs with zero sub-charts. Redis and Postgres are not bundled — use your cluster's existing infrastructure or install them separately. This keeps the chart simple and avoids version conflicts.

### Custom values

Create a `values.yaml` for your environment:

```yaml
replicaCount: 3

secrets:
  databaseUrl: "postgres://user:pass@postgres:5432/openlimit"
  openaiApiKey: "sk-xxx"
  anthropicApiKey: "sk-ant-xxx"

config:
  gateway: |
    server:
      port: 8080
    auth:
      enabled: true
    admin:
      enabled: true
      bearer_token: "${ADMIN_TOKEN}"
    # ... full gateway.yaml

resources:
  requests:
    cpu: 250m
    memory: 256Mi
  limits:
    cpu: 500m
    memory: 512Mi

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70

serviceMonitor:
  enabled: true
```

---

## Kubernetes Production Checklist

### Secrets management

- [ ] Store provider API keys in Kubernetes Secrets or external secret managers (Vault, AWS Secrets Manager)
- [ ] Use `encrypted_value` for provider keys — never store plaintext keys in ConfigMaps
- [ ] Enable KMS for encryption at rest. See [Security](security.md)
- [ ] Rotate provider API keys periodically

### High availability

- [ ] Run 2+ gateway replicas
- [ ] Enable HPA with CPU target (e.g., `averageUtilization: 70`)
- [ ] Use Redis for shared state (rate limits, cache, circuit breakers)
- [ ] Configure `routing.defaults.max_retries: 2` for automatic failover
- [ ] Set up region-aware routing for multi-region deployments. See [Configuration](configuration.md)

### Database

- [ ] Use a managed Postgres service (RDS, Cloud SQL, Crunchy Data)
- [ ] Enable pgvector extension for semantic cache
- [ ] Set up connection pooling (PgBouncer) for high-concurrency workloads
- [ ] Configure backups and point-in-time recovery
- [ ] Use TLS for database connections

### Observability

- [ ] Enable `telemetry.metrics.enabled: true`
- [ ] Deploy Prometheus and configure ServiceMonitor
- [ ] Import Grafana dashboard from `deploy/grafana/openlimit-dashboard.json`
- [ ] Set up alert rules for SLO monitoring
- [ ] Enable OpenTelemetry tracing for debugging
- [ ] Ship logs to a centralized log aggregator

### Networking

- [ ] Use Ingress or LoadBalancer service type for external access
- [ ] Enable TLS termination at the ingress or load balancer level
- [ ] Configure `server.read_timeout_seconds` and `server.write_timeout_seconds`
- [ ] Set up network policies to restrict pod-to-pod traffic

### Governance

- [ ] Enable `auth.enabled: true` to require virtual keys
- [ ] Set `admin.bearer_token` to a strong random value
- [ ] Enable RBAC for multi-team access: `admin.rbac_enabled: true`
- [ ] Configure OIDC SSO for admin API access
- [ ] Set up guardrails for content safety. See [Governance](governance.md)

### Resource planning

- [ ] Set resource requests and limits (recommended: 256Mi-512Mi memory, 250m-500m CPU)
- [ ] Monitor `gateway_active_requests` gauge for capacity planning
- [ ] Use `gateway_request_duration_seconds` histogram for latency budgets
- [ ] Track `gateway_cost_dollars_total` for spend monitoring

---

## Developer commands

For local development:

```bash
make run    # run gateway
make test   # run tests
make lint   # gofmt + go vet
make build  # build binary
make tidy   # tidy Go modules
```

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full development guide including AIV framework overview, code style, and PR process.

---

## Next steps

- **[Configuration](configuration.md)** — Full `gateway.yaml` reference for production tuning
- **[Security](security.md)** — KMS setup, data residency, and security model
- **[Observability](observability.md)** — Prometheus metrics and Grafana dashboard setup
- **[Governance](governance.md)** — RBAC, OIDC, and audit logs
