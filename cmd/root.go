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
// connection, output formatting, language, and debug settings. It also hosts
// cross-command flags that originate on the status workflow (apply/diff/export,
// health-score tuning, and trend history) because list, watch, and other
// subcommands consume them too.
type commonOptions struct {
	namespace             string
	allNamespaces         bool
	contextName           string
	kubeconfig            string
	cluster               string
	output                string
	template              string
	wide                  bool
	selector              string
	color                 string
	lang                  string
	debug                 bool
	config                string
	chunkSize             int64
	concurrency           int
	qps                   float32
	burst                 int
	outputTemplates       map[string]outputTemplateConfig
	clientOverride        kubernetes.Interface
	in                    io.Reader
	apply                 bool
	diff                  bool
	dryRun                bool
	yes                   bool
	allowPartial          bool
	export                string
	exportPatch           string
	trend                 bool
	trendSince            time.Duration
	trendRetain           time.Duration
	healthWeights         hpaanalysis.HealthWeights
	healthWeightOverrides []string
}

// statusOptions holds flags specific to the status / analyze command:
// interpretation, suggestions, enrichment, diagnostics, and simulation.
// featureFlags groups the 34 enrichment/analysis boolean toggles that were
// previously flat fields on statusOptions. Grouping them makes the feature
// surface explicit and is the first structural step toward splitting options.
// All fields are plain value-typed bool, so a shallow copy (copyOptions)
// produces an independent set — callers can mutate the copy without leaking
// flags into the shared process-wide singleton.
type featureFlags struct {
	// Status presentation
	interpret     bool
	noInterpret   bool
	explain       bool
	suggest       bool
	fix           bool
	recommend     bool
	hiddenFactors bool
	contextForAI  bool
	// Metrics diagnostics
	diagnoseMetrics    bool
	metricsFreshness   bool
	metricContract     bool
	adapterDiagnostics bool
	metricHints        bool
	// Pod/resource analysis
	checkResources bool
	explainPods    bool
	// Capacity analysis
	capacityContext  bool
	capacityHeadroom bool
	capacityDeep     bool
	capacityPlan     bool
	scalePath        bool
	nodeAutoscaler   bool
	karpenter        bool
	// Rollout & blockers
	rollout          bool
	rolloutImpact    bool
	readinessImpact  bool
	scaleoutBlockers bool
	// Controller & decision
	controllerProfile bool
	decisionTrace     bool
	// KEDA/VPA/GitOps
	gitopsCheck bool
	// Churn & advisors
	churnDetect      bool
	flappingAdvisor  bool
	trendAnomaly     bool
	containerAdvisor bool
	behaviorAdvisor  bool
}

// Apply/diff/export and health-weight flags live on commonOptions because
// list, watch, and other subcommands share them.
type statusOptions struct {
	features featureFlags

	keda                  string
	vpa                   string
	simulate              []string
	simulateMetric        []string
	simulateDuration      int32
	targetMax             int32
	assumeProfile         string
	controllerProfileFile string
	format                string
	ask                   string
	events                eventOption
	report                string
	manifestPath          string
	decisionTraceFormat   string
	incidentTemplate      string
	policyGuard           string
	policyGuardMode       string
}

// listOptions holds flags specific to the list / scan commands.
type listOptions struct {
	sortBy         string
	filter         string
	healthScoreMin int
	healthScoreMax int
	problem        bool
	summary        bool
	gitopsDrift    bool
	conflicts      bool
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

// copyOptions returns a shallow copy of opts suitable for per-command
// mutation. Several commands (blockers, doctor, bundle, scan, explain, etc.)
// force-enable enrichment flags for their workflow; mutating the shared
// process-wide *options singleton would leak those flags into subsequent
// commands, so each takes a copy first.
//
// Reference-typed fields (clientOverride, outputTemplates) are shared by value
// — both the copy and the original point to the same underlying map/slice.
// Callers that need to diverge on those must deep-copy them explicitly.
func copyOptions(opts *options) options {
	return *opts
}

// Normalize resolves implied flag settings for the status command.
// Instead of scattering this logic in PersistentPreRun, this method
// centralizes the flag normalization rules:
//   - --recommend implies --suggest
//   - --fix or --apply implies --suggest + --explain
//   - --diff implies --suggest
//   - --no-interpret clears --interpret and --suggest
//
// The receiver is *options rather than *statusOptions because the implication
// chain spans both embedded option groups: --apply/--diff/--export live on
// commonOptions (shared with list/watch) while --suggest/--explain live on
// statusOptions. Embedded-field promotion keeps the body unchanged.
func (o *options) Normalize() {
	o.normalizeSuggestFlags()
	o.normalizeDecisionTraceFlags()
	o.normalizeInsightFlags()
	o.normalizeCapacityFlags()
	o.normalizeMiscFlags()
}

// normalizeSuggestFlags handles the --recommend/--fix/--apply/--diff/--export
// implication chain that enables --suggest.
func (o *options) normalizeSuggestFlags() {
	if o.features.recommend {
		o.features.suggest = true
	}
	if o.features.fix || o.apply {
		o.features.suggest = true
		o.features.explain = true
	}
	if o.diff {
		o.features.suggest = true
	}
	if o.export != "" {
		o.features.suggest = true
	}
	if o.exportPatch != "" {
		o.export = o.exportPatch
		o.features.suggest = true
	}
}

// normalizeDecisionTraceFlags enables the decision trace when an explicit
// format is given or the structured status format is selected.
func (o *options) normalizeDecisionTraceFlags() {
	if o.decisionTraceFormat != "" {
		o.features.decisionTrace = true
	}
	if o.format == "structured" {
		o.features.explain = true
		o.features.decisionTrace = true
		o.decisionTraceFormat = "json"
	}
}

// normalizeInsightFlags enables the deeper-insight flags implied by AI context,
// ask, and hiddenFactors.
func (o *options) normalizeInsightFlags() {
	if o.features.contextForAI || o.ask != "" {
		o.features.explain = true
		o.features.diagnoseMetrics = true
		o.features.metricHints = true
		o.features.hiddenFactors = true
	}
	if o.features.hiddenFactors {
		o.features.readinessImpact = true
		o.features.metricsFreshness = true
	}
}

// normalizeCapacityFlags enables capacity/scalePath when node autoscaler
// flavors are requested.
func (o *options) normalizeCapacityFlags() {
	if o.features.nodeAutoscaler || o.features.karpenter {
		o.features.capacityContext = true
		o.features.capacityDeep = true
		o.features.scalePath = true
	}
}

// normalizeMiscFlags covers the remaining standalone normalizations: trend
// anomaly escalation and the no-interpret override.
func (o *options) normalizeMiscFlags() {
	if o.trend && !o.features.trendAnomaly {
		o.features.trendAnomaly = true
	}
	if o.features.noInterpret {
		o.features.interpret = false
		o.features.suggest = false
	}
}

func (o *commonOptions) newClient() (*kube.Client, error) {
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
			dryRun:    true,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: true, limit: 5},
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
			includeInterpretation := (opts.features.interpret || opts.features.explain || opts.features.suggest) && !opts.features.noInterpret
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
