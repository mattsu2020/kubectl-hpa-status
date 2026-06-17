package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newDoctorCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "doctor NAME [NAME...]",
		Short:             "Diagnose HPA scaling failures across metrics, workload, pods, resources, events, and KEDA",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
	cmd.Flags().Bool("startup", false, "include startup/readiness probe impact checks (enabled by doctor)")
	cmd.Flags().Bool("startup-context", false, "include startup/readiness probe impact checks (enabled by doctor)")
	return cmd
}

func runDoctor(ctx context.Context, out io.Writer, opts *options, names []string) error {
	// Enable all diagnostic flags for a full doctor check. Take a shallow copy
	// so the shared process-wide opts is not mutated.
	local := copyOptions(opts)
	local.features.explain = true
	local.features.diagnoseMetrics = true
	local.features.metricsFreshness = true
	local.features.checkResources = true
	local.features.explainPods = true
	local.features.capacityContext = true
	local.features.gitopsCheck = true
	local.features.metricContract = true
	local.features.churnDetect = true
	local.features.metricHints = true
	local.features.containerAdvisor = true
	local.features.behaviorAdvisor = true
	local.features.capacityDeep = true
	local.features.rollout = true
	local.features.readinessImpact = true
	local.features.scalePath = true
	local.features.flappingAdvisor = true
	local.features.trendAnomaly = true
	local.features.adapterDiagnostics = true

	return runStatusMany(ctx, out, &local, names, !local.features.noInterpret)
}
