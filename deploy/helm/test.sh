#!/bin/bash
# Helm chart validation script
set -e

CHART_DIR="./deploy/helm/openlimit"

echo "=== Linting chart ==="
helm lint "$CHART_DIR"

echo ""
echo "=== Template: minimal install ==="
helm template test "$CHART_DIR" > /dev/null

echo "=== Template: with ingress ==="
helm template test "$CHART_DIR" --set ingress.enabled=true \
  --set 'ingress.hosts[0].host=gateway.example.com' \
  --set 'ingress.hosts[0].paths[0].path=/' > /dev/null

echo "=== Template: with autoscaling ==="
helm template test "$CHART_DIR" --set autoscaling.enabled=true > /dev/null

echo "=== Template: with ServiceMonitor ==="
helm template test "$CHART_DIR" --set serviceMonitor.enabled=true > /dev/null

echo "=== Template: with all features ==="
helm template test "$CHART_DIR" \
  --set replicaCount=3 \
  --set secrets.databaseUrl="postgres://u:p@db:5432/openlimit" \
  --set secrets.openaiApiKey=sk-test \
  --set autoscaling.enabled=true \
  --set ingress.enabled=true \
  --set 'ingress.hosts[0].host=gateway.example.com' \
  --set 'ingress.hosts[0].paths[0].path=/' \
  --set serviceMonitor.enabled=true > /dev/null

echo "=== Template: no migrations ==="
helm template test "$CHART_DIR" --set migration.enabled=false > /dev/null

echo "=== Template: existing secret ==="
helm template test "$CHART_DIR" --set existingSecret=my-secret > /dev/null

echo ""
echo "All templates render successfully ✅"
