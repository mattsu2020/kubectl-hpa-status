package cmd

import (
	"context"
	"fmt"
	"io"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
)

type metricsProbeOutput struct {
	Namespace          string                                `json:"namespace" yaml:"namespace"`
	Name               string                                `json:"name" yaml:"name"`
	Metrics            []hpaanalysis.Metric                  `json:"metrics" yaml:"metrics"`
	Freshness          []hpaanalysis.MetricFreshness         `json:"freshness,omitempty" yaml:"freshness,omitempty"`
	Contract           *hpaanalysis.MetricContractReport     `json:"contract,omitempty" yaml:"contract,omitempty"`
	AdapterDiagnostics *hpaanalysis.AdapterDiagnosticsReport `json:"adapterDiagnostics,omitempty" yaml:"adapterDiagnostics,omitempty"`
	Hints              *hpaanalysis.MetricHintsReport        `json:"hints,omitempty" yaml:"hints,omitempty"`
	PrometheusURL      string                                `json:"prometheusURL,omitempty" yaml:"prometheusURL,omitempty"`
	PrometheusChecks   []string                              `json:"prometheusChecks,omitempty" yaml:"prometheusChecks,omitempty"`
}

func newMetricsCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Inspect HPA metrics pipeline diagnostics",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newMetricsProbeCommand(opts))
	return cmd
}

func newMetricsProbeCommand(opts *options) *cobra.Command {
	var prometheusURL string
	cmd := &cobra.Command{
		Use:               "probe NAME",
		Short:             "Probe custom and external metrics adapter availability for an HPA",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMetricsProbe(cmd.Context(), cmd.OutOrStdout(), opts, args[0], prometheusURL)
		},
	}
	cmd.Flags().StringVar(&prometheusURL, "prometheus-url", "", "Prometheus base URL for adapter query follow-up checks")
	return cmd
}

func runMetricsProbe(ctx context.Context, out io.Writer, opts *options, name string, prometheusURL string) error {
	local := *opts
	local.diagnoseMetrics = true
	local.metricsFreshness = true
	local.metricContract = true
	local.adapterDiagnostics = true
	local.metricHints = true

	report, err := buildStatusReportWithClient(ctx, &local, name, true, nil)
	if err != nil {
		return err
	}
	result := metricsProbeOutput{
		Namespace:          report.Analysis.Namespace,
		Name:               report.Analysis.Name,
		Metrics:            report.Analysis.Metrics,
		Freshness:          report.Analysis.MetricFreshnessEntries,
		Contract:           report.Analysis.MetricContract,
		AdapterDiagnostics: report.Analysis.AdapterDiagnostics,
		Hints:              report.Analysis.MetricHints,
		PrometheusURL:      prometheusURL,
		PrometheusChecks:   prometheusMetricChecks(prometheusURL, report.Analysis.Metrics),
	}

	format, templateStr := outputSelection(outputConfig{output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates})
	return writeOutput(out, format, templateStr, result, func() error {
		return writeMetricsProbeText(out, result)
	})
}

func writeMetricsProbeText(out io.Writer, result metricsProbeOutput) error {
	_, _ = fmt.Fprintf(out, "Metrics probe: %s/%s\n\n", result.Namespace, result.Name)
	if len(result.Metrics) == 0 {
		_, _ = fmt.Fprintln(out, "No current metrics are visible in HPA status.")
	}
	for _, metric := range result.Metrics {
		_, _ = fmt.Fprintf(out, "- %s/%s: %s\n", metric.Type, metric.Name, metric.Text)
		if metric.Selector != "" {
			_, _ = fmt.Fprintf(out, "  selector: %s\n", metric.Selector)
		}
	}
	if len(result.Freshness) > 0 {
		_, _ = fmt.Fprintln(out, "\nFreshness:")
		for _, entry := range result.Freshness {
			available := "unknown"
			if entry.APIServiceAvailable != nil {
				available = fmt.Sprintf("%t", *entry.APIServiceAvailable)
			}
			_, _ = fmt.Fprintf(out, "- %s/%s status=%s apiAvailable=%s\n", entry.Type, entry.Name, entry.Status, available)
			if entry.APIServiceMessage != "" {
				_, _ = fmt.Fprintf(out, "  api: %s\n", entry.APIServiceMessage)
			}
			if entry.Risk != "" {
				_, _ = fmt.Fprintf(out, "  risk: %s\n", entry.Risk)
			}
		}
	}
	if result.Contract != nil && len(result.Contract.Checks) > 0 {
		_, _ = fmt.Fprintln(out, "\nMetric contract:")
		for _, check := range result.Contract.Checks {
			_, _ = fmt.Fprintf(out, "- %s/%s: %s\n", check.MetricType, check.MetricName, check.Status)
			if check.Remediation != "" {
				_, _ = fmt.Fprintf(out, "  fix: %s\n", check.Remediation)
			}
		}
	}
	if result.AdapterDiagnostics != nil && len(result.AdapterDiagnostics.Checks) > 0 {
		_, _ = fmt.Fprintln(out, "\nAdapter diagnostics:")
		if result.AdapterDiagnostics.Summary != "" {
			_, _ = fmt.Fprintf(out, "summary: %s\n", result.AdapterDiagnostics.Summary)
		}
		for _, check := range result.AdapterDiagnostics.Checks {
			_, _ = fmt.Fprintf(out, "- [%s] %s\n", check.Status, check.Name)
			if check.Details != "" {
				_, _ = fmt.Fprintf(out, "  details: %s\n", check.Details)
			}
			if check.Remediation != "" {
				_, _ = fmt.Fprintf(out, "  fix: %s\n", check.Remediation)
			}
		}
	}
	if result.Hints != nil && len(result.Hints.Hints) > 0 {
		_, _ = fmt.Fprintln(out, "\nLikely causes:")
		for _, hint := range result.Hints.Hints {
			_, _ = fmt.Fprintf(out, "- [%s] %s\n", hint.Severity, hint.Title)
		}
	}
	if len(result.PrometheusChecks) > 0 {
		_, _ = fmt.Fprintln(out, "\nPrometheus follow-up checks:")
		for _, check := range result.PrometheusChecks {
			_, _ = fmt.Fprintf(out, "- %s\n", check)
		}
	}
	return nil
}

func prometheusMetricChecks(base string, metrics []hpaanalysis.Metric) []string {
	if base == "" {
		return nil
	}
	checks := []string{"GET " + base + "/api/v1/query?query=up"}
	for _, metric := range metrics {
		if metric.Name != "" {
			checks = append(checks, "query metric freshness for "+metric.Name)
		}
	}
	return checks
}
