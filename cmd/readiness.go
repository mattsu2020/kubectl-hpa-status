package cmd

import (
	"context"
	"io"

	"github.com/mattsui2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/spf13/cobra"
)

func newReadinessCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "readiness NAME [NAME...]",
		Short:             "Explain readiness and rollout impact on HPA scaling",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReadiness(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runReadiness(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := applyCommandPreset(opts, cmdoptions.PresetReadiness)
	return runStatusMany(ctx, out, &local, names, !local.NoInterpret)
}