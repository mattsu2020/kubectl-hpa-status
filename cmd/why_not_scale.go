package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
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
	local := applyCommandPreset(opts, presetWhyNotScale)

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

// collectWhyNotScaleObservations gathers human-readable signals (summary,
// conditions, over-target metrics, max-cap, impact metric, warnings) into the
// Observed slice. Extracted from buildWhyNotScaleReport to keep that function
// below the gocyclo threshold.
func collectWhyNotScaleObservations(analysis hpaanalysis.Analysis) []string {
	var observed []string
	observed = appendIfNotEmpty(observed, analysis.Summary)
	for _, cond := range analysis.Conditions {
		if cond.Status == "True" || cond.Status == "False" {
			observed = append(observed, fmt.Sprintf("%s=%s: %s", cond.Type, cond.Status, cond.Reason))
		}
	}
	for _, metric := range analysis.Metrics {
		line := metric.Text
		if line == "" {
			line = fmt.Sprintf("%s %s current=%s target=%s", metric.Type, metric.Name, metric.Current, metric.Target)
		}
		if metric.Ratio != nil && *metric.Ratio > 1.0 {
			observed = append(observed, line)
		}
	}
	if analysis.Desired == analysis.Max && analysis.Max > 0 {
		observed = append(observed, "maxReplicas may be capping scale-up")
	}
	if analysis.ImpactMetric != nil && analysis.ImpactMetric.Ratio > 1.0 {
		observed = append(observed,
			fmt.Sprintf("Resource metric %s ratio=%.2f", analysis.ImpactMetric.Name, analysis.ImpactMetric.Ratio))
	}
	for _, warning := range analysis.Warnings {
		observed = appendIfNotEmpty(observed, warning)
	}
	if len(observed) == 0 {
		observed = append(observed, "no visible scale-up pressure detected from current HPA status")
	}
	return observed
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

	report.Observed = collectWhyNotScaleObservations(analysis)
	return report
}

// writeWhyNotScaleSection writes a titled bullet list, skipping it when lines
// is empty. Shared by writeWhyNotScaleText to avoid tripling the cyclomatic
// complexity with three near-identical output blocks.
func writeWhyNotScaleSection(out io.Writer, title string, lines []string, trim bool) error {
	if len(lines) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, title); err != nil {
		return err
	}
	for _, line := range lines {
		if trim {
			line = strings.TrimSpace(line)
		}
		if _, err := fmt.Fprintf(out, "  - %s\n", line); err != nil {
			return err
		}
	}
	return nil
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
	if err := writeWhyNotScaleSection(out, "\nObserved:", report.Observed, false); err != nil {
		return err
	}
	if err := writeWhyNotScaleSection(out, "\nUnknown / not exposed:", report.Unknown, false); err != nil {
		return err
	}
	if err := writeWhyNotScaleSection(out, "\nNext checks:", report.NextChecks, true); err != nil {
		return err
	}
	return nil
}

// appendIfNotEmpty appends s to slice only when s is non-empty.
func appendIfNotEmpty(slice []string, s string) []string {
	if s != "" {
		return append(slice, s)
	}
	return slice
}
