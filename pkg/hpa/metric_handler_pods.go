package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// --- Pods metric handler ---

type podsHandler struct{}

func (podsHandler) FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.Pods == nil {
		return Metric{Type: MetricTypePods, Text: "Pods metric: <missing status>"}
	}
	targetSpec := FindPodsTargetSpec(hpa, metric.Pods.Metric.Name, metric.Pods.Metric.Selector)
	target := FormatMetricTarget(targetSpec)
	current := FormatMetricValueStatus(metric.Pods.Current)
	ratio, note := calculateRatioAndNote(metric.Pods.Current, targetSpec, target)
	selector := FormatMetricSelector(metric.Pods.Metric.Selector)
	text := fmt.Sprintf("Pods %s current=%s target=%s", metric.Pods.Metric.Name, current, target)
	if selector != "" {
		text = fmt.Sprintf("%s selector=%q", text, selector)
	}
	text = appendRatioAndNote(text, ratio, note)
	return Metric{
		Type: MetricTypePods, Name: metric.Pods.Metric.Name, Selector: selector,
		Current: current, Target: target, Ratio: ratio, Note: note, Text: text,
	}
}

func (podsHandler) ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	if metric.Pods == nil {
		return "", nil
	}
	targetSpec := FindPodsTargetSpec(hpa, metric.Pods.Metric.Name, metric.Pods.Metric.Selector)
	ratio, _ := calculateRatioAndNote(metric.Pods.Current, targetSpec, FormatMetricTarget(targetSpec))
	return metric.Pods.Metric.Name, ratio
}

func (podsHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	if spec.Pods != nil {
		return "Pods", spec.Pods.Metric.Name
	}
	return "Pods", "<unknown>"
}

func (podsHandler) MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	if spec.Pods == nil || current.Pods == nil {
		return false
	}
	if spec.Pods.Metric.Name != current.Pods.Metric.Name {
		return false
	}
	return selectorsEqual(spec.Pods.Metric.Selector, current.Pods.Metric.Selector)
}

func (podsHandler) Remediation(spec autoscalingv2.MetricSpec) string {
	if spec.Pods == nil {
		return ""
	}
	return fmt.Sprintf(
		"Pods metric %q is missing. Verify the custom metrics adapter is serving this metric and check that pods are exposing the expected metric values.",
		spec.Pods.Metric.Name,
	)
}

func (podsHandler) DisplayName(status autoscalingv2.MetricStatus) string {
	if status.Pods != nil {
		return status.Pods.Metric.Name
	}
	return "Pods"
}
