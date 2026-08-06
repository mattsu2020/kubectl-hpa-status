package cmd

import (
	"github.com/spf13/cobra"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/completion"
)

// This file is a thin facade over the completion logic, which lives in
// cmd/internal/completion. The cmd-level symbols keep the historical names so
// the ~40 ValidArgsFunction call sites and the completion tests continue to
// compile unchanged. Each function bridges the mutable *options struct into
// the narrow completion.Deps the package needs.

// completionDeps adapts opts to the completion package's dependency slice.
func completionDeps(opts *options) completion.Deps {
	return completion.Deps{
		NewClient:     opts.NewClient,
		AllNamespaces: opts.AllNamespaces,
		Kubeconfig:    opts.Kubeconfig,
	}
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return completion.ShellCommand(root)
}

func hpaNameCompletion(opts *options) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return completion.HpaName(completionDeps(opts))
}

func namespaceCompletions(opts *options) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return completion.Namespace(completionDeps(opts))
}

func contextCompletions(opts *options) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return completion.Context(completionDeps(opts))
}

// Static completers delegate to the canonical value lists in the completion
// package, preserving the historical signature used by tests.
func outputCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completion.Output(cmd, args, toComplete)
}
func filterCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completion.Filter(cmd, args, toComplete)
}
func sortByCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completion.SortBy(cmd, args, toComplete)
}
func colorCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completion.Color(cmd, args, toComplete)
}
func langCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completion.Lang(cmd, args, toComplete)
}
func eventsCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completion.Events(cmd, args, toComplete)
}
func untilConditionCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completion.UntilCondition(cmd, args, toComplete)
}

// registerFlagCompletions registers shell completions for all flags with known
// values. analysis-profile stays here because its vocabulary lives in
// internal/cmdoptions.
func registerFlagCompletions(root *cobra.Command, opts *options) {
	completion.RegisterFlagCompletions(root, completionDeps(opts))
	_ = root.RegisterFlagCompletionFunc("analysis-profile", analysisProfileCompletions)
}
