package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAlertsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Generate alert rules from kubectl-hpa-status health semantics",
		Args:  cobra.NoArgs,
	}
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generate Prometheus or Datadog alert rules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("format")
			switch format {
			case "", "prometheus":
				_, err := fmt.Fprint(cmd.OutOrStdout(), prometheusAlertRules)
				return err
			case "datadog":
				_, err := fmt.Fprint(cmd.OutOrStdout(), datadogAlertRules)
				return err
			default:
				return fmt.Errorf("unsupported alert format %q (use prometheus or datadog)", format)
			}
		},
	}
	generate.Flags().String("format", "prometheus", "alert rule format: prometheus or datadog")
	cmd.AddCommand(generate)
	return cmd
}

const prometheusAlertRules = `groups:
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

const datadogAlertRules = `- name: HPA health score degraded
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
