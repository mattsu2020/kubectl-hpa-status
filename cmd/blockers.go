package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsui2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/mattsui2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsui2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsui2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type blockerOutput struct {
	Namespace string                     `json:"namespace" yaml:"namespace"`
	Name      string                     `json:"name" yaml:"name"`
	Target    string                     `json:"target" yaml:"target"`
	Report    *hpaanalysis.BlockerReport `json:"blockerReport" yaml:"blockerReport"`
}

func newBlockersCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "blockers NAME [NAME...]",
		Short:             "Diagnose why HPA scale-out is not producing ready pods",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBlockers(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runBlockers(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := applyCommandPreset(opts, cmdoptions.PresetBlockers)

	outputs := make([]blockerOutput, 0, len(names))
	for _, name := range names {
		report, err := buildStatusReportWithClient(ctx, &local, name, false, nil)
		if err != nil {
			if local.Output == "json" || local.Output == "yaml" {
				writeError(out, local.Output, err)
			}
			return err
		}

		blockerReport := buildBlockerReport(ctx, &local, report.Analysis, report.Analysis.Namespace, name)
		report.Analysis.BlockerReport = blockerReport

		outputs = append(outputs, blockerOutput{
			Namespace: report.Analysis.Namespace,
			Name:      report.Analysis.Name,
			Target:    report.Analysis.Target,
			Report:    blockerReport,
		})
	}

	value := any(outputs)
	if len(outputs) == 1 {
		value = outputs[0]
	}

	format, templateStr := outputSelection(outputConfig{
		output: local.Output, template: local.Template, outputTemplates: local.OutputTemplates,
	})

	return writeOutput(out, format, templateStr, value, func() error {
		theme := style.NewTheme(shouldColorize(local.Color, out))
		for i, o := range outputs {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if err := hpaanalysis.WriteBlockerText(out, o.Report, theme); err != nil {
				return err
			}
		}
		return nil
	})
}

// buildBlockerReport assembles BlockerInput from various fetchers and runs
// the blocker analysis engine.
func buildBlockerReport(ctx context.Context, opts *options, analysis hpaanalysis.Analysis, namespace, name string) *hpaanalysis.BlockerReport {
	client, err := opts.newClient()
	if err != nil {
		return &hpaanalysis.BlockerReport{
			Warnings: []string{fmt.Sprintf("client error: %v", err)},
		}
	}

	hpa, err := client.Interface.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return &hpaanalysis.BlockerReport{
			Warnings: []string{fmt.Sprintf("failed to get HPA: %v", err)},
		}
	}

	input := hpaanalysis.BlockerInput{
		Analysis: analysis,
	}
	if analysis.ScalePath != nil {
		input.ScalePath = analysis.ScalePath
	}
	if analysis.TargetReplicas != nil {
		input.TargetReplicas = analysis.TargetReplicas
	}
	if analysis.CapacityContext != nil {
		input.CapacityContext = analysis.CapacityContext
	}
	if analysis.PodAnalysis != nil {
		input.PodAnalysis = analysis.PodAnalysis
	}

	_ = hpa
	return hpaanalysis.AnalyzeBlockers(input)
}