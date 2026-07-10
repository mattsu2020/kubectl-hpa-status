package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// --- Resource metric handler ---

type resourceHandler struct{}

func (resourceHandler) FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.Resource == nil {
		return Metric{Type: MetricTypeResource, Text: "Resource metric: <missing status>"}
	}
	targetSpec := FindResourceTargetSpec(hpa, string(metric.Resource.Name))
	target := FormatMetricTarget(targetSpec)
	current := FormatMetricValue(metric.Resource.Current.AverageUtilization, metric.Resource.Current.AverageValue)
	ratio, note := calculateRatioAndNote(metric.Resource.Current, targetSpec, target)
	text := appendRatioAndNote(
		fmt.Sprintf("Resource %s current=%s target=%s", metric.Resource.Name, current, target),
		ratio, note,
	)
	return Metric{
		Type: MetricTypeResource, Name: string(metric.Resource.Name),
		Current: current, Target: target, Ratio: ratio, Note: note, Text: text,
	}
}

func (resourceHandler) ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	if metric.Resource == nil {
		return "", nil
	}
	targetSpec := FindResourceTargetSpec(hpa, string(metric.Resource.Name))
	ratio, _ := calculateRatioAndNote(metric.Resource.Current, targetSpec, FormatMetricTarget(targetSpec))
	return string(metric.Resource.Name), ratio
}

func (resourceHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	if spec.Resource != nil {
		return "Resource", string(spec.Resource.Name)
	}
	return "Resource", "<unknown>"
}

func (resourceHandler) MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	return spec.Resource != nil && current.Resource != nil && spec.Resource.Name == current.Resource.Name
}

func (resourceHandler) Remediation(spec autoscalingv2.MetricSpec) string {
	if spec.Resource == nil {
		return ""
	}
	return fmt.Sprintf(
		"Resource metric %q is missing. Verify that the metrics-server is running and can scrape kubelet metrics: kubectl top pods -n <namespace>. "+
			"Check metrics-server deployment: kubectl get deploy -n kube-system -l k8s-app=metrics-server. "+
			"Verify the API service: kubectl get apiservice v1beta1.metrics.k8s.io.",
		spec.Resource.Name,
	)
}

func (resourceHandler) DisplayName(status autoscalingv2.MetricStatus) string {
	if status.Resource != nil {
		return string(status.Resource.Name)
	}
	return "Resource"
}
