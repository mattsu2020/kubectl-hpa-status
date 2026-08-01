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

const prometheusRules = `groups:
- name: hpa-status
  rules:
  - alert: HPAScalingLimited
    expr: hpa_status_health_score < 80
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: HPA health score is degraded
      description: Run kubectl hpa status doctor {{ $labels.hpa }} -n {{ $labels.namespace }}.
  - alert: HPAMetricsUnavailable
    expr: hpa_status_health_score < 60
    for: 5m
    labels:
      severity: critical
    annotations:
      summary: HPA may be unable to compute scaling decisions
      description: Check metrics-server, custom.metrics.k8s.io, external.metrics.k8s.io, and KEDA triggers.
`

const datadogRules = `- name: HPA health score degraded
  query: avg(last_10m):avg:hpa_status_health_score{*} by {namespace,hpa} < 80
  message: "Run kubectl hpa status doctor {{hpa.name}} -n {{namespace.name}}"
  tags:
    - hpa
    - autoscaling
- name: HPA metrics unavailable
  query: avg(last_5m):avg:hpa_status_health_score{*} by {namespace,hpa} < 60
  message: "Check metrics APIs and external/custom metric adapters"
  tags:
    - hpa
    - metrics
`
