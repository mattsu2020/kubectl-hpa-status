// Package alerts holds the alert-rule templates emitted by
// `alerts generate`. The rules encode this tool's health-score semantics for
// Prometheus and Datadog, so they live next to each other and away from the
// cobra wiring.
//
// Lifted from cmd/alerts.go as part of the cmd/ sub-package split.
package alerts

import "fmt"

// Format names a supported alert-rule dialect.
type Format string

const (
	// FormatPrometheus emits a Prometheus rule group.
	FormatPrometheus Format = "prometheus"
	// FormatDatadog emits Datadog monitor definitions.
	FormatDatadog Format = "datadog"
	// DashboardGrafana emits a Grafana dashboard JSON document.
	DashboardGrafana string = "grafana"
)

// Rules returns the rule document for the requested format. An empty format
// means Prometheus, preserving the flag's default.
func Rules(format string) (string, error) {
	switch Format(format) {
	case "", FormatPrometheus:
		return prometheusRules, nil
	case FormatDatadog:
		return datadogRules, nil
	default:
		return "", fmt.Errorf("unsupported alert format %q (use %s or %s)", format, FormatPrometheus, FormatDatadog)
	}
}

// Dashboard returns the dashboard document for the requested format.
func Dashboard(format string) (string, error) {
	switch format {
	case DashboardGrafana:
		return grafanaDashboardJSON, nil
	default:
		return "", fmt.Errorf("unsupported dashboard format %q (use %s)", format, DashboardGrafana)
	}
}

const prometheusRules = `groups:
- name: hpa-status
  rules:
  - alert: HPAScalingLimited
    expr: hpa_health_score < 80
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: HPA health score is degraded
      description: Run kubectl hpa status doctor {{ $labels.name }} -n {{ $labels.namespace }}.
  - alert: HPAMetricsUnavailable
    expr: hpa_health_score < 60
    for: 5m
    labels:
      severity: critical
    annotations:
      summary: HPA may be unable to compute scaling decisions
      description: Check metrics-server, custom.metrics.k8s.io, external.metrics.k8s.io, and KEDA triggers.
`

const datadogRules = `- name: HPA health score degraded
  query: avg(last_10m):avg:hpa_health_score{*} by {namespace,name} < 80
  message: "Run kubectl hpa status doctor {{name.name}} -n {{namespace.name}}"
  tags:
    - hpa
    - autoscaling
- name: HPA metrics unavailable
  query: avg(last_5m):avg:hpa_health_score{*} by {namespace,name} < 60
  message: "Check metrics APIs and external/custom metric adapters"
  tags:
    - hpa
    - metrics
`

const grafanaDashboardJSON = `{
  "annotations": {
    "list": [
      {
        "builtIn": 1,
        "datasource": { "type": "grafana", "uid": "-- Grafana --" },
        "enable": true,
        "hide": true,
        "iconColor": "rgba(0, 211, 255, 1)",
        "name": "Annotations & Alerts",
        "type": "dashboard"
      }
    ]
  },
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 1,
  "id": null,
  "links": [],
  "panels": [
    {
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "fieldConfig": {
        "defaults": {
          "max": 100,
          "min": 0,
          "thresholds": {
            "mode": "absolute",
            "steps": [
              { "color": "red", "value": null },
              { "color": "yellow", "value": 60 },
              { "color": "green", "value": 85 }
            ]
          },
          "unit": "none"
        }
      },
      "gridPos": { "h": 8, "w": 8, "x": 0, "y": 0 },
      "id": 1,
      "options": {
        "orientation": "auto",
        "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false },
        "showThresholdLabels": false,
        "showThresholdMarkers": true
      },
      "targets": [
        {
          "expr": "min(hpa_health_score{namespace=~\"$namespace\",name=~\"$hpa\"})",
          "legendFormat": "Min Health Score",
          "refId": "A"
        }
      ],
      "title": "Lowest HPA Health Score",
      "type": "gauge"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "fieldConfig": {
        "defaults": { "max": 100, "min": 0, "unit": "none" }
      },
      "gridPos": { "h": 8, "w": 16, "x": 8, "y": 0 },
      "id": 2,
      "targets": [
        {
          "expr": "hpa_health_score{namespace=~\"$namespace\",name=~\"$hpa\"}",
          "legendFormat": "{{namespace}}/{{name}}",
          "refId": "A"
        }
      ],
      "title": "HPA Health Score Over Time",
      "type": "timeseries"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "gridPos": { "h": 9, "w": 12, "x": 0, "y": 8 },
      "id": 3,
      "targets": [
        {
          "expr": "kube_horizontalpodautoscaler_status_current_replicas{namespace=~\"$namespace\",horizontalpodautoscaler=~\"$hpa\"}",
          "legendFormat": "{{namespace}}/{{horizontalpodautoscaler}} current",
          "refId": "A"
        },
        {
          "expr": "kube_horizontalpodautoscaler_status_desired_replicas{namespace=~\"$namespace\",horizontalpodautoscaler=~\"$hpa\"}",
          "legendFormat": "{{namespace}}/{{horizontalpodautoscaler}} desired",
          "refId": "B"
        },
        {
          "expr": "kube_horizontalpodautoscaler_spec_max_replicas{namespace=~\"$namespace\",horizontalpodautoscaler=~\"$hpa\"}",
          "legendFormat": "{{namespace}}/{{horizontalpodautoscaler}} max",
          "refId": "C"
        }
      ],
      "title": "HPA Replicas (Current vs Desired vs Max)",
      "type": "timeseries"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "gridPos": { "h": 9, "w": 12, "x": 12, "y": 8 },
      "id": 4,
      "targets": [
        {
          "expr": "kube_horizontalpodautoscaler_status_condition{namespace=~\"$namespace\",horizontalpodautoscaler=~\"$hpa\",condition=\"ScalingLimited\",status=\"true\"}",
          "legendFormat": "ScalingLimited: {{namespace}}/{{horizontalpodautoscaler}}",
          "refId": "A"
        },
        {
          "expr": "kube_horizontalpodautoscaler_status_condition{namespace=~\"$namespace\",horizontalpodautoscaler=~\"$hpa\",condition=\"ScalingActive\",status=\"false\"}",
          "legendFormat": "ScalingInactive: {{namespace}}/{{horizontalpodautoscaler}}",
          "refId": "B"
        }
      ],
      "title": "ScalingLimited & ScalingInactive Conditions",
      "type": "timeseries"
    }
  ],
  "refresh": "30s",
  "schemaVersion": 39,
  "tags": ["kubernetes", "hpa", "kubectl-hpa-status"],
  "templating": {
    "list": [
      {
        "current": {},
        "hide": 0,
        "label": "Datasource",
        "name": "DS_PROMETHEUS",
        "query": "prometheus",
        "type": "datasource"
      },
      {
        "allValue": ".*",
        "current": { "selected": true, "text": "All", "value": "$__all" },
        "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
        "includeAll": true,
        "label": "Namespace",
        "multi": true,
        "name": "namespace",
        "query": "label_values(hpa_health_score, namespace)",
        "refresh": 2,
        "type": "query"
      },
      {
        "allValue": ".*",
        "current": { "selected": true, "text": "All", "value": "$__all" },
        "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
        "includeAll": true,
        "label": "HPA",
        "multi": true,
        "name": "hpa",
        "query": "label_values(hpa_health_score{namespace=~\"$namespace\"}, name)",
        "refresh": 2,
        "type": "query"
      }
    ]
  },
  "time": { "from": "now-6h", "to": "now" },
  "title": "kubectl-hpa-status Overview",
  "uid": "kubectl-hpa-status-overview",
  "version": 1
}
`
