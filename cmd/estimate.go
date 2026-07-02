package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/spf13/cobra"
)

type estimateOutput struct {
	Namespace               string  `json:"namespace" yaml:"namespace"`
	Name                    string  `json:"name" yaml:"name"`
	CurrentMaxReplicas      int32   `json:"currentMaxReplicas" yaml:"currentMaxReplicas"`
	ProposedMaxReplicas     int32   `json:"proposedMaxReplicas" yaml:"proposedMaxReplicas"`
	AdditionalWorstCasePods int32   `json:"additionalWorstCasePods" yaml:"additionalWorstCasePods"`
	PodCostPerHour          float64 `json:"podCostPerHour,omitempty" yaml:"podCostPerHour,omitempty"`
	AdditionalCostPerHour   float64 `json:"additionalCostPerHour,omitempty" yaml:"additionalCostPerHour,omitempty"`
	CarbonKgPerPodHour      float64 `json:"carbonKgPerPodHour,omitempty" yaml:"carbonKgPerPodHour,omitempty"`
	AdditionalCarbonKgHour  float64 `json:"additionalCarbonKgPerHour,omitempty" yaml:"additionalCarbonKgPerHour,omitempty"`
	AvailabilityNote        string  `json:"availabilityNote" yaml:"availabilityNote"`
}

func newEstimateCommand(opts *options) *cobra.Command {
	var maxReplicas int32
	var podCost float64
	var carbonKg float64
	cmd := &cobra.Command{
		Use:               "estimate NAME",
		Short:             "Estimate cost and availability impact of HPA maxReplicas changes",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEstimate(cmd.Context(), cmd.OutOrStdout(), opts, args[0], maxReplicas, podCost, carbonKg)
		},
	}
	cmd.Flags().Int32Var(&maxReplicas, "max-replicas", 0, "proposed maxReplicas value")
	cmd.Flags().Float64Var(&podCost, "pod-cost", 0, "estimated cost per pod per hour")
	cmd.Flags().Float64Var(&carbonKg, "carbon-kg-per-pod-hour", 0, "estimated kgCO2e per additional pod per hour")
	_ = cmd.MarkFlagRequired("max-replicas")
	return cmd
}

func runEstimate(ctx context.Context, out io.Writer, opts *options, name string, proposedMax int32, podCost float64, carbonKg float64) error {
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}
	hpa, err := kube.GetHPAFromClient(ctx, client, name)
	if err != nil {
		return wrapHPALookupError(client.Namespace, name, err)
	}
	if proposedMax <= 0 {
		return fmt.Errorf("--max-replicas must be greater than zero")
	}
	additional := proposedMax - hpa.Spec.MaxReplicas
	if additional < 0 {
		additional = 0
	}
	result := estimateOutput{
		Namespace:               hpa.Namespace,
		Name:                    hpa.Name,
		CurrentMaxReplicas:      hpa.Spec.MaxReplicas,
		ProposedMaxReplicas:     proposedMax,
		AdditionalWorstCasePods: additional,
		PodCostPerHour:          podCost,
		AdditionalCostPerHour:   float64(additional) * podCost,
		CarbonKgPerPodHour:      carbonKg,
		AdditionalCarbonKgHour:  float64(additional) * carbonKg,
		AvailabilityNote:        "Higher maxReplicas can reduce capacity risk only if quota, node capacity, and metric availability are healthy; run preflight before applying.",
	}
	format, templateStr := selectOutputFromOptions(opts)
	return writeOutput(out, format, templateStr, result, func() error {
		_, _ = fmt.Fprintln(out, "Estimate:")
		_, _ = fmt.Fprintf(out, "- Current maxReplicas: %d\n", result.CurrentMaxReplicas)
		_, _ = fmt.Fprintf(out, "- Proposed maxReplicas: %d\n", result.ProposedMaxReplicas)
		_, _ = fmt.Fprintf(out, "- Additional worst-case pods: %d\n", result.AdditionalWorstCasePods)
		if podCost > 0 {
			_, _ = fmt.Fprintf(out, "- Approx additional cost: $%.2f/hour\n", result.AdditionalCostPerHour)
		}
		if carbonKg > 0 {
			_, _ = fmt.Fprintf(out, "- Approx additional carbon: %.4f kgCO2e/hour\n", result.AdditionalCarbonKgHour)
		}
		_, _ = fmt.Fprintf(out, "\n%s\n", result.AvailabilityNote)
		return nil
	})
}
