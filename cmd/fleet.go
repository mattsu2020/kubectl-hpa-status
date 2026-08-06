package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/fleet"
)

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
	report := fleet.BuildReport(hpas.Items, risk)
	return writeFleetReport(out, opts, report)
}

func writeFleetReport(out io.Writer, opts *options, report fleet.Report) error {
	format, _ := selectOutputFromOptions(opts)
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
