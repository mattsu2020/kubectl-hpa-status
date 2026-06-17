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

// runSupportBundle orchestrates data collection for the support-bundle command.
// It delegates to the existing bundle infrastructure with KEDA and VPA flags
// force-enabled, and appends KEDA ScaledObject and VPA recommendation data
// to the bundle.
func runSupportBundle(ctx context.Context, out io.Writer, opts *options, name, format, outputPath string, redact bool) error {
	// Force-enable KEDA and VPA enrichment for support bundles.
	local := copyOptions(opts)
	local.keda = "on"
	local.vpa = "on"
	local.features.readinessImpact = true
	local.features.rolloutImpact = true
	local.features.scaleoutBlockers = true
	local.features.controllerProfile = true
	local.features.capacityDeep = true
	local.features.diagnoseMetrics = true
	local.features.metricsFreshness = true
	local.features.metricContract = true
	local.features.churnDetect = true
	local.features.metricHints = true
	local.features.containerAdvisor = true
	local.features.behaviorAdvisor = true

	if format == "" {
		format = "markdown"
	}

	outputPath = defaultBundleOutputPath(outputPath, name, format, "hpa-support-bundle")

	// Delegate to the existing bundle infrastructure.
	return runBundle(ctx, out, &local, name, format, outputPath, redact)
}
