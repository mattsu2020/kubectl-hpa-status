package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
)

func newAssumptionsCommand(opts *options) *cobra.Command {
	var (
		explain                bool
		assumeTolerance        string
		assumeSyncPeriod       string
		assumeCPUInitPeriod    string
		assumeInitialReadiness string
	)

	cmd := &cobra.Command{
		Use:               "assumptions NAME [NAME...]",
		Short:             "Show HPA controller-level assumptions with confidence levels",
		Long:              "Display the inferred or default kube-controller-manager parameters that affect HPA scaling decisions. Since the HPA API does not expose controller-manager flags, this command uses a combination of spec inspection, known defaults, and behavioral inference.",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssumptions(cmd.Context(), cmd.OutOrStdout(), opts, args, assumptionsFlagOverrides{
				explain:                explain,
				assumeTolerance:        assumeTolerance,
				assumeSyncPeriod:       assumeSyncPeriod,
				assumeCPUInitPeriod:    assumeCPUInitPeriod,
				assumeInitialReadiness: assumeInitialReadiness,
			})
		},
	}

	cmd.Flags().BoolVar(&explain, "explain", false, "show detailed explanations for each assumption")
	cmd.Flags().StringVar(&assumeTolerance, "assume-tolerance", "", "override tolerance value (e.g. 0.10)")
	cmd.Flags().StringVar(&assumeSyncPeriod, "assume-sync-period", "", "override HPA sync period (e.g. 15s)")
	cmd.Flags().StringVar(&assumeCPUInitPeriod, "assume-cpu-init-period", "", "override CPU initialization period (e.g. 5m)")
	cmd.Flags().StringVar(&assumeInitialReadiness, "assume-initial-readiness-delay", "", "override initial readiness delay (e.g. 30s)")

	return cmd
}

// assumptionsFlagOverrides holds the parsed CLI flag values for the assumptions command.
type assumptionsFlagOverrides struct {
	explain                bool
	assumeTolerance        string
	assumeSyncPeriod       string
	assumeCPUInitPeriod    string
	assumeInitialReadiness string
}

// assumptionsOutput wraps the controller assumptions for structured output.
type assumptionsOutput struct {
	Namespace   string                             `json:"namespace" yaml:"namespace"`
	Name        string                             `json:"name" yaml:"name"`
	Assumptions *hpaanalysis.ControllerAssumptions `json:"assumptions" yaml:"assumptions"`
}

func runAssumptions(ctx context.Context, out io.Writer, opts *options, names []string, flags assumptionsFlagOverrides) error {
	client, err := newClientOrDefault(opts)
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	// Build overrides from non-empty flag values.
	overrides := hpaanalysis.AssumptionOverrides{}
	if flags.assumeTolerance != "" {
		overrides.Tolerance = &flags.assumeTolerance
	}
	if flags.assumeSyncPeriod != "" {
		overrides.SyncPeriod = &flags.assumeSyncPeriod
	}
	if flags.assumeCPUInitPeriod != "" {
		overrides.CPUInitializationPeriod = &flags.assumeCPUInitPeriod
	}
	if flags.assumeInitialReadiness != "" {
		overrides.InitialReadinessDelay = &flags.assumeInitialReadiness
	}

	// Attempt to observe the controller-manager profile when --explain is set.
	var observed *hpaanalysis.ControllerProfile
	if flags.explain {
		observed = buildControllerProfile(ctx, client, opts.AssumeProfile, opts.ControllerProfileFile)
	}

	reports := make([]assumptionsOutput, 0, len(names))

	for _, name := range names {
		hpa, err := kube.GetHPAFromClient(ctx, client, name)
		if err != nil {
			return wrapHPALookupError(client.Namespace, name, err)
		}

		assumptions := hpaanalysis.DetectControllerAssumptionsWithOverrides(hpa, overrides, observed)
		reports = append(reports, assumptionsOutput{
			Namespace:   hpa.Namespace,
			Name:        hpa.Name,
			Assumptions: assumptions,
		})
	}

	format, templateStr := outputSelection(outputConfig{
		output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates,
	})

	for i, report := range reports {
		if i > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return fmt.Errorf("write assumptions separator: %w", err)
			}
		}
		if err := writeOutput(out, format, templateStr, report, func() error {
			return hpaanalysis.WriteAssumptionsTextWithExplain(out, report.Assumptions,
				flags.explain, style.NewTheme(shouldColorize(opts.Color, out)))
		}); err != nil {
			return fmt.Errorf("write assumptions report for %s/%s: %w", report.Namespace, report.Name, err)
		}
	}

	return nil
}
