package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type fleetReport struct {
	Risk                    string          `json:"risk" yaml:"risk"`
	HPAs                    int             `json:"hpas" yaml:"hpas"`
	CurrentPods             int32           `json:"currentPods" yaml:"currentPods"`
	WorstCasePods           int32           `json:"worstCasePods" yaml:"worstCasePods"`
	AdditionalPods          int32           `json:"additionalPods" yaml:"additionalPods"`
	AtMaxReplicas           int             `json:"atMaxReplicas" yaml:"atMaxReplicas"`
	WithoutConfiguredMetric int             `json:"withoutConfiguredMetric" yaml:"withoutConfiguredMetric"`
	TopRisks                []fleetRiskItem `json:"topRisks,omitempty" yaml:"topRisks,omitempty"`
}

type fleetRiskItem struct {
	Namespace      string `json:"namespace" yaml:"namespace"`
	Name           string `json:"name" yaml:"name"`
	Target         string `json:"target" yaml:"target"`
	Current        int32  `json:"currentReplicas" yaml:"currentReplicas"`
	Max            int32  `json:"maxReplicas" yaml:"maxReplicas"`
	AdditionalPods int32  `json:"additionalPods" yaml:"additionalPods"`
	Risk           string `json:"risk" yaml:"risk"`
}

func newFleetCommand(opts *options) *cobra.Command {
	var risk string
	cmd := &cobra.Command{
		Use:   "fleet",
		Short: "Scan fleet-wide HPA capacity risk",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFleet(cmd.Context(), cmd.OutOrStdout(), opts, risk)
		},
	}
	cmd.Flags().StringVar(&risk, "risk", "max-surge", "fleet risk model to run: max-surge")
	return cmd
}

func runFleet(ctx context.Context, out io.Writer, opts *options, risk string) error {
	if risk == "" {
		risk = "max-surge"
	}
	if risk != "max-surge" {
		return fmt.Errorf("unsupported --risk %q (use max-surge)", risk)
	}
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}
	namespace := client.Namespace
	if opts.AllNamespaces {
		namespace = metav1.NamespaceAll
	}
	hpas, err := client.ListHPAs(ctx, namespace, metav1.ListOptions{LabelSelector: opts.Selector}, opts.ChunkSize)
	if err != nil {
		return fmt.Errorf("failed to list HPAs: %w", err)
	}
	report := buildFleetReport(hpas.Items, risk)
	return writeFleetReport(out, opts, report)
}

func buildFleetReport(hpas []autoscalingv2.HorizontalPodAutoscaler, risk string) fleetReport {
	report := fleetReport{Risk: risk, HPAs: len(hpas)}
	for i := range hpas {
		hpa := &hpas[i]
		current := hpa.Status.CurrentReplicas
		if current == 0 {
			current = hpa.Status.DesiredReplicas
		}
		additional := hpa.Spec.MaxReplicas - current
		if additional < 0 {
			additional = 0
		}
		report.CurrentPods += current
		report.WorstCasePods += current + additional
		report.AdditionalPods += additional
		if current >= hpa.Spec.MaxReplicas {
			report.AtMaxReplicas++
		}
		if len(hpa.Spec.Metrics) == 0 {
			report.WithoutConfiguredMetric++
		}
		if additional > 0 {
			report.TopRisks = append(report.TopRisks, fleetRiskItem{
				Namespace:      hpa.Namespace,
				Name:           hpa.Name,
				Target:         hpa.Spec.ScaleTargetRef.Kind + "/" + hpa.Spec.ScaleTargetRef.Name,
				Current:        current,
				Max:            hpa.Spec.MaxReplicas,
				AdditionalPods: additional,
				Risk:           fmt.Sprintf("could add +%d pod(s) if this HPA reaches maxReplicas", additional),
			})
		}
	}
	sort.Slice(report.TopRisks, func(i, j int) bool {
		if report.TopRisks[i].AdditionalPods != report.TopRisks[j].AdditionalPods {
			return report.TopRisks[i].AdditionalPods > report.TopRisks[j].AdditionalPods
		}
		if report.TopRisks[i].Namespace != report.TopRisks[j].Namespace {
			return report.TopRisks[i].Namespace < report.TopRisks[j].Namespace
		}
		return report.TopRisks[i].Name < report.TopRisks[j].Name
	})
	if len(report.TopRisks) > 10 {
		report.TopRisks = report.TopRisks[:10]
	}
	return report
}

func writeFleetReport(out io.Writer, opts *options, report fleetReport) error {
	format, _ := outputSelection(outputConfig{report: opts.Report, output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates})
	switch format {
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	case "yaml":
		data, err := yaml.Marshal(report)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	default:
		_, _ = fmt.Fprintln(out, "Fleet HPA Risk Summary")
		_, _ = fmt.Fprintf(out, "  risk model: %s\n", report.Risk)
		_, _ = fmt.Fprintf(out, "  HPAs: %d\n", report.HPAs)
		_, _ = fmt.Fprintf(out, "  current pods: %d\n", report.CurrentPods)
		_, _ = fmt.Fprintf(out, "  worst-case pods at maxReplicas: %d\n", report.WorstCasePods)
		_, _ = fmt.Fprintf(out, "  additional pods: +%d\n", report.AdditionalPods)
		_, _ = fmt.Fprintf(out, "  HPAs already at maxReplicas: %d\n", report.AtMaxReplicas)
		if report.WithoutConfiguredMetric > 0 {
			_, _ = fmt.Fprintf(out, "  HPAs without configured metrics: %d\n", report.WithoutConfiguredMetric)
		}
		if len(report.TopRisks) > 0 {
			_, _ = fmt.Fprintln(out, "\nTop risks:")
			for i, item := range report.TopRisks {
				_, _ = fmt.Fprintf(out, "  %d. %s/%s: %s\n", i+1, item.Namespace, item.Name, item.Risk)
			}
		}
		return nil
	}
}
