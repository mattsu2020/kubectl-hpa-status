// Package cmd implements the CLI commands for kubectl-hpa-status.
package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// commonOptions holds CLI flags shared across all commands: Kubernetes
// connection, output formatting, language, and debug settings.
type commonOptions struct {
	namespace       string
	allNamespaces   bool
	contextName     string
	kubeconfig      string
	cluster         string
	output          string
	template        string
	wide            bool
	selector        string
	color           string
	lang            string
	debug           bool
	config          string
	chunkSize       int64
	qps             float32
	burst           int
	outputTemplates map[string]outputTemplateConfig
	clientOverride  kubernetes.Interface
	in              io.Reader
}

// statusOptions holds flags for the status / analyze command: interpretation,
// suggestions, apply workflow, enrichment, diagnostics, and simulation.
type statusOptions struct {
	interpret             bool
	noInterpret           bool
	explain               bool
	suggest               bool
	fix                   bool
	apply                 bool
	diff                  bool
	dryRun                bool
	yes                   bool
	keda                  bool
	vpa                   bool
	healthWeightOverrides []string
	healthWeights         hpaanalysis.HealthWeights
	diagnoseMetrics       bool
	metricsFreshness      bool
	checkResources        bool
	explainPods           bool
	simulate              []string
	simulateMetric        []string
	simulateDuration      int32
	capacityContext       bool
	capacityDeep          bool
	capacityPlan          bool
	targetMax             int32
	scalePath             bool
	events                eventOption
	recommend             bool
	report                string
	gitopsCheck           bool
	manifestPath          string
	metricContract        bool
	churnDetect           bool
	metricHints           bool
	containerAdvisor      bool
	behaviorAdvisor       bool
	trend                 bool
	trendSince            time.Duration
	trendRetain           time.Duration
}

// listOptions holds flags specific to the list / scan commands.
type listOptions struct {
	sortBy         string
	filter         string
	healthScoreMin int
	healthScoreMax int
	problem        bool
}

// watchOptions holds flags specific to the watch / TUI commands.
type watchOptions struct {
	watch          bool
	watchInterval  time.Duration
	watchTimeout   time.Duration
	untilCondition string
	dashboard      bool
}

// options composes all option groups. Fields are accessed through the
// embedded structs (e.g., opts.namespace, opts.apply) to preserve
// backward compatibility with existing code while organizing flags
// by command scope.
type options struct {
	commonOptions
	statusOptions
	listOptions
	watchOptions
}

// Normalize resolves implied flag settings for the status command.
// Instead of scattering this logic in PersistentPreRun, this method
// centralizes the flag normalization rules:
//   - --recommend implies --suggest
//   - --fix or --apply implies --suggest + --explain
//   - --diff implies --suggest
//   - --no-interpret clears --interpret and --suggest
func (o *statusOptions) Normalize() {
	if o.recommend {
		o.suggest = true
	}
	if o.fix || o.apply {
		o.suggest = true
		o.explain = true
	}
	if o.diff {
		o.suggest = true
	}
	if o.noInterpret {
		o.interpret = false
		o.suggest = false
	}
}

func (o *options) newClient() (*kube.Client, error) {
	kopts := kube.Options{
		Namespace:  o.namespace,
		Context:    o.contextName,
		Kubeconfig: o.kubeconfig,
		Cluster:    o.cluster,
		QPS:        o.qps,
		Burst:      o.burst,
	}
	if o.clientOverride != nil {
		return kube.NewClient(kopts, kube.WithInterface(o.clientOverride))
	}
	return kube.NewClient(kopts)
}

// NewRootCommand creates and returns the root cobra command for kubectl-hpa-status.
func NewRootCommand() *cobra.Command {
	opts := &options{
		commonOptions: commonOptions{
			color:     "auto",
			chunkSize: 500,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: true, limit: 5},
			dryRun: true,
		},
		listOptions: listOptions{
			healthScoreMax: -1,
		},
		watchOptions: watchOptions{
			watchInterval: 5 * time.Second,
		},
	}

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
			opts.in = cmd.InOrStdin()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			includeInterpretation := (opts.interpret || opts.explain || opts.suggest) && !opts.noInterpret
			if opts.watch {
				if len(args) != 1 {
					return fmt.Errorf("--watch supports exactly one HPA name")
				}
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], includeInterpretation)
			}
			return runStatusMany(cmd.Context(), cmd.OutOrStdout(), opts, args, includeInterpretation)
		},
	}

	registerCommonFlags(root, opts)
	registerStatusFlags(root, opts)
	registerWatchFlags(root, opts)
	root.PersistentFlags().Lookup("events").NoOptDefVal = "true"

	root.AddCommand(newStatusCommand(opts))
	root.AddCommand(newDoctorCommand(opts))
	root.AddCommand(newAnalyzeCommand(opts))
	root.AddCommand(newAssumptionsCommand(opts))
	root.AddCommand(newListCommand(opts))
	root.AddCommand(newScanCommand(opts))
	root.AddCommand(newWatchCommand(opts))
	root.AddCommand(newTUICommand(opts))
	root.AddCommand(newTimelineCommand(opts))
	root.AddCommand(newPathCommand(opts))
	root.AddCommand(newBlockersCommand(opts))
	root.AddCommand(newCapacityPlanCommand(opts))
	root.AddCommand(newRecordCommand(opts))
	root.AddCommand(newReplayCommand(opts))
	root.AddCommand(newRecommendCommand(opts))
	root.AddCommand(newPolicyCommand(opts))
	root.AddCommand(newSnapshotCommand(opts))
	root.AddCommand(newBundleCommand(opts))
	root.AddCommand(newLintCommand(opts))
	root.AddCommand(newVersionCommand())
	root.AddCommand(newCompletionCommand(root))

	_ = root.MarkPersistentFlagFilename("kubeconfig")
	_ = root.MarkPersistentFlagFilename("config", "yaml", "yml", "json")

	registerFlagCompletions(root, opts)

	return root
}

func buildVersion() string {
	return fmt.Sprintf("%s (commit=%s, date=%s)", version, commit, date)
}

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
