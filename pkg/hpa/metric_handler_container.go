package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// --- ContainerResource metric handler ---

type containerResourceHandler struct{}

func (containerResourceHandler) FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.ContainerResource == nil {
		return Metric{Type: MetricTypeContainerResource, Text: "ContainerResource metric: <missing status>"}
	}
	targetSpec := FindContainerResourceTargetSpec(hpa, string(metric.ContainerResource.Name), metric.ContainerResource.Container)
	target := FormatMetricTarget(targetSpec)
	current := FormatMetricValueStatus(metric.ContainerResource.Current)
	ratio, note := calculateRatioAndNote(metric.ContainerResource.Current, targetSpec, target)
	text := appendRatioAndNote(
		fmt.Sprintf("ContainerResource %s/%s current=%s target=%s", metric.ContainerResource.Container, metric.ContainerResource.Name, current, target),
		ratio, note,
	)
	return Metric{
		Type: MetricTypeContainerResource, Name: fmt.Sprintf("%s/%s", metric.ContainerResource.Container, metric.ContainerResource.Name),
		Current: current, Target: target, Ratio: ratio, Note: note, Text: text,
	}
}

func (containerResourceHandler) ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	if metric.ContainerResource == nil {
		return "", nil
	}
	targetSpec := FindContainerResourceTargetSpec(hpa, string(metric.ContainerResource.Name), metric.ContainerResource.Container)
	target := FormatMetricTarget(targetSpec)
	ratio := utilizationRatio(metric.ContainerResource.Current.AverageUtilization, target)
	name := fmt.Sprintf("%s/%s", metric.ContainerResource.Container, metric.ContainerResource.Name)
	if ratio != nil {
		return name, ratio
	}
	if metric.ContainerResource.Current.AverageValue != nil && targetSpec.AverageValue != nil {
		ratio = quantityRatio(metric.ContainerResource.Current.AverageValue, targetSpec.AverageValue)
		return name, ratio
	}
	return name, nil
}

func (containerResourceHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	if spec.ContainerResource != nil {
		return "ContainerResource", fmt.Sprintf("%s/%s", spec.ContainerResource.Container, spec.ContainerResource.Name)
	}
	return "ContainerResource", "<unknown>"
}

func (containerResourceHandler) MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	return spec.ContainerResource != nil && current.ContainerResource != nil &&
		spec.ContainerResource.Name == current.ContainerResource.Name &&
		spec.ContainerResource.Container == current.ContainerResource.Container
}

func (containerResourceHandler) Remediation(spec autoscalingv2.MetricSpec) string {
	if spec.ContainerResource == nil {
		return ""
	}
	return fmt.Sprintf(
		"ContainerResource metric %s/%s is missing. Verify the metrics-server is running and the container is reporting resource usage.",
		spec.ContainerResource.Container, spec.ContainerResource.Name,
	)
}

func (containerResourceHandler) DisplayName(status autoscalingv2.MetricStatus) string {
	if status.ContainerResource != nil {
		return fmt.Sprintf("%s/%s", status.ContainerResource.Container, status.ContainerResource.Name)
	}
	return "ContainerResource"
}
