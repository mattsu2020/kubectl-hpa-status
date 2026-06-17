package cmd

import (
	"context"
	"fmt"
	"io"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newRecommendCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "recommend NAME [NAME...]",
		Short:             "Audit HPA configuration against best practices",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, _ := cmd.Flags().GetString("profile")
			policyFile, _ := cmd.Flags().GetString("policy")
			if policyFile != "" {
				return runPolicy(cmd.Context(), cmd.OutOrStdout(), opts, &policyCommandOptions{file: policyFile}, args[0])
			}
			return runRecommend(cmd.Context(), cmd.OutOrStdout(), opts, args, hpaanalysis.AuditProfile(profile))
		},
	}
	cmd.Flags().String("profile", "", "workload profile for threshold adjustments: latency, cost, batch, keda, critical")
	cmd.Flags().String("policy", "", "policy YAML file for organization-specific HPA rules")
	return cmd
}

func runRecommend(ctx context.Context, out io.Writer, opts *options, args []string, profile hpaanalysis.AuditProfile) error {
	client, err := opts.NewClient()
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	for _, hpaName := range args {
		hpa, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(client.Namespace).
			Get(ctx, hpaName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting HPA %s: %w", hpaName, err)
		}

		minReplicas := hpaanalysis.DefaultMinReplicas
		if hpa.Spec.MinReplicas != nil {
			minReplicas = *hpa.Spec.MinReplicas
		}

		var report *hpaanalysis.AuditReport
		if profile != "" {
			report = hpaanalysis.AuditHPAWithProfile(hpa, minReplicas, profile)
		} else {
			report = hpaanalysis.AuditHPA(hpa, minReplicas)
		}

		format, templateStr := outputSelection(outputConfig{
			report: opts.Report, output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates,
		})
		if err := writeOutput(out, format, templateStr, report, func() error {
			return hpaanalysis.WriteAuditText(out, report, labelProviderForLang(opts.Lang, opts.Output))
		}); err != nil {
			return err
		}
	}
	return nil
}
