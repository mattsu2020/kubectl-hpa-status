// Package cmd implements the CLI commands for kubectl-hpa-status.
package cmd

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// NewRootCommand creates and returns the root cobra command for kubectl-hpa-status.
func NewRootCommand() *cobra.Command {
	opts := &options{}
	*opts = cmdoptions.DefaultRoot()

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

	root.AddCommand(newStatusCommand(opts))
	root.AddCommand(newExplainCommand(opts))
	root.AddCommand(newDoctorCommand(opts))
	root.AddCommand(newReadinessDoctorCommand(opts))
	root.AddCommand(newWhyNotScaleCommand(opts))
	root.AddCommand(newReadinessCommand(opts))
	root.AddCommand(newAnalyzeCommand(opts))
	root.AddCommand(newAssumptionsCommand(opts))
	root.AddCommand(newListCommand(opts))
	root.AddCommand(newScanCommand(opts))
	root.AddCommand(newFleetCommand(opts))
	root.AddCommand(newWatchCommand(opts))
	root.AddCommand(newTUICommand(opts))
	root.AddCommand(newTimelineCommand(opts))
	root.AddCommand(newTraceCommand(opts))
	root.AddCommand(newCompareCommand(opts))
	root.AddCommand(newProfileCommand(opts))
	root.AddCommand(newPathCommand(opts))
	root.AddCommand(newBlockersCommand(opts))
	root.AddCommand(newAdvisorCommand(opts))
	root.AddCommand(newContainerAdvisorCommand(opts))
	root.AddCommand(newNodeContextCommand(opts))
	root.AddCommand(newRolloutCommand(opts))
	root.AddCommand(newRolloutContextCommand(opts))
	root.AddCommand(newCapacityGapCommand(opts))
	root.AddCommand(newCapacityPlanCommand(opts))
	root.AddCommand(newPreflightCommand(opts))
	root.AddCommand(newRecordCommand(opts))
	root.AddCommand(newReplayCommand(opts))
	root.AddCommand(newMetricsCommand(opts))
	root.AddCommand(newHistoryCommand(opts))
	root.AddCommand(newBehaviorCommand(opts))
	root.AddCommand(newTuneCommand(opts))
	root.AddCommand(newEstimateCommand(opts))
	root.AddCommand(newSLOCommand(opts))
	root.AddCommand(newExportCommand(opts))
	root.AddCommand(newRecommendCommand(opts))
	root.AddCommand(newPolicyCommand(opts))
	root.AddCommand(newSnapshotCommand(opts))
	root.AddCommand(newBundleCommand(opts))
	root.AddCommand(newIncidentBundleCommand(opts))
	root.AddCommand(newSupportBundleCommand(opts))
	root.AddCommand(newAutoscalerMapCommand(opts))
	root.AddCommand(newLintCommand(opts))
	root.AddCommand(newGitOpsCommand(opts))
	root.AddCommand(newOwnershipCommand(opts))
	root.AddCommand(newAlertsCommand())
	root.AddCommand(newFlapCommand(opts))
	root.AddCommand(newSimulateCommand(opts))
	root.AddCommand(newAnalyzeRecordCommand(opts))
	root.AddCommand(newCompatCommand(opts))
	root.AddCommand(newVersionCommand())
	root.AddCommand(newCompletionCommand(root))

	_ = root.MarkPersistentFlagFilename("kubeconfig")
	_ = root.MarkPersistentFlagFilename("config")

	return root
}

func buildVersion() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)
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
