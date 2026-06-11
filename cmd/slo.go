package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type sloReport struct {
	Namespace   string   `json:"namespace" yaml:"namespace"`
	Name        string   `json:"name" yaml:"name"`
	Metric      string   `json:"metric" yaml:"metric"`
	Target      string   `json:"target" yaml:"target"`
	Health      string   `json:"health" yaml:"health"`
	HealthScore int      `json:"healthScore" yaml:"healthScore"`
	Findings    []string `json:"findings" yaml:"findings"`
	Actions     []string `json:"actions" yaml:"actions"`
}

func newSLOCommand(opts *options) *cobra.Command {
	var metric string
	var target string
	cmd := &cobra.Command{
		Use:               "slo NAME",
		Short:             "Relate HPA health to an SLO metric",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSLO(cmd.Context(), cmd.OutOrStdout(), opts, args[0], metric, target)
		},
	}
	cmd.Flags().StringVar(&metric, "metric", "", "SLO metric name, for example latency_p95")
	cmd.Flags().StringVar(&target, "target", "", "SLO target, for example 300ms")
	_ = cmd.MarkFlagRequired("metric")
	return cmd
}

func runSLO(ctx context.Context, out io.Writer, opts *options, name, metric, target string) error {
	local := *opts
	local.explain = true
	local.metricHints = true
	report, err := buildStatusReportWithClient(ctx, &local, name, true, nil)
	if err != nil {
		return err
	}
	result := sloReport{
		Namespace:   report.Analysis.Namespace,
		Name:        report.Analysis.Name,
		Metric:      metric,
		Target:      target,
		Health:      report.Analysis.Health,
		HealthScore: report.Analysis.HealthScore,
		Findings: []string{
			fmt.Sprintf("HPA health is %s (%d/100)", report.Analysis.Health, report.Analysis.HealthScore),
		},
		Actions: []string{
			"Correlate the SLO time series with HPA desiredReplicas and current metrics.",
			"If SLO burn continues while HPA is at maxReplicas, run capacity-plan or preflight before raising limits.",
		},
	}
	if report.Analysis.Current == report.Analysis.Max {
		result.Findings = append(result.Findings, "current replicas are at maxReplicas; SLO risk may be capacity-limited")
	}
	if len(report.Analysis.Metrics) == 0 {
		result.Findings = append(result.Findings, "no current HPA metrics are visible; SLO correlation may require adapter diagnostics")
	}
	format, templateStr := outputSelection(outputConfig{output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates})
	return writeOutput(out, format, templateStr, result, func() error {
		_, _ = fmt.Fprintf(out, "SLO-aware HPA Report: %s/%s\n", result.Namespace, result.Name)
		_, _ = fmt.Fprintf(out, "SLO: %s target=%s\n\nFindings:\n", result.Metric, result.Target)
		for _, finding := range result.Findings {
			_, _ = fmt.Fprintf(out, "- %s\n", finding)
		}
		_, _ = fmt.Fprintln(out, "\nActions:")
		for _, action := range result.Actions {
			_, _ = fmt.Fprintf(out, "- %s\n", action)
		}
		return nil
	})
}
