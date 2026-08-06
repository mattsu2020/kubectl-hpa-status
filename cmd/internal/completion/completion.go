// Package completion provides shell-completion logic for the kubectl-hpa-status
// CLI. It is decoupled from cmd's mutable *options struct: the dynamic
// completers take a narrow Deps value describing how to reach the cluster and
// kubeconfig, so this package can be exercised and reused without wiring up a
// full command tree.
//
// The static completers (output, color, lang, ...) are pure value lists with
// cobra-compatible wrapper functions whose *cobra.Command receiver is unused.
package completion

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// timeout bounds API calls made while generating shell completions so that a
// slow or unreachable cluster cannot hang the user's shell.
const timeout = 3 * time.Second

// ClientFactory builds a Kubernetes client for completion-time API reads.
type ClientFactory func() (*kube.Client, error)

// Deps carries the option-derived values the dynamic completers need to reach
// the cluster and kubeconfig. cmd supplies it from *options without exposing
// the mutable options struct to this package.
type Deps struct {
	// NewClient creates a client bound to the CLI's namespace/context.
	NewClient ClientFactory
	// AllNamespaces lists HPAs/clusters across every namespace.
	AllNamespaces bool
	// Kubeconfig is an explicit kubeconfig path, or empty for the default.
	Kubeconfig string
}

// ShellCommand returns the `completion [bash|zsh|fish|powershell]` subcommand
// that generates completion scripts from the root command.
func ShellCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
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
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
}

// HpaName completes HPA names (optionally "namespace/name") for a ValidArgs
// function. A client-creation failure stays silent so the shell only sees the
// NoFileComp directive.
func HpaName(deps Deps) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		client, err := deps.NewClient()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		namespace := client.Namespace
		if deps.AllNamespaces {
			namespace = metav1.NamespaceAll
		}
		// Bound the API read so a slow server cannot hang tab completion.
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		hpas, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := make([]string, 0, len(hpas.Items))
		for _, hpa := range hpas.Items {
			if deps.AllNamespaces {
				names = append(names, fmt.Sprintf("%s/%s\t%s", hpa.Namespace, hpa.Name, hpa.Spec.ScaleTargetRef.Name))
				continue
			}
			names = append(names, fmt.Sprintf("%s\t%s", hpa.Name, hpa.Spec.ScaleTargetRef.Name))
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// Namespace completes cluster namespaces.
func Namespace(deps Deps) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		client, err := deps.NewClient()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		namespaces, err := client.Interface.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := make([]string, 0, len(namespaces.Items))
		for _, ns := range namespaces.Items {
			names = append(names, ns.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// Context completes kubeconfig context names.
func Context(deps Deps) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		if deps.Kubeconfig != "" {
			loadingRules.ExplicitPath = deps.Kubeconfig
		}
		config, err := loadingRules.Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return contextNames(config), cobra.ShellCompDirectiveNoFileComp
	}
}

func contextNames(config *api.Config) []string {
	names := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		names = append(names, name)
	}
	return names
}

// Static value lists for flag-value completion. Kept as package data so the
// completion UI and any validation vocabulary stay in one place.
var (
	OutputValues = []completionValue{
		{"table", "Default table output"},
		{"wide", "Extended table columns"},
		{"json", "JSON format"},
		{"yaml", "YAML format"},
		{"jsonpath=", "JSONPath expression"},
		{"template=", "Go template"},
		{"prometheus", "Prometheus exposition format"},
		{"junit", "JUnit XML for CI reports"},
		{"sarif", "SARIF for code scanning"},
	}
	FilterValues = []completionValue{
		{"all", "Show all HPAs"},
		{"ok", "Show healthy HPAs"},
		{"error", "Show HPAs with errors"},
		{"limited", "Show scaling-limited HPAs"},
		{"issue", "Show HPAs with any issue"},
	}
	SortByValues = []completionValue{
		{"name", "Sort by HPA name"},
		{"namespace", "Sort by namespace"},
		{"health", "Sort by health state"},
		{"healthscore", "Sort by health score"},
		{"current", "Sort by current replicas"},
		{"desired", "Sort by desired replicas"},
		{"diff", "Sort by replica difference"},
		{"age", "Sort by creation time"},
		{"issue", "Sort by issue description"},
		{"min", "Sort by minReplicas"},
		{"max", "Sort by maxReplicas"},
		{"target", "Sort by target utilization"},
	}
	ColorValues = []completionValue{
		{"auto", "Colorize when stdout is a terminal"},
		{"always", "Always colorize output"},
		{"never", "Never colorize output"},
	}
	LangValues = []completionValue{
		{"en", "English output"},
		{"ja", "Japanese output"},
	}
	EventsValues = []completionValue{
		{"true", "Show recent HPA events"},
		{"false", "Hide events"},
	}
	UntilConditionValues = []completionValue{
		{"ok", "HPA is healthy"},
		{"healthy", "HPA is healthy"},
		{"stable", "HPA is stable (not scaling)"},
		{"scaling-limited", "HPA hit min or max replicas"},
		{"error", "HPA has an error condition"},
	}
)

type completionValue struct {
	value, desc string
}

func (v completionValue) text() string {
	return v.value + "\t" + v.desc
}

func staticCompletions(values []completionValue) ([]string, cobra.ShellCompDirective) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, v.text())
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// Static completers adapt the value lists to cobra's completion signature.
func Output(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return staticCompletions(OutputValues)
}
func Filter(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return staticCompletions(FilterValues)
}
func SortBy(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return staticCompletions(SortByValues)
}
func Color(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return staticCompletions(ColorValues)
}
func Lang(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return staticCompletions(LangValues)
}
func Events(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return staticCompletions(EventsValues)
}
func UntilCondition(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return staticCompletions(UntilConditionValues)
}

// RegisterFlagCompletions wires value completions onto the root command's
// known flags. analysis-profile is registered by cmd because its vocabulary
// lives in internal/cmdoptions.
func RegisterFlagCompletions(root *cobra.Command, deps Deps) {
	_ = root.RegisterFlagCompletionFunc("output", Output)
	_ = root.RegisterFlagCompletionFunc("filter", Filter)
	_ = root.RegisterFlagCompletionFunc("sort-by", SortBy)
	_ = root.RegisterFlagCompletionFunc("color", Color)
	_ = root.RegisterFlagCompletionFunc("lang", Lang)
	_ = root.RegisterFlagCompletionFunc("events", Events)
	_ = root.RegisterFlagCompletionFunc("until-condition", UntilCondition)
	_ = root.RegisterFlagCompletionFunc("namespace", Namespace(deps))
	_ = root.RegisterFlagCompletionFunc("context", Context(deps))
}
