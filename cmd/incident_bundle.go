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
			format, output, redact := readBundleFlags(cmd)
			return runIncidentBundle(cmd.Context(), cmd.OutOrStdout(), opts, args[0], format, output, redact)
		},
	}
	// Incident bundles default to zip (single handoff artifact) and leave the
	// operator's machine by definition, so the shared privacy-preserving
	// defaults apply (see addBundleFlags).
	addBundleFlags(cmd, "hpa-bundle-<name>-<timestamp>.{md|zip}", "zip")
	return cmd
}

func runIncidentBundle(ctx context.Context, out io.Writer, opts *options, name, format, outputPath string, redact bool) error {
	local := applyCommandPreset(opts, presetIncidentBundle)
	return runBundle(ctx, out, &local, name, format, outputPath, redact)
}
