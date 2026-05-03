# OpenLimit Helm Chart

Deploy the OpenLimit AI Gateway on Kubernetes with Helm.

## Quick Start

```bash
# Install with defaults (no providers configured)
helm install openlimit ./deploy/helm/openlimit

# Install with provider API keys
helm install openlimit ./deploy/helm/openlimit \
  --set secrets.openaiApiKey=sk-xxx \
  --set secrets.databaseUrl="postgres://user:pass@postgres:5432/openlimit?sslmode=disable"
```

## Configuration

### Minimal Install

The chart starts with zero dependencies — no database, no Redis, no providers:

```yaml
# values-minimal.yaml
config:
  gateway: |
    server:
      port: 8080
    logging:
      level: info
    telemetry:
      metrics:
        enabled: true
```

### With Database and Providers

```bash
helm install openlimit ./deploy/helm/openlimit \
  --set secrets.databaseUrl="postgres://openlimit:password@postgres.openlimit.svc:5432/openlimit?sslmode=disable" \
  --set secrets.openaiApiKey=sk-xxx \
  --set secrets.anthropicApiKey=sk-ant-xxx \
  --set-file config.gateway=my-gateway.yaml
```

### With Redis (Multi-Instance)

When Redis is enabled, set `replicaCount` to 2 or more:

```bash
helm install openlimit ./deploy/helm/openlimit \
  --set replicaCount=2 \
  --set secrets.databaseUrl="postgres://..." \
  --set-file config.gateway=gateway-with-redis.yaml
```

Where `gateway-with-redis.yaml` includes:

```yaml
redis:
  enabled: true
  addr: redis.openlimit.svc:6379
```

### With OIDC and RBAC

```bash
# 1. Deploy with RBAC disabled, create first admin
helm install openlimit ./deploy/helm/openlimit \
  --set secrets.adminBearerToken=my-admin-token \
  --set secrets.databaseUrl="postgres://..." \
  --set-file config.gateway=gateway.yaml

# 2. Create the first admin user
curl -X POST http://gateway:8080/admin/users \
  -H "Authorization: Bearer my-admin-token" \
  -d '{"subject":"oidc-user-123","email":"admin@example.com","role":"admin"}'

# 3. Upgrade with OIDC enabled
helm upgrade openlimit ./deploy/helm/openlimit \
  --set secrets.databaseUrl="postgres://..." \
  --set-file config.gateway=gateway-with-oidc.yaml
```

Where `gateway-with-oidc.yaml` includes:

```yaml
admin:
  enabled: true
  rbac_enabled: true
  oidc:
    enabled: true
    issuer: "https://dev-xxx.okta.com/oauth2/default"
    audience: "openlimit-admin"
```

### With KMS (Vault)

```bash
helm install openlimit ./deploy/helm/openlimit \
  --set secrets.vaultToken=hvs.xxx \
  --set secrets.databaseUrl="postgres://..." \
  --set-file config.gateway=gateway-with-vault.yaml
```

Where `gateway-with-vault.yaml` includes:

```yaml
kms:
  enabled: true
  type: vault
  key_id: "secret/data/openlimit-dek"
  vault:
    addr: "https://vault.example.com:8200"
```

## Autoscaling

### CPU/Memory-based HPA

```bash
helm install openlimit ./deploy/helm/openlimit \
  --set autoscaling.enabled=true \
  --set autoscaling.minReplicas=2 \
  --set autoscaling.maxReplicas=10 \
  --set autoscaling.targetCPUUtilizationPercentage=70
```

### Custom Metrics (Prometheus Adapter)

For HPA based on request rate, configure the Prometheus adapter with this rule:

```yaml
# prometheus-adapter-rules.yaml
rules:
  - seriesQuery: 'gateway_requests_total'
    resources:
      overrides:
        kubernetes_namespace: {resource: "namespace"}
        kubernetes_pod_name: {resource: "pod"}
    name:
      matches: "^(.*)_total"
      as: "${1}_per_second"
    metricsQuery: 'sum(rate(<<.Series>>[1m])) by (<<.GroupBy>>)'
```

Then install with custom metrics:

```bash
helm install openlimit ./deploy/helm/openlimit \
  --set autoscaling.enabled=true \
  --set-file autoscaling.customMetrics=custom-hpa-metrics.yaml
```

## Values Reference

| Key | Type | Default | Description |
|---|---|---|---|
| `replicaCount` | int | `1` | Number of gateway pods |
| `image.repository` | string | `openlimit/gateway` | Container image repository |
| `image.tag` | string | `""` | Image tag (defaults to appVersion) |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `config.gateway` | string | (minimal config) | Full gateway.yaml content |
| `secrets.databaseUrl` | string | `""` | Postgres connection string |
| `secrets.adminBearerToken` | string | `""` | Admin API bearer token |
| `secrets.openaiApiKey` | string | `""` | OpenAI API key |
| `secrets.anthropicApiKey` | string | `""` | Anthropic API key |
| `secrets.kmsStaticKey` | string | `""` | Static KMS DEK (base64) |
| `secrets.vaultToken` | string | `""` | Vault authentication token |
| `existingSecret` | string | `""` | Use an existing Secret resource |
| `migration.enabled` | bool | `true` | Run DB migrations as init container |
| `migration.backoffLimit` | int | `3` | Init container retry limit |
| `service.type` | string | `ClusterIP` | Kubernetes service type |
| `service.port` | int | `8080` | Service port |
| `ingress.enabled` | bool | `false` | Enable ingress |
| `autoscaling.enabled` | bool | `false` | Enable HPA |
| `autoscaling.minReplicas` | int | `2` | Minimum replicas (HPA) |
| `autoscaling.maxReplicas` | int | `10` | Maximum replicas (HPA) |
| `serviceMonitor.enabled` | bool | `false` | Enable Prometheus ServiceMonitor |
| `serviceMonitor.interval` | string | `15s` | Scrape interval |
| `resources.requests.cpu` | string | `100m` | CPU request |
| `resources.requests.memory` | string | `128Mi` | Memory request |
| `resources.limits.cpu` | string | `1` | CPU limit |
| `resources.limits.memory` | string | `512Mi` | Memory limit |
| `podSecurityContext` | object | (non-root) | Pod security context |
| `securityContext` | object | (read-only FS) | Container security context |

## Production Checklist

- [ ] Override `secrets.databaseUrl` with a real Postgres connection string
- [ ] Set provider API keys via secrets (not values.yaml)
- [ ] Use `existingSecret` or External Secrets Operator for secret management
- [ ] Enable Redis for multi-instance deployments (`replicaCount > 1`)
- [ ] Configure ingress with TLS (`cert-manager` recommended)
- [ ] Enable ServiceMonitor for Prometheus scraping
- [ ] Set resource requests and limits appropriate to your workload
- [ ] Configure KMS for provider key encryption at rest
- [ ] Bootstrap RBAC users before enabling OIDC in production

## Architecture

```
                    ┌─────────────┐
                    │  Ingress /  │
                    │  LoadBalancer│
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │   Gateway    │  x N replicas
            ┌───────┤   Pods      ├───────┐
            │       └─────────────┘       │
            │               │             │
     ┌──────▼──────┐ ┌──────▼──────┐ ┌────▼────┐
     │  Postgres   │ │    Redis    │ │  KMS    │
     │  (migrations│ │  (optional) │ │ (optional)│
     │   init-cont)│ │             │ │         │
     └─────────────┘ └─────────────┘ └─────────┘
```
