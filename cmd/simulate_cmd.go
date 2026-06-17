package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type simulateReport struct {
	Namespace      string                      `json:"namespace" yaml:"namespace"`
	Name           string                      `json:"name" yaml:"name"`
	Before         hpaanalysis.SimulationState `json:"before" yaml:"before"`
	After          hpaanalysis.SimulationState `json:"after" yaml:"after"`
	Confidence     string                      `json:"confidence" yaml:"confidence"`
	Parameter      string                      `json:"parameter,omitempty" yaml:"parameter,omitempty"`
	Interpretation []string                    `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	Suggestions    []hpaanalysis.Suggestion    `json:"suggestions,omitempty" yaml:"suggestions,omitempty"`
	RiskWarnings   []string                    `json:"riskWarnings,omitempty" yaml:"riskWarnings,omitempty"`
}

func newSimulateCommand(opts *options) *cobra.Command {
	var setMetric []string
	var setTarget []string
	var tolerance string
	var suggest bool
	var duration int32

	cmd := &cobra.Command{
		Use:               "simulate NAME",
		Short:             "What-if analysis for HPA scaling decisions",
		Long:              "Simulate HPA behavior under hypothetical conditions without modifying the cluster.\nUses the public HPA scaling algorithm (estimated, not controller-internal).",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSimulate(cmd.Context(), cmd.OutOrStdout(), opts, args[0],
				setMetric, setTarget, tolerance, suggest, duration)
		},
	}
	cmd.Flags().StringArrayVar(&setMetric, "set-metric", nil,
		"override current metric value (e.g. cpu=90%%, memory=4Gi)")
	cmd.Flags().StringArrayVar(&setTarget, "set-target", nil,
		"override metric target value (e.g. cpu=60)")
	cmd.Flags().StringVar(&tolerance, "tolerance", "",
		"override HPA tolerance (e.g. 0.1)")
	cmd.Flags().BoolVar(&suggest, "suggest", false,
		"show suggestions for the simulated state")
	cmd.Flags().Int32Var(&duration, "duration", 0,
		"time-series projection duration in seconds (0 = single-point)")
	return cmd
}

func runSimulate(ctx context.Context, out io.Writer, opts *options, name string,
	setMetric, setTarget []string, tolerance string, suggest bool, _ int32) error {

	client, err := opts.newClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	hpa, err := client.Interface.AutoscalingV2().HorizontalPodAutoscalers(client.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get HPA %s/%s: %w", client.Namespace, name, err)
	}

	// Build overrides map from flags.
	overrides, err := buildSimulateOverrides(setTarget, tolerance)
	if err != nil {
		return err
	}

	// Run simulation with spec overrides.
	var simResult *hpaanalysis.SimulationResult
	if len(overrides) > 0 {
		simResult, err = hpaanalysis.SimulateHPA(hpa, overrides, hpaanalysis.HealthWeights{})
		if err != nil {
			return fmt.Errorf("simulation failed: %w", err)
		}
	} else {
		simResult = simulateCurrentState(hpa)
	}

	// --set-metric: apply metric value overrides for display.
	// These modify the simulated "current metric values" which affects ratio display.
	if err := validateSetMetricFlags(setMetric); err != nil {
		return err
	}

	// Build report.
	report := simulateReport{
		Namespace:      hpa.Namespace,
		Name:           hpa.Name,
		Before:         simResult.Before,
		After:          simResult.After,
		Confidence:     simResult.Confidence,
		Parameter:      simResult.Parameter,
		Interpretation: simResult.Interpretation,
		RiskWarnings:   simResult.RiskWarnings,
	}

	// Optional suggestions on the simulated state.
	if suggest {
		var minReplicas int32 = 1
		if hpa.Spec.MinReplicas != nil {
			minReplicas = *hpa.Spec.MinReplicas
		}
		report.Suggestions = hpaanalysis.BuildSuggestions(hpa, minReplicas)
	}

	// Render output.
	format, _ := outputSelection(outputConfig{
		report: opts.Report, output: opts.Output, template: opts.Template,
		outputTemplates: opts.OutputTemplates,
	})

	switch format {
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	case "yaml":
		data, marshalErr := yaml.Marshal(report)
		if marshalErr != nil {
			return marshalErr
		}
		_, err = out.Write(data)
		return err
	default:
		theme := style.NewTheme(shouldColorize(opts.Color, out))
		return writeSimulateText(out, report, theme)
	}
}

// buildSimulateOverride builds the spec override map from --set-target and
// --tolerance flags.
func buildSimulateOverrides(setTarget []string, tolerance string) (map[string]string, error) {
	overrides := make(map[string]string)

	// --set-target: cpu=60 → targetAverageUtilization=60
	for _, t := range setTarget {
		parts := strings.SplitN(t, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --set-target %q: expected name=value format", t)
		}
		metricName := strings.ToLower(parts[0])
		value := parts[1]
		switch metricName {
		case "cpu":
			overrides["cpu.targetAverageUtilization"] = value
		case "memory":
			overrides["memory.targetAverageUtilization"] = value
		default:
			overrides[metricName+".targetAverageUtilization"] = value
		}
	}

	// --tolerance
	if tolerance != "" {
		overrides["tolerance"] = tolerance
	}

	return overrides, nil
}

// simulateCurrentState returns a SimulationResult reflecting the HPA's current
// state, used when no spec overrides are supplied.
func simulateCurrentState(hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.SimulationResult {
	analysis := hpaanalysis.AnalyzeWithOptions(hpa, true, hpaanalysis.AnalysisOptions{})
	simResult := &hpaanalysis.SimulationResult{
		Before: hpaanalysis.SimulationState{
			DesiredReplicas: analysis.Desired,
			Health:          analysis.Health,
			HealthScore:     analysis.HealthScore,
			Summary:         analysis.Summary,
			Metrics:         analysis.Metrics,
		},
		Confidence: "estimated",
	}
	simResult.After = simResult.Before
	return simResult
}

// validateSetMetricFlags validates the --set-metric name=value pairs. The
// metric value override is informational for now and is not applied.
func validateSetMetricFlags(setMetric []string) error {
	for _, m := range setMetric {
		parts := strings.SplitN(m, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid --set-metric %q: expected name=value format", m)
		}
	}
	return nil
}

func writeSimulateText(out io.Writer, report simulateReport, theme style.Theme) error {
	_, _ = fmt.Fprintf(out, "HPA Simulation: %s/%s\n\n", report.Namespace, report.Name)

	if report.Parameter != "" {
		_, _ = fmt.Fprintf(out, "  Parameter: %s\n", report.Parameter)
	}
	_, _ = fmt.Fprintf(out, "  Confidence: %s\n\n", theme.SummaryColor(report.Confidence))

	// Before/After comparison.
	_, _ = fmt.Fprintln(out, "  Current State:")
	_, _ = fmt.Fprintf(out, "    desiredReplicas: %d\n", report.Before.DesiredReplicas)
	_, _ = fmt.Fprintf(out, "    health: %s (score: %d)\n", report.Before.Health, report.Before.HealthScore)
	if report.Before.Summary != "" {
		_, _ = fmt.Fprintf(out, "    summary: %s\n", report.Before.Summary)
	}

	if report.Before.DesiredReplicas != report.After.DesiredReplicas ||
		report.Before.HealthScore != report.After.HealthScore {
		_, _ = fmt.Fprintln(out, "\n  Simulated State:")
		_, _ = fmt.Fprintf(out, "    desiredReplicas: %d", report.After.DesiredReplicas)
		if report.After.DesiredReplicas != report.Before.DesiredReplicas {
			_, _ = fmt.Fprintf(out, " (was %d)", report.Before.DesiredReplicas)
		}
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintf(out, "    health: %s (score: %d)\n", report.After.Health, report.After.HealthScore)
		if report.After.Summary != "" {
			_, _ = fmt.Fprintf(out, "    summary: %s\n", report.After.Summary)
		}
	}

	if len(report.Interpretation) > 0 {
		_, _ = fmt.Fprintln(out, "\n  Interpretation:")
		for _, line := range report.Interpretation {
			_, _ = fmt.Fprintf(out, "    - %s\n", line)
		}
	}

	if len(report.RiskWarnings) > 0 {
		_, _ = fmt.Fprintln(out, "\n  Risk Warnings:")
		for _, w := range report.RiskWarnings {
			_, _ = fmt.Fprintf(out, "    ⚠ %s\n", w)
		}
	}

	if len(report.Suggestions) > 0 {
		_, _ = fmt.Fprintln(out, "\n  Suggestions:")
		for _, s := range report.Suggestions {
			_, _ = fmt.Fprintf(out, "    - [%s] %s\n", s.Risk, s.Title)
			if s.Description != "" {
				_, _ = fmt.Fprintf(out, "      %s\n", s.Description)
			}
		}
	}

	return nil
}
