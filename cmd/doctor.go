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
	local := *opts
	local.explain = true
	local.diagnoseMetrics = true
	local.metricsFreshness = true
	local.checkResources = true
	local.explainPods = true
	local.capacityContext = true
	local.gitopsCheck = true
	local.metricContract = true
	local.churnDetect = true
	local.metricHints = true
	local.containerAdvisor = true
	local.behaviorAdvisor = true
	local.capacityDeep = true
	local.rollout = true
	local.readinessImpact = true
	local.scalePath = true
	local.flappingAdvisor = true
	local.trendAnomaly = true
	local.adapterDiagnostics = true

	return runStatusMany(ctx, out, &local, names, !local.noInterpret)
}
