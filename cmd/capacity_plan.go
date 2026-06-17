package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsui2020/kubectl-hpa-status/internal/cmdoptions"
	hpaanalysis "github.com/mattsui2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type capacityPlanOutput struct {
	Namespace string                    `json:"namespace" yaml:"namespace"`
	Name      string                    `json:"name" yaml:"name"`
	Target    string                    `json:"target" yaml:"target"`
	Plan      *hpaanalysis.CapacityPlan `json:"capacityPlan" yaml:"capacityPlan"`
}

func newCapacityPlanCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "capacity NAME [NAME...]",
		Short:             "Estimate resources needed to reach a higher maxReplicas target",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCapacityPlan(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runCapacityPlan(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := applyCommandPreset(opts, cmdoptions.PresetCapacityPlan)
	return runCapacityPlanWithOptions(ctx, out, &local, names)
}

func runCapacityPlanWithOptions(ctx context.Context, out io.Writer, opts *options, names []string) error {
	client, err := opts.newClient()
	if err != nil {
		return err
	}

	outputs := make([]capacityPlanOutput, 0, len(names))
	for _, name := range names {
		hpa, err := client.Interface.AutoscalingV2().HorizontalPodAutoscalers(client.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get HPA %s: %w", name, err)
		}
		target := fmt.Sprintf("%s/%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name)
		input := hpaanalysis.CapacityPlanInput{
			Namespace:       hpa.Namespace,
			HPAName:         hpa.Name,
			Target:          target,
			CurrentReplicas: hpa.Status.CurrentReplicas,
			MaxReplicas:     hpa.Spec.MaxReplicas,
			TargetMaxReplicas: opts.TargetMax,
		}
		outputs = append(outputs, capacityPlanOutput{
			Namespace: hpa.Namespace,
			Name:      hpa.Name,
			Target:    target,
			Plan:      hpaanalysis.AnalyzeCapacityPlan(input),
		})
	}

	value := any(outputs)
	if len(outputs) == 1 {
		value = outputs[0]
	}
	format, templateStr := outputSelection(outputConfig{
		output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates,
	})
	return writeOutput(out, format, templateStr, value, func() error {
		for i, item := range outputs {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if item.Plan == nil {
				continue
			}
			if err := hpaanalysis.WriteCapacityPlanText(out, *item.Plan); err != nil {
				return err
			}
		}
		return nil
	})
}