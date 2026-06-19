package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// --- Object metric handler ---

type objectHandler struct{}

func (objectHandler) FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.Object == nil {
		return Metric{Type: MetricTypeObject, Text: "Object metric: <missing status>"}
	}
	targetSpec := FindObjectTargetSpec(hpa, metric.Object.Metric.Name, metric.Object.Metric.Selector, metric.Object.DescribedObject)
	target := FormatMetricTarget(targetSpec)
	current := FormatMetricValueStatus(metric.Object.Current)
	ratio, note := calculateRatioAndNote(metric.Object.Current, targetSpec, target)
	name := fmt.Sprintf("%s/%s", metric.Object.DescribedObject.Kind, metric.Object.DescribedObject.Name)
	selector := FormatMetricSelector(metric.Object.Metric.Selector)
	text := fmt.Sprintf("Object %s %s current=%s target=%s", name, metric.Object.Metric.Name, current, target)
	if selector != "" {
		text = fmt.Sprintf("%s selector=%q", text, selector)
	}
	text = appendRatioAndNote(text, ratio, note)
	return Metric{
		Type: MetricTypeObject, Name: metric.Object.Metric.Name, Selector: selector,
		Object: name, Current: current, Target: target, Ratio: ratio, Note: note, Text: text,
	}
}

func (objectHandler) ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	if metric.Object == nil {
		return "", nil
	}
	targetSpec := FindObjectTargetSpec(hpa, metric.Object.Metric.Name, metric.Object.Metric.Selector, metric.Object.DescribedObject)
	ratio, _ := calculateRatioAndNote(metric.Object.Current, targetSpec, FormatMetricTarget(targetSpec))
	return metric.Object.Metric.Name, ratio
}

func (objectHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	if spec.Object != nil {
		return "Object", spec.Object.Metric.Name
	}
	return "Object", "<unknown>"
}

func (objectHandler) MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	if spec.Object == nil || current.Object == nil {
		return false
	}
	if spec.Object.Metric.Name != current.Object.Metric.Name {
		return false
	}
	if !selectorsEqual(spec.Object.Metric.Selector, current.Object.Metric.Selector) {
		return false
	}
	return spec.Object.DescribedObject.Kind == current.Object.DescribedObject.Kind &&
		spec.Object.DescribedObject.Name == current.Object.DescribedObject.Name
}

func (objectHandler) Remediation(spec autoscalingv2.MetricSpec) string {
	if spec.Object == nil {
		return ""
	}
	return fmt.Sprintf(
		"Object metric %q is missing. Verify the described object %s/%s exists and the metrics adapter can retrieve its values.",
		spec.Object.Metric.Name, spec.Object.DescribedObject.Kind, spec.Object.DescribedObject.Name,
	)
}

func (objectHandler) DisplayName(status autoscalingv2.MetricStatus) string {
	if status.Object != nil {
		return status.Object.Metric.Name
	}
	return "Object"
}
