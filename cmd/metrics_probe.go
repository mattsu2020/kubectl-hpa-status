package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newMetricsCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Metrics adapter diagnostics",
	}
	cmd.AddCommand(newMetricsProbeCommand(opts))
	cmd.AddCommand(newMetricsContractCommand(opts))
	return cmd
}

func newMetricsProbeCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "probe NAME [NAME...]",
		Short:             "Probe custom/external metrics adapter health for an HPA",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMetricsProbe(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runMetricsProbe(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := applyCommandPreset(opts, presetMetricsProbe)
	return runStatusMany(ctx, out, &local, names, !local.NoInterpret)
}
