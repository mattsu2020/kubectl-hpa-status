package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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

type options struct {
	namespace      string
	allNamespaces  bool
	contextName    string
	kubeconfig     string
	cluster        string
	output         string
	template       string
	wide           bool
	selector       string
	sortBy         string
	filter         string
	healthScoreMin int
	healthScoreMax int
	color          string
	events         eventOption
	interpret      bool
	noInterpret    bool
	explain        bool
	suggest        bool
	fix            bool
	apply          bool
	dryRun         bool
	yes            bool
	lang           string
	debug          bool
	dashboard      bool
	keda           bool
	vpa            bool
	config         string
	healthWeights  hpaanalysis.HealthWeights
	problem        bool
	recommend      bool
	watch          bool
	watchInterval  time.Duration
	watchTimeout   time.Duration
	untilCondition string
	clientOverride kubernetes.Interface
	in             io.Reader
}

func (o *options) newClient() (*kube.Client, error) {
	kopts := kube.Options{
		Namespace:  o.namespace,
		Context:    o.contextName,
		Kubeconfig: o.kubeconfig,
		Cluster:    o.cluster,
	}
	if o.clientOverride != nil {
		return kube.NewClient(kopts, kube.WithInterface(o.clientOverride))
	}
	return kube.NewClient(kopts)
}

func NewRootCommand() *cobra.Command {
	opts := &options{
		events:         eventOption{enabled: true, limit: 5},
		color:          "auto",
		dryRun:         true,
		healthScoreMax: -1,
		watchInterval:  5 * time.Second,
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
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if err := applyConfigDefaults(cmd, opts); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
			}
			if opts.recommend {
				opts.suggest = true
			}
			if opts.fix || opts.apply {
				opts.suggest = true
				opts.explain = true
			}
			if opts.noInterpret {
				opts.interpret = false
				opts.suggest = false
			}
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
	root.PersistentFlags().BoolVar(&opts.apply, "apply", false, "run suggested HPA spec patch workflow")
	root.PersistentFlags().BoolVar(&opts.dryRun, "dry-run", opts.dryRun, "use server-side dry-run for --apply; set --dry-run=false to persist changes")
	root.PersistentFlags().BoolVarP(&opts.yes, "yes", "y", false, "skip confirmation when used with --apply")
	root.PersistentFlags().StringVar(&opts.lang, "lang", "", "text output language: en or ja")
	root.PersistentFlags().BoolVarP(&opts.debug, "debug", "v", false, "include internal analysis details such as ratios and health scoring inputs")
	root.PersistentFlags().StringVar(&opts.config, "config", "", "optional config file for analysis settings such as health score weights")
	root.PersistentFlags().BoolVar(&opts.recommend, "recommend", false, "alias for --suggest")
	root.PersistentFlags().BoolVar(&opts.noInterpret, "no-interpret", false, "omit interpretation and show raw status-derived data")
	root.PersistentFlags().Var(&opts.events, "events", "show recent HPA events: true, false, or a number")
	root.PersistentFlags().BoolVarP(&opts.watch, "watch", "w", false, "watch HPA status periodically")
	root.PersistentFlags().BoolVar(&opts.dashboard, "dashboard", false, "render watch output as a compact terminal dashboard")
	root.PersistentFlags().BoolVar(&opts.keda, "keda", false, "enable KEDA ScaledObject integration and cross-reference diagnostics")
	root.PersistentFlags().BoolVar(&opts.vpa, "vpa", false, "detect VerticalPodAutoscaler conflicts with HPA target")
	root.PersistentFlags().DurationVar(&opts.watchInterval, "interval", opts.watchInterval, "watch refresh interval")
	root.PersistentFlags().DurationVar(&opts.watchTimeout, "timeout", 0, "stop watching after this duration")
	root.PersistentFlags().StringVar(&opts.untilCondition, "until-condition", "", "stop watching once an HPA condition type is present, for example scaling-limited")
	root.PersistentFlags().Lookup("events").NoOptDefVal = "true"

	root.AddCommand(newStatusCommand(opts))
	root.AddCommand(newAnalyzeCommand(opts))
	root.AddCommand(newListCommand(opts))
	root.AddCommand(newScanCommand(opts))
	root.AddCommand(newWatchCommand(opts))
	root.AddCommand(newTUICommand(opts))
	root.AddCommand(newVersionCommand())
	root.AddCommand(newCompletionCommand(root))

	_ = root.MarkPersistentFlagFilename("kubeconfig")
	_ = root.MarkPersistentFlagFilename("config", "yaml", "yml", "json")

	return root
}

func buildVersion() string {
	return fmt.Sprintf("%s (commit=%s, date=%s)", version, commit, date)
}

type configFile struct {
	Namespace     string                    `json:"namespace" yaml:"namespace"`
	AllNamespaces *bool                     `json:"allNamespaces" yaml:"allNamespaces"`
	Output        string                    `json:"output" yaml:"output"`
	Wide          *bool                     `json:"wide" yaml:"wide"`
	Selector      string                    `json:"selector" yaml:"selector"`
	SortBy        string                    `json:"sortBy" yaml:"sortBy"`
	Filter        string                    `json:"filter" yaml:"filter"`
	MinScore      *int                      `json:"minScore" yaml:"minScore"`
	MaxScore      *int                      `json:"maxScore" yaml:"maxScore"`
	HealthScore   *int                      `json:"healthScore" yaml:"healthScore"`
	Color         string                    `json:"color" yaml:"color"`
	Events        *int                      `json:"events" yaml:"events"`
	EventsEnabled *bool                     `json:"eventsEnabled" yaml:"eventsEnabled"`
	Lang          string                    `json:"lang" yaml:"lang"`
	Debug         *bool                     `json:"debug" yaml:"debug"`
	Dashboard     *bool                     `json:"dashboard" yaml:"dashboard"`
	HealthWeights hpaanalysis.HealthWeights `json:"healthWeights" yaml:"healthWeights"`
}

func applyConfigDefaults(cmd *cobra.Command, opts *options) error {
	path, explicit := opts.config, opts.config != ""
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(home, ".kube", "hpa-status.yaml")
	}

	cfg, err := loadConfigFile(path)
	if err != nil {
		if !explicit && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to load config %s: %w", path, err)
	}
	applyConfig(cmd, opts, cfg)
	return nil
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and build metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "kubectl-hpa-status version %s\n", buildVersion())
			return err
		},
	}
}
