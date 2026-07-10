// Package cmd implements the CLI commands for kubectl-hpa-status.
package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

var (
	// version is the plugin version. Overridden via -ldflags at release time
	// (see .goreleaser.yml). The default reflects the v2.0 development line.
	// When left at the defaults (e.g. `go install`), buildVersion falls back
	// to the Go build info embedded by the toolchain.
	version = defaultVersion
	commit  = defaultCommit
	date    = defaultDate
)

// NewRootCommand creates and returns the root cobra command for kubectl-hpa-status.
func NewRootCommand() *cobra.Command {
	opts := &options{}
	*opts = defaultRootOptions()

	root := &cobra.Command{
		Use:           "kubectl-hpa-status",
		Short:         "Inspect HorizontalPodAutoscaler status",
		Version:       buildVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return hpaNameCompletion(opts)(cmd, args, toComplete)
		},
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			opts.Err = cmd.ErrOrStderr()
			if err := applyConfigDefaults(cmd, opts); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
			}
			if err := applyHealthWeightOverrides(opts); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
			}
			opts.Normalize()
			opts.In = cmd.InOrStdin()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			includeInterpretation := (opts.Interpret || opts.Explain || opts.Suggest) && !opts.NoInterpret
			if opts.Watch.Watch {
				if len(args) != 1 {
					return fmt.Errorf("--watch supports exactly one HPA name")
				}
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], includeInterpretation)
			}
			return runStatusMany(cmd.Context(), cmd.OutOrStdout(), opts, args, includeInterpretation)
		},
	}

	registerCommonFlags(root, opts)
	registerWatchFlags(root, opts)
	registerFlagCompletions(root, opts)

	registerCommands(root, opts)

	// alpha groups operational/experimental commands (policy, gitops, bundles,
	// capacity planning, record analysis). As of v2.0 these live exclusively
	// under the alpha path; the historical top-level aliases have been removed.
	root.AddCommand(newAlphaCommand(opts))

	_ = root.MarkPersistentFlagFilename("kubeconfig")
	_ = root.MarkPersistentFlagFilename("config")

	return root
}

// commandGroup pairs a cobra help group with the constructors of the
// subcommands that belong to it, so the root --help output presents the ~40
// subcommands by workflow instead of one flat alphabetical list.
type commandGroup struct {
	group    cobra.Group
	builders []func(opts *options) *cobra.Command
}

// commandGroups is the registry of subcommands attached to the root command.
// Add a new subcommand by appending its constructor to the group matching its
// workflow. Most constructors share the (opts) signature; the few that need no
// options (alerts, version) use a thin adapter. Commands outside every group
// (version, completion, alpha, help) appear under "Additional Commands".
var commandGroups = []commandGroup{
	{
		group: cobra.Group{ID: "diagnose", Title: "Diagnose Commands:"},
		builders: []func(opts *options) *cobra.Command{
			newStatusCommand,
			newDoctorCommand,
			newReadinessDoctorCommand,
			newWhyNotScaleCommand,
			newBlockersCommand,
			newTraceCommand,
			newPathCommand,
			newReadinessCommand,
			newRolloutCommand,
			newRolloutContextCommand,
			newNodeContextCommand,
			newMetricsCommand,
			newExplainCommand,
		},
	},
	{
		group: cobra.Group{ID: "fleet", Title: "Fleet Overview Commands:"},
		builders: []func(opts *options) *cobra.Command{
			newListCommand,
			newScanCommand,
			newFleetCommand,
			newCompareCommand,
			newOwnershipCommand,
		},
	},
	{
		group: cobra.Group{ID: "monitor", Title: "Monitoring & History Commands:"},
		builders: []func(opts *options) *cobra.Command{
			newWatchCommand,
			newTUICommand,
			newTimelineCommand,
			newHistoryCommand,
			newRecordCommand,
			newReplayCommand,
		},
	},
	{
		group: cobra.Group{ID: "tune", Title: "Tuning & Planning Commands:"},
		builders: []func(opts *options) *cobra.Command{
			newAdvisorCommand,
			newContainerAdvisorCommand,
			newTuneCommand,
			newBehaviorCommand,
			newSimulateCommand,
			newEstimateCommand,
			newRecommendCommand,
			newPreflightCommand,
			newAssumptionsCommand,
			newProfileCommand,
			newSLOCommand,
		},
	},
	{
		group: cobra.Group{ID: "integrate", Title: "Integration & Export Commands:"},
		builders: []func(opts *options) *cobra.Command{
			newExportCommand,
			newSnapshotCommand,
			func(*options) *cobra.Command { return newAlertsCommand() },
			newLintCommand,
			newCompatCommand,
		},
	},
}

// registerCommands attaches the grouped subcommands to root. The completion
// and version commands are wired separately: completion needs the root itself,
// and both belong under "Additional Commands" rather than a workflow group.
func registerCommands(root *cobra.Command, opts *options) {
	for _, cg := range commandGroups {
		root.AddGroup(&cg.group)
		for _, build := range cg.builders {
			sub := build(opts)
			sub.GroupID = cg.group.ID
			root.AddCommand(sub)
		}
	}
	root.AddCommand(newVersionCommand())
	root.AddCommand(newCompletionCommand(root))
}

func buildVersion() string {
	v, c, d := resolveBuildInfo(version, commit, date, debug.ReadBuildInfo)
	return fmt.Sprintf("%s (commit: %s, built: %s)", v, c, d)
}

// newVersionCommand prints version and build metadata.
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and build metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "kubectl-hpa-status version %s\n", buildVersion())
			return err
		},
	}
}
