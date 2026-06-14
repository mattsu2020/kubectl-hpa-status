package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newIncidentBundleCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "incident-bundle NAME",
		Short:             "Collect an incident handoff evidence bundle for one HPA",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			output, _ := cmd.Flags().GetString("output")
			redact, _ := cmd.Flags().GetBool("redact")
			return runIncidentBundle(cmd.Context(), cmd.OutOrStdout(), opts, args[0], format, output, redact)
		},
	}
	cmd.Flags().String("format", "zip", "output format: markdown or zip")
	cmd.Flags().StringP("output", "o", "", "output file path")
	cmd.Flags().Bool("redact", false, "redact sensitive information")
	return cmd
}

func runIncidentBundle(ctx context.Context, out io.Writer, opts *options, name, format, outputPath string, redact bool) error {
	opts.readinessImpact = true
	opts.rolloutImpact = true
	opts.scaleoutBlockers = true
	opts.controllerProfile = true
	return runBundle(ctx, out, opts, name, format, outputPath, redact)
}
