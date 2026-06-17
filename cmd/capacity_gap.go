package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newCapacityGapCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "capacity-gap NAME [NAME...]",
		Short:             "Compare HPA desired replicas with ready pods and visible serving capacity",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCapacityGap(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runCapacityGap(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := copyOptions(opts)
	local.explain = true
	local.explainPods = true
	local.readinessImpact = true
	local.capacityHeadroom = true
	local.capacityDeep = true
	local.scalePath = true
	local.scaleoutBlockers = true
	local.events.enabled = true
	if local.events.limit == 0 {
		local.events.limit = 10
	}
	return runStatusMany(ctx, out, &local, names, !local.noInterpret)
}
