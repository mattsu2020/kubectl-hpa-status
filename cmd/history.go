package cmd

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
)

type historyReport struct {
	Namespace         string                     `json:"namespace" yaml:"namespace"`
	Name              string                     `json:"name" yaml:"name"`
	Since             string                     `json:"since" yaml:"since"`
	Prometheus        string                     `json:"prometheus,omitempty" yaml:"prometheus,omitempty"`
	Health            string                     `json:"health" yaml:"health"`
	HealthScore       int                        `json:"healthScore" yaml:"healthScore"`
	Queries           []prometheusQuery          `json:"queries,omitempty" yaml:"queries,omitempty"`
	Anomalies         []string                   `json:"anomalies,omitempty" yaml:"anomalies,omitempty"`
	Churn             *hpaanalysis.ChurnAnalysis `json:"churn,omitempty" yaml:"churn,omitempty"`
	StructuredSummary string                     `json:"structuredSummary,omitempty" yaml:"structuredSummary,omitempty"`
}

type prometheusQuery struct {
	Name string `json:"name" yaml:"name"`
	URL  string `json:"url" yaml:"url"`
}

func newHistoryCommand(opts *options) *cobra.Command {
	var since time.Duration
	var prometheusURL string
	cmd := &cobra.Command{
		Use:               "history NAME",
		Short:             "Build a history and trend report for an HPA",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHistory(cmd.Context(), cmd.OutOrStdout(), opts, args[0], since, prometheusURL)
		},
	}
	cmd.Flags().DurationVar(&since, "since", 6*time.Hour, "lookback window")
	cmd.Flags().StringVar(&prometheusURL, "prometheus", "", "Prometheus base URL used to generate query_range links")
	return cmd
}

func runHistory(ctx context.Context, out io.Writer, opts *options, name string, since time.Duration, prometheusURL string) error {
	local := applyCommandPreset(opts, presetHistory)
	local.Events = EventOption{Enabled: true, Limit: 50}
	local.TrendSince = since
	report, err := buildStatusReportWithClient(ctx, &local, name, true, nil)
	if err != nil {
		return err
	}
	result := historyReport{
		Namespace:         report.Analysis.Namespace,
		Name:              report.Analysis.Name,
		Since:             since.String(),
		Prometheus:        prometheusURL,
		Health:            report.Analysis.Health,
		HealthScore:       report.Analysis.HealthScore,
		Churn:             report.Analysis.ChurnAnalysis,
		StructuredSummary: report.Analysis.Summary,
		Anomalies:         historyAnomalies(report.Analysis),
	}
	if prometheusURL != "" {
		result.Queries = buildPrometheusHistoryQueries(prometheusURL, report.Analysis, since)
	}
	format, templateStr := selectOutputFromOptions(opts)
	return writeOutput(out, format, templateStr, result, func() error {
		_, _ = fmt.Fprintf(out, "HPA history: %s/%s since=%s\n", result.Namespace, result.Name, result.Since)
		_, _ = fmt.Fprintf(out, "Health: %s (%d/100)\n", result.Health, result.HealthScore)
		if len(result.Anomalies) > 0 {
			_, _ = fmt.Fprintln(out, "\nDetected patterns:")
			for _, anomaly := range result.Anomalies {
				_, _ = fmt.Fprintf(out, "- %s\n", anomaly)
			}
		}
		if len(result.Queries) > 0 {
			_, _ = fmt.Fprintln(out, "\nPrometheus query_range links:")
			for _, query := range result.Queries {
				_, _ = fmt.Fprintf(out, "- %s: %s\n", query.Name, query.URL)
			}
		}
		return nil
	})
}

func historyAnomalies(a hpaanalysis.Analysis) []string {
	var anomalies []string
	if a.Health == string(hpaanalysis.HealthLimited) && a.Current == a.Max {
		anomalies = append(anomalies, "HPA is capped at maxReplicas; investigate capacity and target metric demand")
	}
	if a.ChurnAnalysis != nil && (a.ChurnAnalysis.Level == hpaanalysis.ChurnHigh || a.ChurnAnalysis.Level == hpaanalysis.ChurnCritical) {
		anomalies = append(anomalies, "replica churn suggests oscillation; review stabilization windows and tolerance")
	}
	if a.StabilizationRemaining != nil && *a.StabilizationRemaining > 0 {
		anomalies = append(anomalies, "scale-down appears to be held by stabilization")
	}
	return anomalies
}

func buildPrometheusHistoryQueries(base string, a hpaanalysis.Analysis, since time.Duration) []prometheusQuery {
	end := time.Now()
	start := end.Add(-since)
	params := func(query string) string {
		u, err := url.Parse(base)
		if err != nil {
			return base
		}
		u.Path = "/api/v1/query_range"
		q := u.Query()
		q.Set("query", query)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		q.Set("step", "60s")
		u.RawQuery = q.Encode()
		return u.String()
	}
	queries := []prometheusQuery{{
		Name: "replicas",
		URL:  params(fmt.Sprintf(`kube_horizontalpodautoscaler_status_current_replicas{namespace="%s",horizontalpodautoscaler="%s"}`, a.Namespace, a.Name)),
	}}
	for _, metric := range a.Metrics {
		if metric.Name == "" {
			continue
		}
		queries = append(queries, prometheusQuery{Name: metric.Name, URL: params(metric.Name)})
	}
	return queries
}
