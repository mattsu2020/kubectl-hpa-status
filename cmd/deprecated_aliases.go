package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// v3 grouped the focused diagnosis preset commands under their workflow
// parents (doctor, advisor). The historical top-level names remain working as
// hidden deprecated aliases for the whole v3 line: each prints a one-line
// migration hint on stderr and then runs the exact same command body. They
// will be removed in the next major release.

// deprecatedAliasSpec describes one hidden top-level alias: the historical
// name, its replacement invocation, an optional equivalence hint, and the
// shared run function that both the alias and the grouped command call.
type deprecatedAliasSpec struct {
	name        string
	replacement string
	hint        string
	run         func(ctx context.Context, out io.Writer, opts *options, names []string) error
}

func newDeprecatedPresetAlias(opts *options, spec deprecatedAliasSpec) *cobra.Command {
	hint := ""
	if spec.hint != "" {
		hint = " " + spec.hint
	}
	return &cobra.Command{
		Use:   spec.name + " NAME [NAME...]",
		Short: fmt.Sprintf("Deprecated: use '%s' instead", spec.replacement),
		// Hidden so the deprecated names do not clutter --help; they keep
		// working (and stay discoverable via `help <name>`) for the v3 line.
		Hidden: true,
		Args:   cobra.MinimumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return hpaNameCompletion(opts)(cmd, args, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Best-effort hint; a closed stderr must not block the command.
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: '%s' is deprecated and will be removed in a future release; use '%s' instead%s\n",
				spec.name, spec.replacement, hint)
			return spec.run(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func newDeprecatedReadinessAlias(opts *options) *cobra.Command {
	return newDeprecatedPresetAlias(opts, deprecatedAliasSpec{
		name:        "readiness",
		replacement: "doctor readiness",
		run:         runReadiness,
	})
}

func newDeprecatedRolloutContextAlias(opts *options) *cobra.Command {
	return newDeprecatedPresetAlias(opts, deprecatedAliasSpec{
		name:        "rollout-context",
		replacement: "doctor rollout",
		run:         runRolloutContext,
	})
}

func newDeprecatedNodeContextAlias(opts *options) *cobra.Command {
	return newDeprecatedPresetAlias(opts, deprecatedAliasSpec{
		name:        "node-context",
		replacement: "doctor capacity",
		run:         runNodeContext,
	})
}

func newDeprecatedTraceAlias(opts *options) *cobra.Command {
	return newDeprecatedPresetAlias(opts, deprecatedAliasSpec{
		name:        "trace",
		replacement: "doctor trace",
		hint:        "(equivalent to 'status --decision-trace')",
		run:         runTrace,
	})
}

func newDeprecatedPathAlias(opts *options) *cobra.Command {
	return newDeprecatedPresetAlias(opts, deprecatedAliasSpec{
		name:        "path",
		replacement: "doctor path",
		hint:        "(equivalent to 'status --scale-path')",
		run:         runPath,
	})
}

func newDeprecatedPreflightAlias(opts *options) *cobra.Command {
	return newDeprecatedPresetAlias(opts, deprecatedAliasSpec{
		name:        "preflight",
		replacement: "doctor preflight",
		run:         runPreflight,
	})
}

func newDeprecatedContainerAdvisorAlias(opts *options) *cobra.Command {
	return newDeprecatedPresetAlias(opts, deprecatedAliasSpec{
		name:        "container-advisor",
		replacement: "advisor container",
		run:         runContainerAdvisor,
	})
}
