package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// completionTimeout bounds API calls made while generating shell completions so
// that a slow or unreachable cluster cannot hang the user's shell.
const completionTimeout = 3 * time.Second

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
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
	return cmd
}

func hpaNameCompletion(opts *options) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		client, err := opts.NewClient()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		namespace := client.Namespace
		if opts.AllNamespaces {
			namespace = metav1.NamespaceAll
		}
		// Shell completion should never block the shell. Use the command's
		// context (propagated cancellation, e.g. on Ctrl-C) and bound it to a
		// short timeout so a slow API server cannot hang tab completion.
		ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
		defer cancel()
		hpas, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := make([]string, 0, len(hpas.Items))
		for _, hpa := range hpas.Items {
			if opts.AllNamespaces {
				names = append(names, fmt.Sprintf("%s/%s\t%s", hpa.Namespace, hpa.Name, hpa.Spec.ScaleTargetRef.Name))
				continue
			}
			names = append(names, fmt.Sprintf("%s\t%s", hpa.Name, hpa.Spec.ScaleTargetRef.Name))
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// Static flag completion functions provide tab-completion for flag values.

func outputCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"table\tDefault table output",
		"wide\tExtended table columns",
		"json\tJSON format",
		"yaml\tYAML format",
		"jsonpath=\tJSONPath expression",
		"template=\tGo template",
		"prometheus\tPrometheus exposition format",
		"junit\tJUnit XML for CI reports",
		"sarif\tSARIF for code scanning",
	}, cobra.ShellCompDirectiveNoFileComp
}

func filterCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"all\tShow all HPAs",
		"ok\tShow healthy HPAs",
		"error\tShow HPAs with errors",
		"limited\tShow scaling-limited HPAs",
		"issue\tShow HPAs with any issue",
	}, cobra.ShellCompDirectiveNoFileComp
}

func sortByCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"name\tSort by HPA name",
		"namespace\tSort by namespace",
		"health\tSort by health state",
		"healthscore\tSort by health score",
		"current\tSort by current replicas",
		"desired\tSort by desired replicas",
		"diff\tSort by replica difference",
		"age\tSort by creation time",
		"issue\tSort by issue description",
		"min\tSort by minReplicas",
		"max\tSort by maxReplicas",
		"target\tSort by target utilization",
	}, cobra.ShellCompDirectiveNoFileComp
}

func colorCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"auto\tColorize when stdout is a terminal",
		"always\tAlways colorize output",
		"never\tNever colorize output",
	}, cobra.ShellCompDirectiveNoFileComp
}

func langCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"en\tEnglish output",
		"ja\tJapanese output",
	}, cobra.ShellCompDirectiveNoFileComp
}

func eventsCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"true\tShow recent HPA events",
		"false\tHide events",
	}, cobra.ShellCompDirectiveNoFileComp
}

func untilConditionCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"ok\tHPA is healthy",
		"healthy\tHPA is healthy",
		"stable\tHPA is stable (not scaling)",
		"scaling-limited\tHPA hit min or max replicas",
		"error\tHPA has an error condition",
	}, cobra.ShellCompDirectiveNoFileComp
}

// Dynamic flag completion functions that query the Kubernetes API or kubeconfig.

func namespaceCompletions(opts *options) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		client, err := opts.NewClient()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
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

func contextCompletions(opts *options) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		if opts.Kubeconfig != "" {
			loadingRules.ExplicitPath = opts.Kubeconfig
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

// registerFlagCompletions registers shell completions for all flags with known values.
func registerFlagCompletions(root *cobra.Command, opts *options) {
	_ = root.RegisterFlagCompletionFunc("output", outputCompletions)
	_ = root.RegisterFlagCompletionFunc("filter", filterCompletions)
	_ = root.RegisterFlagCompletionFunc("sort-by", sortByCompletions)
	_ = root.RegisterFlagCompletionFunc("color", colorCompletions)
	_ = root.RegisterFlagCompletionFunc("lang", langCompletions)
	_ = root.RegisterFlagCompletionFunc("events", eventsCompletions)
	_ = root.RegisterFlagCompletionFunc("until-condition", untilConditionCompletions)
	_ = root.RegisterFlagCompletionFunc("namespace", namespaceCompletions(opts))
	_ = root.RegisterFlagCompletionFunc("context", contextCompletions(opts))
	_ = root.RegisterFlagCompletionFunc("analysis-profile", analysisProfileCompletions)
}
