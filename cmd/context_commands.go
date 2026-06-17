package cmd

import (
	"context"
	"io"

	"github.com/mattsui2020/kubectl-hpa-status/internal/cmdoptions"
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
	local := applyCommandPreset(opts, presetNodeContext, cmdoptions.CommandPresetOptions{
		Events: &cmdoptions.EventOption{Enabled: true, Limit: 10},
	})
	return runStatusMany(ctx, out, &local, names, !local.NoInterpret)
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
	local := applyCommandPreset(opts, presetRolloutContext, cmdoptions.CommandPresetOptions{
		Events: &cmdoptions.EventOption{Enabled: true, Limit: 10},
	})
	return runStatusMany(ctx, out, &local, names, !local.NoInterpret)
}