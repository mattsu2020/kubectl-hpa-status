package cmd

import (
	"github.com/spf13/cobra"
)

// alphaCommands lists the subcommands that have been grouped under the
// `alpha` parent. These are operational and experimental commands (bundle
// generation, policy enforcement, GitOps linting, capacity planning, record
// analysis) that advanced operators use and casual users do not need in the
// top-level --help output. As of v2.0 these commands live exclusively under
// the alpha path.
//
// Each entry pairs a command constructor (called fresh to produce an
// independent *cobra.Command instance for the alpha tree) with a category
// ("operational" or "experimental") for the alpha --help grouping.
type alphaCommandSpec struct {
	constructor func(*options) *cobra.Command
	category    string // "operational" or "experimental", for the alpha --help grouping
}

// alphaCommandSpecs is the registry of commands under `alpha`. Add new
// operational/experimental commands here rather than at the root.
var alphaCommandSpecs = []alphaCommandSpec{
	// --- Operational: apply-time gating, bundle generation, GitOps ---
	{newPolicyCommand, "operational"},
	{newGitOpsCommand, "operational"},
	{newBundleCommand, "operational"},
	{newIncidentBundleCommand, "operational"},
	{newSupportBundleCommand, "operational"},
	// --- Experimental: capacity planning, record analysis, niche investigation ---
	{newCapacityGapCommand, "experimental"},
	{newCapacityPlanCommand, "experimental"},
	{newAutoscalerMapCommand, "experimental"},
	{newAnalyzeRecordCommand, "experimental"},
	{newFlapCommand, "experimental"},
}

// newAlphaCommand creates the `alpha` parent command grouping operational and
// experimental subcommands. Each child is a fresh command instance. As of v2.0
// these commands live exclusively under the alpha path; the historical
// top-level aliases have been removed.
func newAlphaCommand(opts *options) *cobra.Command {
	alpha := &cobra.Command{
		Use:   "alpha",
		Short: "Operational and experimental commands (policy, gitops, bundles, capacity, records)",
		Long: `Grouped home for operational and experimental subcommands.

Operational commands (policy, gitops, bundle, incident-bundle, support-bundle)
govern apply-time gating, GitOps drift, and support-data collection.

Experimental commands (capacity, capacity-gap, autoscaler-map, analyze-record,
flap) are niche investigation tools that may change between releases.
`,
		// alpha itself is not hidden; it surfaces in --help so users discover the
		// grouped commands.
	}
	for _, spec := range alphaCommandSpecs {
		alpha.AddCommand(spec.constructor(opts))
	}
	return alpha
}
