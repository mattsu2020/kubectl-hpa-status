package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
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
			return runTune(cmd.Context(), cmd.OutOrStdout(), opts, args[0], goal, suggest)
		},
	}
	cmd.Flags().StringVar(&goal, "goal", "stable", "tuning goal: stable, fast-scale-up, or cost-saving")
	cmd.Flags().BoolVar(&suggest, "suggest", false, "print suggested behavior YAML")
	return cmd
}

func runTune(ctx context.Context, out io.Writer, opts *options, name, goal string, suggest bool) error {
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}
	hpa, err := kube.GetHPAFromClient(ctx, client, name)
	if err != nil {
		return wrapHPALookupError(client.Namespace, name, err)
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
	format, templateStr := outputSelection(outputConfig{output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates})
	return writeOutput(out, format, templateStr, report, func() error {
		_, _ = fmt.Fprintf(out, "HPA Tuning Advisor: %s/%s\n\nGoal: %s\n\nFindings:\n", report.Namespace, report.Name, report.Goal)
		if len(report.Findings) == 0 {
			_, _ = fmt.Fprintln(out, "- behavior is configured; review current policy against workload goal")
		}
		for _, finding := range report.Findings {
			_, _ = fmt.Fprintf(out, "- %s\n", finding)
		}
		if report.SuggestedBehavior != "" {
			_, _ = fmt.Fprintf(out, "\nSuggested behavior:\n%s\n", report.SuggestedBehavior)
		}
		_, _ = fmt.Fprintln(out, "\nRisk:")
		for _, risk := range report.Risks {
			_, _ = fmt.Fprintf(out, "- %s\n", risk)
		}
		return nil
	})
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
