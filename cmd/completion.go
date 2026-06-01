package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		client, err := opts.newClient()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		namespace := client.Namespace
		if opts.allNamespaces {
			namespace = metav1.NamespaceAll
		}
		hpas, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := make([]string, 0, len(hpas.Items))
		for _, hpa := range hpas.Items {
			if opts.allNamespaces {
				names = append(names, fmt.Sprintf("%s/%s\t%s", hpa.Namespace, hpa.Name, hpa.Spec.ScaleTargetRef.Name))
				continue
			}
			names = append(names, fmt.Sprintf("%s\t%s", hpa.Name, hpa.Spec.ScaleTargetRef.Name))
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}
