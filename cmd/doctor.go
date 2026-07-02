package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newDoctorCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "doctor NAME [NAME...]",
		Short:             "Diagnose HPA scaling failures across metrics, workload, pods, resources, events, and KEDA",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runDoctor(ctx context.Context, out io.Writer, opts *options, names []string) error {
	return runStatusWithPreset(ctx, out, opts, presetDoctor, names)
}
