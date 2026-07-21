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
	// Incident bundles leave the operator's machine by definition, so redaction
	// is on by default. --redact=false remains for trusted local archives.
	cmd.Flags().Bool("redact", true, "redact sensitive information; use --redact=false only for trusted local archives")
	return cmd
}

func runIncidentBundle(ctx context.Context, out io.Writer, opts *options, name, format, outputPath string, redact bool) error {
	local := applyCommandPreset(opts, presetIncidentBundle)
	return runBundle(ctx, out, &local, name, format, outputPath, redact)
}
