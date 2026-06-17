package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newNodeContextCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "node-context NAME [NAME...]",
		Short:             "Explain node, scheduler, quota, and autoscaler context behind HPA scale-out",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNodeContext(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runNodeContext(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := copyOptions(opts)
	local.features.explain = true
	local.features.explainPods = true
	local.features.capacityContext = true
	local.features.capacityHeadroom = true
	local.features.capacityDeep = true
	local.features.scalePath = true
	local.features.scaleoutBlockers = true
	local.features.nodeAutoscaler = true
	local.features.karpenter = true
	local.events.enabled = true
	if local.events.limit == 0 {
		local.events.limit = 10
	}
	return runStatusMany(ctx, out, &local, names, !local.features.noInterpret)
}

func newRolloutContextCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "rollout-context NAME [NAME...]",
		Short:             "Explain rollout, ReplicaSet, and readiness context behind HPA behavior",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRolloutContext(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runRolloutContext(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := copyOptions(opts)
	local.features.explain = true
	local.features.explainPods = true
	local.features.readinessImpact = true
	local.features.rollout = true
	local.features.rolloutImpact = true
	local.features.scalePath = true
	local.events.enabled = true
	if local.events.limit == 0 {
		local.events.limit = 10
	}
	return runStatusMany(ctx, out, &local, names, !local.features.noInterpret)
}
