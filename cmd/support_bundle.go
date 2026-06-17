package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newSupportBundleCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "support-bundle NAME",
		Aliases:           []string{"sb"},
		Short:             "Create an HPA incident investigation bundle with KEDA/VPA enrichment",
		Long:              "Collects HPA configuration, status analysis, workload details, events, metrics diagnostics, capacity context, KEDA ScaledObject, VPA recommendations, quotas, PDBs, and node capacity into a single evidence pack for incident handoff, Slack, GitHub issues, or SRE escalation.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, output, redact := readBundleFlags(cmd)
			return runSupportBundle(cmd.Context(), cmd.OutOrStdout(), opts, args[0], format, output, redact)
		},
	}
	addBundleFlags(cmd, "hpa-support-bundle-<name>-<timestamp>.{md|zip}")
	return cmd
}

func runSupportBundle(ctx context.Context, out io.Writer, opts *options, name, format, outputPath string, redact bool) error {
	local := applyCommandPreset(opts, presetSupportBundle)

	if format == "" {
		format = "markdown"
	}
	outputPath = defaultBundleOutputPath(outputPath, name, format, "hpa-support-bundle")
	return runBundle(ctx, out, &local, name, format, outputPath, redact)
}
