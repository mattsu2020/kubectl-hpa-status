package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newTraceCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "trace NAME [NAME...]",
		Short:             "Show a step-by-step visible HPA decision trace",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrace(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runTrace(ctx context.Context, out io.Writer, opts *options, names []string) error {
	return runStatusWithPreset(ctx, out, opts, presetTrace, names)
}
