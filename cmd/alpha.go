package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// alphaCommands lists the subcommands that have been grouped under the
// `alpha` parent. These are operational and experimental commands (bundle
// generation, policy enforcement, GitOps linting, capacity planning, record
// analysis) that advanced operators use and casual users do not need in the
// top-level --help output. The grouping is gradual: the commands remain
// runnable at their historical top-level path too (marked Deprecated), and
// the alpha path is the preferred location going forward. Removal of the
// top-level aliases is scheduled for v2.0 (see ROADMAP.md).
//
// Each entry pairs a command constructor (called fresh to produce an
// independent *cobra.Command instance for the alpha tree) with the top-level
// command name it deprecates, so the deprecation notice can point users at the
// new canonical path.
type alphaCommandSpec struct {
	constructor  func(*options) *cobra.Command
	topLevelName string // the root-level command being deprecated
	category     string // "operational" or "experimental", for the alpha --help grouping
}

// alphaCommandSpecs is the registry of commands moved under `alpha`. Add new
// operational/experimental commands here rather than at the root.
var alphaCommandSpecs = []alphaCommandSpec{
	// --- Operational: apply-time gating, bundle generation, GitOps ---
	{newPolicyCommand, "policy", "operational"},
	{newGitOpsCommand, "gitops", "operational"},
	{newBundleCommand, "bundle", "operational"},
	{newIncidentBundleCommand, "incident-bundle", "operational"},
	{newSupportBundleCommand, "support-bundle", "operational"},
	// --- Experimental: capacity planning, record analysis, niche investigation ---
	{newCapacityGapCommand, "capacity-gap", "experimental"},
	{newCapacityPlanCommand, "capacity", "experimental"},
	{newAutoscalerMapCommand, "autoscaler-map", "experimental"},
	{newAnalyzeRecordCommand, "analyze-record", "experimental"},
	{newFlapCommand, "flap", "experimental"},
}

// newAlphaCommand creates the `alpha` parent command grouping operational and
// experimental subcommands. Each child is a fresh command instance so the
// alpha tree is independent of the deprecated top-level aliases.
func newAlphaCommand(opts *options) *cobra.Command {
	alpha := &cobra.Command{
		Use:   "alpha",
		Short: "Operational and experimental commands (policy, gitops, bundles, capacity, records)",
		Long: `Grouped home for operational and experimental subcommands.

Operational commands (policy, gitops, bundle, incident-bundle, support-bundle)
govern apply-time gating, GitOps drift, and support-data collection.

Experimental commands (capacity, capacity-gap, autoscaler-map, analyze-record,
flap) are niche investigation tools that may change between releases.

These commands are also reachable at their historical top-level path, but those
aliases are deprecated and scheduled for removal in v2.0. Prefer the alpha path.
`,
		// alpha itself is not hidden; it surfaces in --help so users discover the
		// grouped commands without tripping over the deprecated top-level aliases.
	}
	for _, spec := range alphaCommandSpecs {
		alpha.AddCommand(spec.constructor(opts))
	}
	return alpha
}

// markAlphaAliasesDeprecated annotates the top-level copies of the alpha
// commands as deprecated in favor of the alpha path. The commands keep working
// (Cobra prints the deprecation message on invocation and exits 0 by default
// unless the RunE returns an error), so existing scripts are not broken. The
// aliases are scheduled for removal in v2.0.
//
// We walk the root's commands and match by Use name. The Deprecated string is
// what Cobra shows to the user, so it names the replacement explicitly.
func markAlphaAliasesDeprecated(root *cobra.Command) {
	for _, spec := range alphaCommandSpecs {
		name := commandFirstName(spec.topLevelName)
		cmd := findSubCommand(root, name)
		if cmd == nil {
			continue
		}
		cmd.Deprecated = fmt.Sprintf("use %q instead; this top-level alias is scheduled for removal in v2.0", "alpha "+name)
	}
}

// findSubCommand returns the direct child of root with the given first Use
// word, or nil. Cobra does not expose a public by-name lookup for non-runnable
// matching, so we scan Commands().
func findSubCommand(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if commandFirstName(c.Use) == name {
			return c
		}
	}
	return nil
}

// commandFirstName extracts the first whitespace-delimited token from a
// command's Use string, which is the command's invocation name (e.g.
// "bundle NAME" -> "bundle").
func commandFirstName(use string) string {
	for i, r := range use {
		if r == ' ' || r == '\t' {
			return use[:i]
		}
	}
	return use
}
