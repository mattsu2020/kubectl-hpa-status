// Package cmd implements the CLI commands for kubectl-hpa-status.
package cmd

import (
	"fmt"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
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
	return NewRootCommandWithDeps(AppDeps{})
}

// AppDeps contains process-boundary dependencies used by the command tree.
// Production callers use zero values; tests and embedders can inject the same
// tree with a fake Kubernetes client instead of reconstructing Cobra wiring.
type AppDeps struct {
	Kubernetes kubernetes.Interface
	Now        func() time.Time
}

// NewRootCommandWithDeps creates the production command tree with injectable
// external dependencies.
func NewRootCommandWithDeps(deps AppDeps) *cobra.Command {
	opts := &options{}
	*opts = defaultRootOptions()
	opts.ClientOverride = deps.Kubernetes
	opts.Now = deps.Now

	root := &cobra.Command{
		Use:               "kubectl-hpa-status",
		Short:             "Inspect HorizontalPodAutoscaler status",
		Version:           buildVersion(),
		SilenceUsage:      true,
		SilenceErrors:     true,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: rootValidArgsFunction(opts),
		PersistentPreRunE: rootPersistentPreRunE(opts),
		RunE:              rootRunE(opts),
	}

	registerCommonFlags(root, opts)

	// The workflow and watch groups are attached per command instead of being
	// inherited from root, so leaf utilities (version, completion, help) reject
	// flags they never act on. Root itself gets both because it runs status
	// implicitly for `kubectl hpa_status NAME`.
	workflowFlags := newWorkflowFlagSet(opts)
	watchFlags := newWatchFlagSet(opts)
	addSharedFlags(root, workflowFlags, watchFlags)

	registerCommands(root, opts, workflowFlags, watchFlags)

	// alpha groups operational/experimental commands (policy, gitops, bundles,
	// capacity planning, record analysis). As of v2.0 these live exclusively
	// under the alpha path; the historical top-level aliases have been removed.
	alpha := newAlphaCommand(opts)
	root.AddCommand(alpha)
	if err := registerFlagCompletions(root, opts); err != nil {
		panic(fmt.Sprintf("register flag completions: %v", err))
	}

	_ = root.MarkPersistentFlagFilename("kubeconfig")
	_ = root.MarkPersistentFlagFilename("config")

	return root
}

func rootValidArgsFunction(opts *options) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return hpaNameCompletion(opts)(cmd, args, toComplete)
	}
}

func rootPersistentPreRunE(opts *options) func(cmd *cobra.Command, _ []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		opts.Err = cmd.ErrOrStderr()
		if err := applyConfigDefaults(cmd, opts); err != nil {
			return err
		}
		if err := applyHealthWeightOverrides(opts); err != nil {
			return err
		}
		opts.Normalize()
		applyStatusDepthDefaults(cmd, opts)
		opts.In = cmd.InOrStdin()
		return validateEffectiveOptions(cmd, opts)
	}
}

func rootRunE(opts *options) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		request := snapshotStatusRequest(opts, args)
		return executeStatusRequest(cmd.Context(), cmd.OutOrStdout(), request)
	}
}

// commandGroup pairs a cobra help group with the constructors of the
// subcommands that belong to it, so the root --help output presents the ~40
// subcommands by workflow instead of one flat alphabetical list.
type commandGroup struct {
	group    cobra.Group
	commands []commandSpec
}

// commandSpec is the single registration point for one root command.
type commandSpec struct {
	build      func(opts *options) *cobra.Command
	capability commandCapability
}

func rootCommand(build func(*options) *cobra.Command) commandSpec { return commandSpec{build: build} }

func workflowCommand(build func(*options) *cobra.Command, watch bool) commandSpec {
	return commandSpec{build: build, capability: commandCapability{workflowFlags: true, watchFlags: watch}}
}

// commandGroups is the registry of subcommands attached to the root command.
// Add a new subcommand by appending its constructor to the group matching its
// workflow. Most constructors share the (opts) signature; the few that need no
// options (alerts, version) use a thin adapter. Commands outside every group
// (version, completion, alpha, help) appear under "Additional Commands".
var commandGroups = []commandGroup{
	{
		group: cobra.Group{ID: "diagnose", Title: "Diagnose Commands:"},
		commands: []commandSpec{
			workflowCommand(newStatusCommand, true),
			rootCommand(newDoctorCommand), rootCommand(newReadinessDoctorCommand),
			rootCommand(newWhyNotScaleCommand), rootCommand(newBlockersCommand),
			rootCommand(newDeprecatedTraceAlias), rootCommand(newDeprecatedPathAlias),
			rootCommand(newDeprecatedReadinessAlias), rootCommand(newRolloutCommand),
			rootCommand(newDeprecatedRolloutContextAlias), rootCommand(newDeprecatedNodeContextAlias),
			rootCommand(newMetricsCommand), rootCommand(newExplainCommand),
		},
	},
	{
		group: cobra.Group{ID: "fleet", Title: "Fleet Overview Commands:"},
		commands: []commandSpec{
			workflowCommand(newListCommand, false), workflowCommand(newScanCommand, false),
			rootCommand(newFleetCommand), rootCommand(newCompareCommand), rootCommand(newOwnershipCommand),
		},
	},
	{
		group: cobra.Group{ID: "monitor", Title: "Monitoring & History Commands:"},
		commands: []commandSpec{
			workflowCommand(newWatchCommand, true), workflowCommand(newTUICommand, true),
			rootCommand(newTimelineCommand), rootCommand(newHistoryCommand),
			rootCommand(newRecordCommand), rootCommand(newReplayCommand),
		},
	},
	{
		group: cobra.Group{ID: "tune", Title: "Tuning & Planning Commands:"},
		commands: []commandSpec{
			rootCommand(newAdvisorCommand), rootCommand(newDeprecatedContainerAdvisorAlias),
			rootCommand(newTuneCommand), rootCommand(newBehaviorCommand),
			rootCommand(newSimulateCommand), rootCommand(newEstimateCommand),
			rootCommand(newRecommendCommand), rootCommand(newDeprecatedPreflightAlias),
			rootCommand(newAssumptionsCommand), rootCommand(newProfileCommand), rootCommand(newSLOCommand),
		},
	},
	{
		group: cobra.Group{ID: "integrate", Title: "Integration & Export Commands:"},
		commands: []commandSpec{
			rootCommand(newExportCommand), rootCommand(newSnapshotCommand),
			rootCommand(func(*options) *cobra.Command { return newAlertsCommand() }),
			rootCommand(newLintCommand), rootCommand(newCompatCommand),
		},
	},
}

// registerCommands attaches the grouped subcommands to root, giving each one
// the workflow flag group and, for the watch-capable commands, the watch
// group. version and completion are added without either group: they act on
// no analysis option, so inheriting those flags would only let a typo pass
// silently.
func registerCommands(root *cobra.Command, opts *options, workflowFlags, watchFlags *pflag.FlagSet) {
	for _, cg := range commandGroups {
		root.AddGroup(&cg.group)
		for _, spec := range cg.commands {
			sub := spec.build(opts)
			sub.GroupID = cg.group.ID
			capability := spec.capability
			if capability.workflowFlags {
				addSharedFlags(sub, workflowFlags)
			}
			if capability.watchFlags {
				addSharedFlags(sub, watchFlags)
			}
			root.AddCommand(sub)
		}
	}
	root.AddCommand(newVersionCommand())
	root.AddCommand(newCompletionCommand(root))
}

// workflowFlagCommands declares the commands that actually execute the shared
// apply/export/trend/enrichment/report workflow. Other commands reject these
// flags instead of accepting values they never inspect.
type commandCapability struct {
	workflowFlags bool
	watchFlags    bool
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
