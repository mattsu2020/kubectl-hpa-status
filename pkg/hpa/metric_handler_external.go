package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// --- External metric handler ---

type externalHandler struct{}

func (externalHandler) FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.External == nil {
		return Metric{Type: MetricTypeExternal, Text: "External metric: <missing status>"}
	}
	targetSpec := FindExternalTargetSpec(hpa, metric.External.Metric.Name, metric.External.Metric.Selector)
	target := FormatMetricTarget(targetSpec)
	current := FormatMetricValueStatus(metric.External.Current)
	ratio, note := calculateRatioAndNote(metric.External.Current, targetSpec, target)
	selector := FormatMetricSelector(metric.External.Metric.Selector)
	text := fmt.Sprintf("External %s current=%s target=%s", metric.External.Metric.Name, current, target)
	if selector != "" {
		text = fmt.Sprintf("%s selector=%q", text, selector)
	}
	text = appendRatioAndNote(text, ratio, note)
	return Metric{
		Type: MetricTypeExternal, Name: metric.External.Metric.Name, Selector: selector,
		Current: current, Target: target, Ratio: ratio, Note: note, Text: text,
	}
}

func (externalHandler) ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	if metric.External == nil {
		return "", nil
	}
	targetSpec := FindExternalTargetSpec(hpa, metric.External.Metric.Name, metric.External.Metric.Selector)
	ratio, _ := calculateRatioAndNote(metric.External.Current, targetSpec, FormatMetricTarget(targetSpec))
	return metric.External.Metric.Name, ratio
}

func (externalHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	if spec.External != nil {
		return "External", spec.External.Metric.Name
	}
	return "External", "<unknown>"
}

func (externalHandler) MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	if spec.External == nil || current.External == nil {
		return false
	}
	if spec.External.Metric.Name != current.External.Metric.Name {
		return false
	}
	return selectorsEqual(spec.External.Metric.Selector, current.External.Metric.Selector)
}

func (externalHandler) Remediation(spec autoscalingv2.MetricSpec) string {
	if spec.External == nil {
		return ""
	}
	return fmt.Sprintf(
		"External metric %q is missing. Verify the external metrics adapter is serving the metric and check adapter logs for errors. "+
			"Check the API service: kubectl get --raw /apis/external.metrics.k8s.io/v1beta1. "+
			"If using Prometheus Adapter, check the API service and rules ConfigMap.",
		spec.External.Metric.Name,
	)
}

func (externalHandler) DisplayName(status autoscalingv2.MetricStatus) string {
	if status.External != nil {
		return status.External.Metric.Name
	}
	return "External"
}
