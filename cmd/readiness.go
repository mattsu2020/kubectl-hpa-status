package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newReadinessCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "readiness NAME [NAME...]",
		Aliases:           []string{"startup"},
		Short:             "Analyze readiness, startup, and not-yet-ready pod impact on HPA decisions",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReadiness(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
	cmd.AddCommand(newReadinessDoctorCommand(opts))
	return cmd
}

func runReadiness(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := *opts
	local.explain = true
	local.readinessImpact = true
	local.explainPods = true
	local.scalePath = true
	local.rolloutImpact = true
	local.metricsFreshness = true
	local.controllerProfile = true
	local.events.enabled = true
	if local.events.limit == 0 {
		local.events.limit = 10
	}
	return runStatusMany(ctx, out, &local, names, !local.noInterpret)
}
