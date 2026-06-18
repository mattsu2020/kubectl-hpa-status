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
	local := applyCommandPreset(opts, presetDoctor)
	return runStatusMany(ctx, out, &local, names, !local.NoInterpret)
}
