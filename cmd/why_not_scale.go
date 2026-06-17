package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mattsui2020/kubectl-hpa-status/internal/cmdoptions"
	hpaanalysis "github.com/mattsui2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
)

type whyNotScaleReport struct {
	Namespace  string   `json:"namespace" yaml:"namespace"`
	Name       string   `json:"name" yaml:"name"`
	Target     string   `json:"target" yaml:"target"`
	Summary    string   `json:"summary" yaml:"summary"`
	Observed   []string `json:"observed" yaml:"observed"`
	Unknown    []string `json:"unknown" yaml:"unknown"`
	NextChecks []string `json:"nextChecks" yaml:"nextChecks"`
}

func newWhyNotScaleCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "why-not-scale NAME [NAME...]",
		Short:             "Explain why an HPA is not scaling out despite high metrics",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWhyNotScale(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runWhyNotScale(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := applyCommandPreset(opts, cmdoptions.PresetWhyNotScale)

	reports := make([]whyNotScaleReport, 0, len(names))
	for _, name := range names {
		statusReport, err := buildStatusReportWithClient(ctx, &local, name, true, nil)
		if err != nil {
			if local.Output == "json" || local.Output == "yaml" {
				writeError(out, local.Output, err)
			}
			return err
		}
		reports = append(reports, buildWhyNotScaleReport(statusReport.Analysis))
	}

	value := any(reports)
	if len(reports) == 1 {
		value = reports[0]
	}

	format, templateStr := outputSelection(outputConfig{
		output: local.Output, template: local.Template, outputTemplates: local.OutputTemplates,
	})

	return writeOutput(out, format, templateStr, value, func() error {
		for i, report := range reports {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if err := writeWhyNotScaleText(out, report); err != nil {
				return err
			}
		}
		return nil
	})
}

func buildWhyNotScaleReport(analysis hpaanalysis.Analysis) whyNotScaleReport {
	report := whyNotScaleReport{
		Namespace: analysis.Namespace,
		Name:      analysis.Name,
		Target:    analysis.Target,
		Summary:   analysis.Summary,
		Unknown: []string{
			"controller-internal per-metric replica recommendations are not exposed through the HPA API",
		},
		NextChecks: []string{
			fmt.Sprintf("kubectl describe hpa %s -n %s", analysis.Name, analysis.Namespace),
		},
	}

	if analysis.Summary != "" {
		report.Observed = append(report.Observed, analysis.Summary)
	}
	for _, cond := range analysis.Conditions {
		if cond.Status == "True" || cond.Status == "False" {
			report.Observed = append(report.Observed, fmt.Sprintf("%s=%s: %s", cond.Type, cond.Status, cond.Reason))
		}
	}
	for _, metric := range analysis.Metrics {
		line := metric.Text
		if line == "" {
			line = fmt.Sprintf("%s %s current=%s target=%s", metric.Type, metric.Name, metric.Current, metric.Target)
		}
		if metric.Ratio != nil && *metric.Ratio > 1.0 {
			report.Observed = append(report.Observed, line)
		}
	}
	if analysis.Desired == analysis.Max && analysis.Max > 0 {
		report.Observed = append(report.Observed, "maxReplicas may be capping scale-up")
	}
	if analysis.ImpactMetric != nil && analysis.ImpactMetric.Ratio > 1.0 {
		report.Observed = append(report.Observed,
			fmt.Sprintf("Resource metric %s ratio=%.2f", analysis.ImpactMetric.Name, analysis.ImpactMetric.Ratio))
	}
	for _, warning := range analysis.Warnings {
		if warning != "" {
			report.Observed = append(report.Observed, warning)
		}
	}

	if len(report.Observed) == 0 {
		report.Observed = append(report.Observed, "no visible scale-up pressure detected from current HPA status")
	}
	return report
}

func writeWhyNotScaleText(out io.Writer, report whyNotScaleReport) error {
	if _, err := fmt.Fprintf(out, "Why not scale: %s/%s\n", report.Namespace, report.Name); err != nil {
		return err
	}
	if report.Summary != "" {
		if _, err := fmt.Fprintf(out, "%s\n", report.Summary); err != nil {
			return err
		}
	}
	if len(report.Observed) > 0 {
		if _, err := fmt.Fprintln(out, "\nObserved:"); err != nil {
			return err
		}
		for _, line := range report.Observed {
			if _, err := fmt.Fprintf(out, "  - %s\n", line); err != nil {
				return err
			}
		}
	}
	if len(report.Unknown) > 0 {
		if _, err := fmt.Fprintln(out, "\nUnknown / not exposed:"); err != nil {
			return err
		}
		for _, line := range report.Unknown {
			if _, err := fmt.Fprintf(out, "  - %s\n", line); err != nil {
				return err
			}
		}
	}
	if len(report.NextChecks) > 0 {
		if _, err := fmt.Fprintln(out, "\nNext checks:"); err != nil {
			return err
		}
		for _, line := range report.NextChecks {
			if _, err := fmt.Fprintf(out, "  - %s\n", strings.TrimSpace(line)); err != nil {
				return err
			}
		}
	}
	return nil
}