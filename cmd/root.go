package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/mattsu2020/kubehpa_cli/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubehpa_cli/pkg/hpa"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var version = "dev"

type options struct {
	namespace     string
	allNamespaces bool
	contextName   string
	kubeconfig    string
	cluster       string
	output        string
	wide          bool
	events        eventOption
	interpret     bool
	noInterpret   bool
	watch         bool
	watchInterval time.Duration
}

func NewRootCommand() *cobra.Command {
	opts := &options{
		events:        eventOption{enabled: true, limit: 5},
		watchInterval: 5 * time.Second,
	}

	root := &cobra.Command{
		Use:           "kubectl-hpa-status",
		Short:         "Inspect HorizontalPodAutoscaler status",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if opts.noInterpret {
				opts.interpret = false
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if opts.watch {
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
		},
	}

	root.PersistentFlags().StringVarP(&opts.namespace, "namespace", "n", "", "namespace")
	root.PersistentFlags().BoolVarP(&opts.allNamespaces, "all-namespaces", "A", false, "list HPAs across all namespaces")
	root.PersistentFlags().StringVar(&opts.contextName, "context", "", "kubeconfig context")
	root.PersistentFlags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to kubeconfig")
	root.PersistentFlags().StringVar(&opts.cluster, "cluster", "", "kubeconfig cluster")
	root.PersistentFlags().StringVarP(&opts.output, "output", "o", "", "output format: table, wide, json, yaml")
	root.PersistentFlags().BoolVar(&opts.interpret, "interpret", false, "include interpretation in status output")
	root.PersistentFlags().BoolVar(&opts.noInterpret, "no-interpret", false, "omit interpretation and show raw status-derived data")
	root.PersistentFlags().Var(&opts.events, "events", "show recent HPA events: true, false, or a number")
	root.PersistentFlags().BoolVar(&opts.watch, "watch", false, "watch one HPA from the main status command")
	root.PersistentFlags().DurationVar(&opts.watchInterval, "interval", opts.watchInterval, "watch refresh interval")
	root.PersistentFlags().Lookup("events").NoOptDefVal = "true"

	root.AddCommand(newStatusCommand(opts))
	root.AddCommand(newAnalyzeCommand(opts))
	root.AddCommand(newListCommand(opts))
	root.AddCommand(newWatchCommand(opts))
	root.AddCommand(newCompletionCommand(root))

	return root
}

func newStatusCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status NAME",
		Short: "Show concise status for one HPA",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			includeInterpretation := opts.interpret && !opts.noInterpret
			if opts.watch {
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], includeInterpretation)
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), opts, args[0], includeInterpretation)
		},
	}
}

func newAnalyzeCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:     "analyze NAME",
		Aliases: []string{"diagnose"},
		Short:   "Analyze one HPA using visible Kubernetes API signals",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.watch {
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
		},
	}
}

func newListCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List HPAs and highlight visible issues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
}

func newWatchCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch NAME",
		Short: "Watch one HPA status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
		},
	}
	return cmd
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
	return cmd
}

func runStatus(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	client, err := kube.NewClient(kube.Options{
		Namespace:  opts.namespace,
		Context:    opts.contextName,
		Kubeconfig: opts.kubeconfig,
		Cluster:    opts.cluster,
	})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	hpa, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(client.Namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get HPA %s/%s: %w", client.Namespace, name, err)
	}

	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analyze(hpa, includeInterpretation),
	}

	if opts.events.enabled {
		events, err := hpaanalysis.RecentEvents(ctx, client.Interface, hpa.Namespace, hpa.Name, int64(opts.events.limit))
		if err != nil {
			report.Events = []hpaanalysis.Event{{Reason: "Error", Message: fmt.Sprintf("failed to list events: %v", err)}}
		} else {
			report.Events = events
		}
	}

	return writeOutput(out, opts.output, report, func() error {
		return hpaanalysis.WriteStatusText(out, report)
	})
}

func runList(ctx context.Context, out io.Writer, opts *options) error {
	client, err := kube.NewClient(kube.Options{
		Namespace:  opts.namespace,
		Context:    opts.contextName,
		Kubeconfig: opts.kubeconfig,
		Cluster:    opts.cluster,
	})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	namespace := client.Namespace
	if opts.allNamespaces {
		namespace = metav1.NamespaceAll
	}

	hpas, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(namespace).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list HPAs: %w", err)
	}

	report := hpaanalysis.ListReport{}
	for i := range hpas.Items {
		report.Items = append(report.Items, hpaanalysis.NewListItem(hpaanalysis.Analyze(&hpas.Items[i], false)))
	}

	wide := opts.wide || opts.output == "wide"
	return writeOutput(out, opts.output, report, func() error {
		return hpaanalysis.WriteListText(out, report, wide)
	})
}

func runWatch(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	ticker := time.NewTicker(opts.watchInterval)
	defer ticker.Stop()

	for {
		if _, err := fmt.Fprintf(out, "Updated: %s\n\n", time.Now().Format(time.RFC3339)); err != nil {
			return err
		}
		if err := runStatus(ctx, out, opts, name, includeInterpretation); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
	}
}

func writeOutput(out io.Writer, format string, value any, writeText func() error) error {
	switch format {
	case "", "table", "wide":
		return writeText()
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	case "yaml":
		data, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

type eventOption struct {
	enabled bool
	limit   int
}

func (o *eventOption) Set(value string) error {
	switch value {
	case "", "true":
		o.enabled = true
		if o.limit <= 0 {
			o.limit = 5
		}
		return nil
	case "false":
		o.enabled = false
		return nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("events must be true, false, or a positive number")
	}
	if limit < 1 {
		return fmt.Errorf("events limit must be greater than zero")
	}
	o.enabled = true
	o.limit = limit
	return nil
}

func (o eventOption) String() string {
	if !o.enabled {
		return "false"
	}
	return strconv.Itoa(o.limit)
}

func (o eventOption) Type() string {
	return "boolOrInt"
}
