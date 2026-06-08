package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newAssumptionsCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "assumptions NAME [NAME...]",
		Short:             "Show HPA controller-level assumptions with confidence levels",
		Long:              "Display the inferred or default kube-controller-manager parameters that affect HPA scaling decisions. Since the HPA API does not expose controller-manager flags, this command uses a combination of spec inspection, known defaults, and behavioral inference.",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssumptions(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

// assumptionsOutput wraps the controller assumptions for structured output.
type assumptionsOutput struct {
	Namespace   string                            `json:"namespace" yaml:"namespace"`
	Name        string                            `json:"name" yaml:"name"`
	Assumptions *hpaanalysis.ControllerAssumptions `json:"assumptions" yaml:"assumptions"`
}

func runAssumptions(ctx context.Context, out io.Writer, opts *options, names []string) error {
	client, err := opts.newClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	reports := make([]assumptionsOutput, 0, len(names))

	for _, name := range names {
		hpa, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(client.Namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get HPA %s: %w", name, err)
		}

		assumptions := hpaanalysis.DetectControllerAssumptions(hpa)
		reports = append(reports, assumptionsOutput{
			Namespace:   hpa.Namespace,
			Name:        hpa.Name,
			Assumptions: assumptions,
		})
	}

	format, templateStr := outputSelection(outputConfig{
		output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates,
	})

	for i, report := range reports {
		if i > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if err := writeOutput(out, format, templateStr, report, func() error {
			return hpaanalysis.WriteAssumptionsText(out, report.Assumptions,
				style.NewTheme(shouldColorize(opts.color, out)))
		}); err != nil {
			return err
		}
	}

	return nil
}
