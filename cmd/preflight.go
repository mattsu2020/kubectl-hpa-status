package cmd

import (
	"context"
	"io"

	"github.com/mattsui2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/spf13/cobra"
)

func newPreflightCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "preflight NAME [NAME...]",
		Short:             "Validate capacity and workload prerequisites before raising HPA limits",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPreflight(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runPreflight(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := applyCommandPreset(opts, cmdoptions.PresetPreflight)
	return runStatusMany(ctx, out, &local, names, !local.NoInterpret)
}