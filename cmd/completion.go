package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
		AllNamespaces: func() bool { return opts.AllNamespaces },
		Kubeconfig:    func() string { return opts.Kubeconfig },
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
func registerFlagCompletions(root *cobra.Command, opts *options) error {
	if err := completion.RegisterFlagCompletions(root, completionDeps(opts)); err != nil {
		return err
	}
	seen := make(map[*pflag.Flag]struct{})
	var visit func(*cobra.Command) error
	visit = func(cmd *cobra.Command) error {
		flag := cmd.LocalNonPersistentFlags().Lookup("analysis-profile")
		if flag == nil {
			flag = cmd.PersistentFlags().Lookup("analysis-profile")
		}
		if flag != nil {
			if _, ok := seen[flag]; !ok {
				if err := cmd.RegisterFlagCompletionFunc("analysis-profile", analysisProfileCompletions); err != nil {
					return err
				}
				seen[flag] = struct{}{}
			}
		}
		for _, child := range cmd.Commands() {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root)
}
