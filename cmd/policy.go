package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type policyCommandOptions struct {
	file string
}

type policyListReport struct {
	Items []hpaanalysis.PolicyReport `json:"items" yaml:"items"`
}

func newPolicyCommand(opts *options) *cobra.Command {
	policyOpts := &policyCommandOptions{}
	cmd := &cobra.Command{
		Use:               "policy [NAME]",
		Short:             "Evaluate HPA Policy as Code rules",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runPolicy(cmd.Context(), cmd.OutOrStdout(), opts, policyOpts, name)
		},
	}
	cmd.Flags().StringVarP(&policyOpts.file, "file", "f", "", "policy YAML file (defaults to ~/.kube/hpa-policies.yaml)")
	return cmd
}

func runPolicy(ctx context.Context, out io.Writer, opts *options, policyOpts *policyCommandOptions, name string) error {
	path := policyOpts.file
	if path == "" {
		path = "~/.kube/hpa-policies.yaml"
	}
	path = expandHomePath(path)
	policyFile, err := hpaanalysis.LoadPolicyFile(path)
	if err != nil {
		return err
	}

	client, err := opts.newClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
	}

	var reports []hpaanalysis.PolicyReport
	if name != "" {
		hpa, err := client.Interface.AutoscalingV2().HorizontalPodAutoscalers(client.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get HPA %s/%s: %w", client.Namespace, name, err)
		}
		reports = append(reports, *hpaanalysis.EvaluatePolicies(hpa, policyFile))
	} else {
		namespace := client.Namespace
		if opts.allNamespaces {
			namespace = metav1.NamespaceAll
		}
		hpas, err := client.ListHPAs(ctx, namespace, metav1.ListOptions{LabelSelector: opts.selector}, opts.chunkSize)
		if err != nil {
			return fmt.Errorf("failed to list HPAs: %w", err)
		}
		for i := range hpas.Items {
			reports = append(reports, *hpaanalysis.EvaluatePolicies(&hpas.Items[i], policyFile))
		}
	}

	report := policyListReport{Items: reports}
	format, templateStr := outputSelection(outputConfig{
		report: opts.report, output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates,
	})
	if err := writeOutput(out, format, templateStr, report, func() error {
		return writePolicyText(out, report, style.NewTheme(shouldColorize(opts.color, out)))
	}); err != nil {
		return err
	}

	for _, item := range reports {
		if len(item.Violations) > 0 {
			return &ExitCodeError{Code: ExitWarning, Err: fmt.Errorf("policy violations found")}
		}
	}
	return nil
}

func writePolicyText(out io.Writer, report policyListReport, theme style.Theme) error {
	var b strings.Builder
	b.WriteString("HPA Policy Report\n\n")
	b.WriteString(fmt.Sprintf("%-32s %-8s %s\n", "NAMESPACE/NAME", "SCORE", "SUMMARY"))
	b.WriteString(strings.Repeat("-", 80) + "\n")
	for _, item := range report.Items {
		score := fmt.Sprintf("%d/100", item.Score)
		b.WriteString(fmt.Sprintf("%-32s %-8s %s\n", item.Namespace+"/"+item.Name, score, item.Summary))
		for _, violation := range item.Violations {
			severity := violation.Severity
			if severity == "critical" || severity == "warning" {
				severity = theme.Warning.Render(severity)
			}
			b.WriteString(fmt.Sprintf("  - [%s] %s: %s", severity, violation.RuleName, violation.Description))
			if violation.Required != "" {
				b.WriteString(fmt.Sprintf(" (required: %s)", violation.Required))
			}
			b.WriteString("\n")
		}
	}
	_, err := io.WriteString(out, b.String())
	return err
}

func expandHomePath(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
