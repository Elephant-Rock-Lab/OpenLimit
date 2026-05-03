#!/usr/bin/env python3
"""Generate Grafana dashboard JSON for OpenLimit gateway metrics.

Usage:
    python scripts/generate-dashboard.py > deploy/grafana/openlimit-dashboard.json

Customize the panels array below to add/remove metrics.
"""

import json
import sys


def panel(title, expr, panel_type="timeseries", unit="short", legend="", grid=None):
    """Create a Grafana panel definition."""
    p = {
        "title": title,
        "type": panel_type,
        "gridPos": grid or {"h": 8, "w": 12},
        "fieldConfig": {
            "defaults": {
                "unit": unit,
                "custom": {"drawStyle": "line", "fillOpacity": 10},
            },
            "overrides": [],
        },
        "targets": [
            {
                "expr": expr,
                "legendFormat": legend or "",
                "refId": "A",
            }
        ],
    }
    return p


panels = [
    # Row: Request Overview
    panel(
        "Request Rate",
        'sum(rate(gateway_requests_total[5m]))',
        legend="req/s",
        unit="reqps",
        grid={"h": 8, "w": 8},
    ),
    panel(
        "Error Rate",
        'gateway:error_rate:5m',
        legend="error rate",
        unit="percentunit",
        grid={"h": 8, "w": 8},
    ),
    panel(
        "Active Requests",
        'gateway_active_requests',
        panel_type="stat",
        legend="active",
        unit="short",
        grid={"h": 8, "w": 8},
    ),

    # Row: Latency
    panel(
        "Request Latency (p50/p90/p99)",
        'gateway:latency_p50:5m',
        legend="p50",
        unit="s",
        grid={"h": 8, "w": 12},
    ),
    panel(
        "Request Latency p90",
        'gateway:latency_p90:5m',
        legend="p90",
        unit="s",
        grid={"h": 8, "w": 12},
    ),
    panel(
        "Request Latency p99",
        'gateway:latency_p99:5m',
        legend="p99",
        unit="s",
        grid={"h": 8, "w": 12},
    ),

    # Row: Cost & Tokens
    panel(
        "Cost Rate (USD/s)",
        'gateway:cost_rate:5m',
        legend="cost",
        unit="USD",
        grid={"h": 8, "w": 12},
    ),
    panel(
        "Token Rate",
        'gateway:token_rate:5m',
        legend="tokens/s",
        unit="short",
        grid={"h": 8, "w": 12},
    ),
    panel(
        "Cost by Model",
        'sum by (model) (rate(gateway_cost_dollars_total[5m]))',
        legend="{{model}}",
        unit="USD",
        grid={"h": 8, "w": 12},
    ),

    # Row: Cache & Retries
    panel(
        "Cache Hit Rate",
        'gateway:cache_hit_rate:5m',
        legend="hit rate",
        unit="percentunit",
        grid={"h": 8, "w": 12},
    ),
    panel(
        "Rate Limit Rejections",
        'sum by (project_id) (rate(gateway_rate_limit_rejections_total[5m]))',
        legend="{{project_id}}",
        unit="rejs",
        grid={"h": 8, "w": 12},
    ),
    panel(
        "Provider Retries",
        'sum by (provider) (rate(gateway_retries_total[5m]))',
        legend="{{provider}}",
        unit="short",
        grid={"h": 8, "w": 12},
    ),
    panel(
        "Provider Fallbacks",
        'sum by (from_provider, to_provider) (rate(gateway_fallbacks_total[5m]))',
        legend="{{from_provider}} → {{to_provider}}",
        unit="short",
        grid={"h": 8, "w": 12},
    ),
]

# Also add p90 and p99 as additional targets to the latency panel
panels[3]["targets"].append({
    "expr": "gateway:latency_p90:5m",
    "legendFormat": "p90",
    "refId": "B",
})
panels[3]["targets"].append({
    "expr": "gateway:latency_p99:5m",
    "legendFormat": "p99",
    "refId": "C",
})

dashboard = {
    "annotations": {"list": []},
    "editable": True,
    "fiscalYearStartMonth": 0,
    "graphTooltip": 1,
    "id": None,
    "links": [],
    "panels": panels,
    "schemaVersion": 39,
    "tags": ["openlimit", "gateway"],
    "templating": {"list": []},
    "time": {"from": "now-1h", "to": "now"},
    "timepicker": {},
    "timezone": "",
    "title": "OpenLimit Gateway",
    "uid": "openlimit",
    "version": 0,
}

json.dump(dashboard, sys.stdout, indent=2)
print()  # trailing newline
