package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// buildMetricContractInput builds the input for metrics contract analysis.
func buildMetricContractInput(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) hpaanalysis.MetricContractInput {
	input := hpaanalysis.MetricContractInput{
		Namespace: hpa.Namespace,
		HPAName:   hpa.Name,
		Target:    fmt.Sprintf("%s/%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name),
		Metrics:   make([]hpaanalysis.MetricContractMetric, 0, len(hpa.Spec.Metrics)),
		APIServices: map[string]hpaanalysis.APIServiceStatus{
			"metrics.k8s.io/v1beta1":         checkAPIServiceAvailability(ctx, client, "metrics.k8s.io/v1beta1"),
			"custom.metrics.k8s.io/v1beta1":  checkAPIServiceAvailability(ctx, client, "custom.metrics.k8s.io/v1beta1"),
			"external.metrics.k8s.io/v1beta1": checkAPIServiceAvailability(ctx, client, "external.metrics.k8s.io/v1beta1"),
		},
	}

	// Build current metric data map for hasCurrentData check
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

	// Extract metrics from HPA spec
	for _, m := range hpa.Spec.Metrics {
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

		input.Metrics = append(input.Metrics, metric)
	}

	return input
}

// checkAPIServiceAvailability checks if a metrics API is available via discovery.
func checkAPIServiceAvailability(ctx context.Context, client *kube.Client, groupVersion string) hpaanalysis.APIServiceStatus {
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
