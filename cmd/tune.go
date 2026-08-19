package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type tuneReport struct {
	Namespace         string   `json:"namespace" yaml:"namespace"`
	Name              string   `json:"name" yaml:"name"`
	Goal              string   `json:"goal" yaml:"goal"`
	Findings          []string `json:"findings" yaml:"findings"`
	SuggestedBehavior string   `json:"suggestedBehavior" yaml:"suggestedBehavior"`
	Risks             []string `json:"risks" yaml:"risks"`
}

func newTuneCommand(opts *options) *cobra.Command {
	var goal string
	var suggest bool
	cmd := &cobra.Command{
		Use:               "tune NAME",
		Short:             "Advise HPA behavior, stabilization, and tolerance settings",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateMode("--goal", goal, "stable", "fast-scale-up", "cost-saving"); err != nil {
				return err
			}
			return runTune(cmd.Context(), cmd.OutOrStdout(), opts, args[0], goal, suggest)
		},
	}
	cmd.Flags().StringVar(&goal, "goal", "stable", "tuning goal: stable, fast-scale-up, or cost-saving")
	cmd.Flags().BoolVar(&suggest, "suggest", false, "print suggested behavior YAML")
	return cmd
}

func runTune(ctx context.Context, out io.Writer, opts *options, name, goal string, suggest bool) error {
	_, hpa, err := lookupHPA(ctx, opts, name)
	if err != nil {
		return err
	}
	report := tuneReport{
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
		Goal:      goal,
		Risks: []string{
			"Validate with server-side dry-run before applying.",
			"behavior.scaleUp/scaleDown.tolerance requires Kubernetes support for HPA configurable tolerance.",
		},
	}
	if hpa.Spec.Behavior == nil {
		report.Findings = append(report.Findings, "spec.behavior is not configured")
	} else {
		if hpa.Spec.Behavior.ScaleDown == nil {
			report.Findings = append(report.Findings, "scaleDown behavior is not configured")
		}
		if hpa.Spec.Behavior.ScaleUp == nil {
			report.Findings = append(report.Findings, "scaleUp behavior is not configured")
		}
	}
	report.SuggestedBehavior = suggestedBehaviorForGoal(goal)
	if !suggest {
		report.SuggestedBehavior = ""
	}
	return renderWithOutput(out, opts, report, func(out io.Writer) error {
		return writeTuneText(out, report)
	})

}

// writeTuneText renders the tune report as plain text, propagating write
// errors per the command-layer convention.
func writeTuneText(out io.Writer, report tuneReport) error {
	if _, err := fmt.Fprintf(out, "HPA Tuning Advisor: %s/%s\n\nGoal: %s\n\nFindings:\n", report.Namespace, report.Name, report.Goal); err != nil {
		return fmt.Errorf("write tune report: %w", err)
	}
	if len(report.Findings) == 0 {
		if _, err := fmt.Fprintln(out, "- behavior is configured; review current policy against workload goal"); err != nil {
			return fmt.Errorf("write tune report: %w", err)
		}
	}
	for _, finding := range report.Findings {
		if _, err := fmt.Fprintf(out, "- %s\n", finding); err != nil {
			return fmt.Errorf("write tune report: %w", err)
		}
	}
	if report.SuggestedBehavior != "" {
		if _, err := fmt.Fprintf(out, "\nSuggested behavior:\n%s\n", report.SuggestedBehavior); err != nil {
			return fmt.Errorf("write tune report: %w", err)
		}
	}
	if _, err := fmt.Fprintln(out, "\nRisk:"); err != nil {
		return fmt.Errorf("write tune report: %w", err)
	}
	for _, risk := range report.Risks {
		if _, err := fmt.Fprintf(out, "- %s\n", risk); err != nil {
			return fmt.Errorf("write tune report: %w", err)
		}
	}
	return nil
}

func suggestedBehaviorForGoal(goal string) string {
	switch goal {
	case "fast-scale-up":
		return `scaleUp:
  tolerance: 0.05
  stabilizationWindowSeconds: 0
  policies:
  - type: Percent
    value: 200
    periodSeconds: 60
scaleDown:
  tolerance: 0.10
  stabilizationWindowSeconds: 300
  policies:
  - type: Percent
    value: 50
    periodSeconds: 60`
	case "cost-saving":
		return `scaleUp:
  tolerance: 0.10
  policies:
  - type: Percent
    value: 100
    periodSeconds: 60
scaleDown:
  tolerance: 0.05
  stabilizationWindowSeconds: 180
  policies:
  - type: Percent
    value: 100
    periodSeconds: 60`
	default:
		return `scaleUp:
  tolerance: 0.05
  policies:
  - type: Percent
    value: 100
    periodSeconds: 60
scaleDown:
  tolerance: 0.10
  stabilizationWindowSeconds: 300
  policies:
  - type: Percent
    value: 50
    periodSeconds: 60`
	}
}
