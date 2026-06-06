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
			return runRecommend(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
	return cmd
}

func runRecommend(ctx context.Context, out io.Writer, opts *options, args []string) error {
	client, err := opts.newClient()
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	for _, name := range args {
		hpa, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(client.Namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting HPA %s: %w", name, err)
		}

		minReplicas := hpaanalysis.DefaultMinReplicas
		if hpa.Spec.MinReplicas != nil {
			minReplicas = *hpa.Spec.MinReplicas
		}

		report := hpaanalysis.AuditHPA(hpa, minReplicas)

		format, templateStr := outputSelection(outputConfig{
			report: opts.report, output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates,
		})
		if err := writeOutput(out, format, templateStr, report, func() error {
			return hpaanalysis.WriteAuditText(out, report, labelProviderForLang(opts.lang, opts.output))
		}); err != nil {
			return err
		}
	}
	return nil
}
