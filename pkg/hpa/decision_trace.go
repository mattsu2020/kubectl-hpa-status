package hpa

import (
	"fmt"
	"math"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// BuildDecisionTrace builds a human-oriented decision trace using only HPA
// spec/status fields exposed by the stable Kubernetes API.
func BuildDecisionTrace(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) *DecisionTrace {
	if hpa == nil {
		return nil
	}
	trace := &DecisionTrace{
		Namespace:       hpa.Namespace,
		Name:            hpa.Name,
		CurrentReplicas: hpa.Status.CurrentReplicas,
		VisibleDesired:  hpa.Status.DesiredReplicas,
		MaxReplicas:     hpa.Spec.MaxReplicas,
		MinReplicas:     minReplicas,
		Confidence:      ConfidenceMedium,
	}

	var maxRaw int32
	for _, metric := range hpa.Status.CurrentMetrics {
		entry := buildDecisionTraceMetric(hpa, metric)
		if entry.Name == "" {
			continue
		}
		if entry.RawDesired != nil && *entry.RawDesired > maxRaw {
			maxRaw = *entry.RawDesired
		}
		trace.Metrics = append(trace.Metrics, entry)
	}
	trace.EstimatedRawDesired = maxRaw
	trace.LimitCheck = buildDecisionLimitCheck(trace)
	trace.Stabilization = buildDecisionStabilization(hpa)
	trace.FinalInterpretation = buildDecisionFinalInterpretation(trace)

	if len(trace.Metrics) == 0 {
		trace.Confidence = ConfidenceLow
		trace.Notes = append(trace.Notes, "No current metrics are visible in HPA status; raw desired replicas cannot be estimated.")
	}
	if len(trace.Metrics) > 1 {
		trace.Notes = append(trace.Notes, "For multi-metric HPAs, Kubernetes chooses the largest per-metric recommendation; per-metric recommendations are estimated here.")
	}
	trace.Notes = append(trace.Notes, "Missing metrics and not-yet-ready pod adjustments are controller-internal; this trace marks inferred math with medium or low confidence.")
	return trace
}

func buildDecisionTraceMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) DecisionTraceMetric {
	name, ratio := metricImpactRatio(hpa, metric)
	entry := DecisionTraceMetric{
		Name:       name,
		Type:       string(metric.Type),
		Confidence: ConfidenceMedium,
	}
	if name == "" {
		return entry
	}
	entry.Current, entry.Target = metricDisplayValues(hpa, metric)
	if ratio == nil || hpa.Status.CurrentReplicas == 0 {
		entry.Confidence = ConfidenceLow
		return entry
	}
	raw := int32(math.Ceil(float64(hpa.Status.CurrentReplicas) * *ratio))
	entry.Ratio = ratio
	entry.RawDesired = &raw
	entry.Direction = "none"
	if raw > hpa.Status.CurrentReplicas {
		entry.Direction = "up"
	} else if raw < hpa.Status.CurrentReplicas {
		entry.Direction = "down"
	}
	entry.Formula = fmt.Sprintf("ceil(%d * %.3f) = %d", hpa.Status.CurrentReplicas, *ratio, raw)
	return entry
}

func metricDisplayValues(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, string) {
	switch metric.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource == nil {
			return "", ""
		}
		target := FindResourceTarget(hpa, string(metric.Resource.Name))
		current := metricValueStatusString(metric.Resource.Current)
		return current, target
	case autoscalingv2.ContainerResourceMetricSourceType:
		if metric.ContainerResource == nil {
			return "", ""
		}
		target := FindContainerResourceTarget(hpa, string(metric.ContainerResource.Name), metric.ContainerResource.Container)
		current := metricValueStatusString(metric.ContainerResource.Current)
		return current, target
	case autoscalingv2.PodsMetricSourceType:
		if metric.Pods == nil {
			return "", ""
		}
		target := FindPodsTarget(hpa, metric.Pods.Metric.Name, metric.Pods.Metric.Selector)
		current := metricValueStatusString(metric.Pods.Current)
		return current, target
	case autoscalingv2.ObjectMetricSourceType:
		if metric.Object == nil {
			return "", ""
		}
		target := FindObjectTarget(hpa, metric.Object.Metric.Name, metric.Object.Metric.Selector, metric.Object.DescribedObject)
		current := metricValueStatusString(metric.Object.Current)
		return current, target
	case autoscalingv2.ExternalMetricSourceType:
		if metric.External == nil {
			return "", ""
		}
		target := FindExternalTarget(hpa, metric.External.Metric.Name, metric.External.Metric.Selector)
		current := metricValueStatusString(metric.External.Current)
		return current, target
	default:
		return "", ""
	}
}

func metricValueStatusString(v autoscalingv2.MetricValueStatus) string {
	switch {
	case v.AverageUtilization != nil:
		return fmt.Sprintf("%d%%", *v.AverageUtilization)
	case v.AverageValue != nil:
		return v.AverageValue.String()
	case v.Value != nil:
		return v.Value.String()
	default:
		return "<unknown>"
	}
}

func buildDecisionLimitCheck(trace *DecisionTrace) string {
	if trace == nil {
		return ""
	}
	switch {
	case trace.EstimatedRawDesired > trace.MaxReplicas:
		return fmt.Sprintf("maxReplicas=%d caps estimated raw desired replicas %d", trace.MaxReplicas, trace.EstimatedRawDesired)
	case trace.EstimatedRawDesired > 0 && trace.EstimatedRawDesired < trace.MinReplicas:
		return fmt.Sprintf("minReplicas=%d raises estimated raw desired replicas %d", trace.MinReplicas, trace.EstimatedRawDesired)
	case trace.EstimatedRawDesired > 0:
		return "estimated raw desired replicas are within minReplicas/maxReplicas"
	default:
		return "raw desired replicas unavailable"
	}
}

func buildDecisionStabilization(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if remaining := estimateStabilizationRemaining(hpa); remaining != nil && *remaining > 0 {
		window := scaleDownStabilizationWindow(hpa)
		if window != nil {
			return fmt.Sprintf("scaleDown window active, about %ds remaining in %ds window", *remaining, *window)
		}
		return "scaleDown window active"
	}
	return "scaleDown window not visibly active"
}

func buildDecisionFinalInterpretation(trace *DecisionTrace) string {
	if trace == nil {
		return ""
	}
	if trace.EstimatedRawDesired > trace.MaxReplicas {
		return fmt.Sprintf("HPA wants about %d replicas from visible metrics, but maxReplicas=%d caps it.", trace.EstimatedRawDesired, trace.MaxReplicas)
	}
	if trace.EstimatedRawDesired > 0 {
		return fmt.Sprintf("HPA wants about %d replicas from visible metrics; API status reports desiredReplicas=%d.", trace.EstimatedRawDesired, trace.VisibleDesired)
	}
	return fmt.Sprintf("HPA status reports desiredReplicas=%d; metric math is not visible enough to estimate raw desired replicas.", trace.VisibleDesired)
}
