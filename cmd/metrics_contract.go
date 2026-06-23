package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func newMetricsContractCommand(opts *options) *cobra.Command {
	var generate string

	cmd := &cobra.Command{
		Use:               "contract NAME",
		Short:             "Validate HPA metric API contract and optionally generate test artifacts",
		Long:              "Check that each HPA metric has a reachable API service and current data. Use --generate to produce YAML, Markdown, JUnit XML, or kubectl verification commands.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMetricsContract(cmd.Context(), cmd.OutOrStdout(), opts, args[0], generate)
		},
	}

	cmd.Flags().StringVar(&generate, "generate", "",
		"generate test artifacts: yaml, markdown, junit, commands")

	return cmd
}

func runMetricsContract(ctx context.Context, out io.Writer, opts *options, name string, generate string) error {
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}

	hpa, err := kube.GetHPAFromClient(ctx, client, name)
	if err != nil {
		return wrapHPALookupError(client.Namespace, name, err)
	}

	input := buildMetricContractInput(ctx, client, hpa)
	report := hpaanalysis.AnalyzeMetricContract(input)

	switch generate {
	case "yaml":
		data, err := hpaanalysis.GenerateContractYAML(report)
		if err != nil {
			return fmt.Errorf("failed to generate YAML: %w", err)
		}
		_, err = out.Write(data)
		return err
	case "markdown":
		data, err := hpaanalysis.GenerateContractMarkdown(report)
		if err != nil {
			return fmt.Errorf("failed to generate Markdown: %w", err)
		}
		_, err = out.Write(data)
		return err
	case "junit":
		data, err := hpaanalysis.GenerateContractJUnit(report)
		if err != nil {
			return fmt.Errorf("failed to generate JUnit XML: %w", err)
		}
		_, err = out.Write(data)
		return err
	case "commands":
		commands := hpaanalysis.GenerateContractCommands(report)
		for _, cmd := range commands {
			_, _ = fmt.Fprintln(out, cmd)
		}
		return nil
	default:
		// Standard output (text, JSON, YAML via --output flag)
		format, templateStr := outputSelection(outputConfig{
			output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates,
		})

		output := metricsContractOutput{
			Namespace: report.Namespace,
			Name:      report.Name,
			Target:    report.Target,
			Contract:  report,
		}

		return writeOutput(out, format, templateStr, output, func() error {
			return hpaanalysis.WriteMetricContractText(out, report)
		})
	}
}

// metricsContractOutput wraps the contract report for structured output.
type metricsContractOutput struct {
	Namespace string                            `json:"namespace" yaml:"namespace"`
	Name      string                            `json:"name" yaml:"name"`
	Target    string                            `json:"target" yaml:"target"`
	Contract  *hpaanalysis.MetricContractReport `json:"contract" yaml:"contract"`
}

// buildMetricContractInput builds the input for metrics contract analysis.
func buildMetricContractInput(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) hpaanalysis.MetricContractInput {
	input := hpaanalysis.MetricContractInput{
		Namespace: hpa.Namespace,
		HPAName:   hpa.Name,
		Target:    fmt.Sprintf("%s/%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name),
		Metrics:   make([]hpaanalysis.MetricContractMetric, 0, len(hpa.Spec.Metrics)),
		APIServices: map[string]hpaanalysis.APIServiceStatus{
			"metrics.k8s.io/v1beta1":          checkAPIServiceAvailability(ctx, client, "metrics.k8s.io/v1beta1"),
			"custom.metrics.k8s.io/v1beta1":   checkAPIServiceAvailability(ctx, client, "custom.metrics.k8s.io/v1beta1"),
			"external.metrics.k8s.io/v1beta1": checkAPIServiceAvailability(ctx, client, "external.metrics.k8s.io/v1beta1"),
		},
	}

	currentMetricMap := buildCurrentMetricDataMap(hpa)

	for _, m := range hpa.Spec.Metrics {
		input.Metrics = append(input.Metrics, buildMetricContractMetric(m, currentMetricMap))
	}

	return input
}

// buildCurrentMetricDataMap builds a set of "Type/Name" keys for metrics that
// have current data in the HPA status.
func buildCurrentMetricDataMap(hpa *autoscalingv2.HorizontalPodAutoscaler) map[string]bool {
	currentMetricMap := make(map[string]bool)
	for _, m := range hpa.Status.CurrentMetrics {
		switch {
		case m.Resource != nil:
			currentMetricMap[fmt.Sprintf("Resource/%s", m.Resource.Name)] = true
		case m.ContainerResource != nil:
			currentMetricMap[fmt.Sprintf("ContainerResource/%s/%s", m.ContainerResource.Container, m.ContainerResource.Name)] = true
		case m.Pods != nil:
			currentMetricMap[fmt.Sprintf("Pods/%s", m.Pods.Metric.Name)] = true
		case m.Object != nil:
			currentMetricMap[fmt.Sprintf("Object/%s", m.Object.Metric.Name)] = true
		case m.External != nil:
			currentMetricMap[fmt.Sprintf("External/%s", m.External.Metric.Name)] = true
		}
	}
	return currentMetricMap
}

// buildMetricContractMetric converts a single HPA spec metric into a contract
// metric and records its hasCurrentData flag against the provided map.
func buildMetricContractMetric(m autoscalingv2.MetricSpec, currentMetricMap map[string]bool) hpaanalysis.MetricContractMetric {
	metric := hpaanalysis.MetricContractMetric{
		Type: string(m.Type),
	}

	switch {
	case m.Resource != nil:
		metric.Name = string(m.Resource.Name)
		metric.APIGroup = "metrics.k8s.io/v1beta1"
		currentMetricMap[fmt.Sprintf("Resource/%s", m.Resource.Name)] = true
	case m.ContainerResource != nil:
		metric.Name = string(m.ContainerResource.Name)
		metric.APIGroup = "metrics.k8s.io/v1beta1"
	case m.Pods != nil:
		metric.Name = m.Pods.Metric.Name
		metric.APIGroup = "custom.metrics.k8s.io/v1beta1"
		if m.Pods.Metric.Selector != nil {
			metric.Selector = m.Pods.Metric.Selector.String()
		}
	case m.Object != nil:
		metric.Name = m.Object.Metric.Name
		metric.APIGroup = "custom.metrics.k8s.io/v1beta1"
		if m.Object.Metric.Selector != nil {
			metric.Selector = m.Object.Metric.Selector.String()
		}
	case m.External != nil:
		metric.Name = m.External.Metric.Name
		metric.APIGroup = "external.metrics.k8s.io/v1beta1"
	}

	// Check if current data exists
	metricKey := fmt.Sprintf("%s/%s", metric.Type, metric.Name)
	if metric.Type == "ContainerResource" && m.ContainerResource != nil {
		metricKey = fmt.Sprintf("%s/%s/%s", metric.Type, m.ContainerResource.Container, metric.Name)
	}
	metric.HasCurrentData = currentMetricMap[metricKey]

	return metric
}

// checkAPIServiceAvailability checks if a metrics API is available via discovery.
func checkAPIServiceAvailability(_ context.Context, client *kube.Client, groupVersion string) hpaanalysis.APIServiceStatus {
	_, err := client.Interface.Discovery().ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return hpaanalysis.APIServiceStatus{
			Available: false,
			Message:   err.Error(),
		}
	}
	return hpaanalysis.APIServiceStatus{
		Available: true,
		Message:   groupVersion,
	}
}
