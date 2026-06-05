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
	checkResources        bool
	explainPods           bool
	simulate              []string
	capacityContext       bool
	events                eventOption
	recommend             bool
	report                string
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

	root.PersistentFlags().StringVarP(&opts.namespace, "namespace", "n", "", "namespace")
	root.PersistentFlags().BoolVarP(&opts.allNamespaces, "all-namespaces", "A", false, "list HPAs across all namespaces")
	root.PersistentFlags().StringVar(&opts.contextName, "context", "", "kubeconfig context")
	root.PersistentFlags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to kubeconfig")
	root.PersistentFlags().StringVar(&opts.cluster, "cluster", "", "kubeconfig cluster")
	root.PersistentFlags().StringVarP(&opts.output, "output", "o", "", "output format: table, wide, json, yaml, jsonpath=..., template=...")
	root.PersistentFlags().StringVar(&opts.template, "template", "", "template string to use when -o jsonpath or -o go-template/template is specified")
	root.PersistentFlags().BoolVar(&opts.wide, "wide", false, "show additional columns in table output")
	root.PersistentFlags().StringVarP(&opts.selector, "selector", "l", "", "label selector for list and scan, for example app=web,tier!=canary")
	root.PersistentFlags().StringVar(&opts.color, "color", opts.color, "colorize table output: auto, always, never")
	root.PersistentFlags().BoolVar(&opts.interpret, "interpret", false, "include interpretation in status output")
	root.PersistentFlags().BoolVar(&opts.explain, "explain", false, "include detailed interpretation and recommended actions")
	root.PersistentFlags().BoolVar(&opts.suggest, "suggest", false, "include concrete suggestions for configuration changes")
	root.PersistentFlags().BoolVar(&opts.fix, "fix", false, "show stronger fix plan with patch commands")
	root.PersistentFlags().BoolVar(&opts.diff, "diff", false, "show field-level diff of suggested changes")
	root.PersistentFlags().BoolVar(&opts.apply, "apply", false, "run suggested HPA spec patch workflow")
	root.PersistentFlags().BoolVar(&opts.dryRun, "dry-run", opts.dryRun, "use server-side dry-run for --apply; set --dry-run=false to persist changes")
	root.PersistentFlags().BoolVarP(&opts.yes, "yes", "y", false, "skip confirmation when used with --apply")
	root.PersistentFlags().StringVar(&opts.lang, "lang", "", "text output language: en or ja")
	root.PersistentFlags().BoolVarP(&opts.debug, "debug", "v", false, "include internal analysis details such as ratios and health scoring inputs")
	root.PersistentFlags().StringVar(&opts.config, "config", "", "optional config file for analysis settings such as health score weights")
	root.PersistentFlags().Int64Var(&opts.chunkSize, "chunk-size", opts.chunkSize, "Kubernetes list page size for list/scan/tui; set 0 to disable pagination")
	root.PersistentFlags().StringArrayVar(&opts.healthWeightOverrides, "health-weight", nil, "override a health score penalty, for example scalingInactive=50; repeatable")
	root.PersistentFlags().BoolVar(&opts.recommend, "recommend", false, "alias for --suggest")
	root.PersistentFlags().BoolVar(&opts.noInterpret, "no-interpret", false, "omit interpretation and show raw status-derived data")
	root.PersistentFlags().Var(&opts.events, "events", "show recent HPA events: true, false, or a number")
	root.PersistentFlags().BoolVarP(&opts.watch, "watch", "w", false, "watch HPA status periodically")
	root.PersistentFlags().BoolVar(&opts.dashboard, "dashboard", false, "render watch output as a compact terminal dashboard")
	root.PersistentFlags().BoolVar(&opts.keda, "keda", true, "enable KEDA ScaledObject integration (auto-detected when CRD is present; use --keda=false to disable)")
	root.PersistentFlags().BoolVar(&opts.diagnoseMetrics, "diagnose-metrics", false, "run comprehensive metrics pipeline health checks")
	root.PersistentFlags().BoolVar(&opts.vpa, "vpa", true, "detect VerticalPodAutoscaler conflicts (auto-detected when CRD is present; use --vpa=false to disable)")
	root.PersistentFlags().BoolVar(&opts.checkResources, "check-resources", false, "check HPA target utilization against pod resource requests")
	root.PersistentFlags().BoolVar(&opts.explainPods, "explain-pods", false, "analyze scale target pods for readiness, resource requests, and metric coverage")
	root.PersistentFlags().StringArrayVar(&opts.simulate, "simulate", nil, "simulate HPA spec changes (e.g. maxReplicas=20); repeatable")
	root.PersistentFlags().BoolVar(&opts.capacityContext, "capacity-context", false, "check infrastructure capacity constraints affecting HPA scaling")
	root.PersistentFlags().Float32Var(&opts.qps, "qps", 0, "client-side rate limiting queries per second (0 uses client-go default)")
	root.PersistentFlags().IntVar(&opts.burst, "burst", 0, "client-side rate limiting burst size (0 uses client-go default)")
	root.PersistentFlags().DurationVar(&opts.watchInterval, "interval", opts.watchInterval, "watch refresh interval")
	root.PersistentFlags().DurationVar(&opts.watchTimeout, "timeout", 0, "stop watching after this duration")
	root.PersistentFlags().StringVar(&opts.untilCondition, "until-condition", "", "stop watching once an HPA condition type is present, for example scaling-limited")
	root.PersistentFlags().StringVar(&opts.report, "report", "", "generate standalone report: markdown or html")
	root.PersistentFlags().Lookup("events").NoOptDefVal = "true"

	root.AddCommand(newStatusCommand(opts))
	root.AddCommand(newAnalyzeCommand(opts))
	root.AddCommand(newListCommand(opts))
	root.AddCommand(newScanCommand(opts))
	root.AddCommand(newWatchCommand(opts))
	root.AddCommand(newTUICommand(opts))
	root.AddCommand(newTimelineCommand(opts))
	root.AddCommand(newRecordCommand(opts))
	root.AddCommand(newReplayCommand(opts))
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
