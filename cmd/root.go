// Package cmd implements the CLI commands for kubectl-hpa-status.
package cmd

import (
	"fmt"
	"io"

	"github.com/mattsui2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/mattsui2020/kubectl-hpa-status/internal/kube"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func (o *options) Normalize() {
	(*cmdoptions.Root)(o).Normalize()
}

func (o *commonOptions) newClient() (*kube.Client, error) {
	return (*cmdoptions.Common)(o).NewClient()
}

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
			if opts.Watch {
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

	root.AddCommand(newStatusCommand(opts))
	root.AddCommand(newExplainCommand(opts))
	root.AddCommand(newDoctorCommand(opts))
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

// setStdin allows tests to inject stdin without reaching into unexported fields.
func setStdin(opts *options, in io.Reader) {
	opts.In = in
}