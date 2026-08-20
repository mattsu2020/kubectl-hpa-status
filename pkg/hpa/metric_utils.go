package hpa

import (
	"fmt"
	"sort"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SummarizeDirection returns a one-line summary of the HPA scaling direction.
func SummarizeDirection(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) string {
	summary, _ := SummarizeDirectionWithKey(hpa, minReplicas)
	return summary
}

// SummarizeDirectionWithKey returns the same one-line summary as
// SummarizeDirection plus the stable i18n key (e.g. "dir_scale_up") that
// identifies which branch produced the summary. Callers that need to localize
// the summary should use this form and translate via the key; the English
// summary is the fallback when no translator is wired in. The key is empty
// only if hpa is nil and SummarizeDirection's nil-handling branch ran, which
// cannot happen for a real Analysis (Analysis is never built from a nil HPA).
func SummarizeDirectionWithKey(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) (string, string) {
	if hpa == nil {
		return "HPA data is unavailable.", "dir_unavailable"
	}
	if condition := FindCondition(hpa, ConditionScalingActive); condition != nil && condition.Status != corev1.ConditionTrue {
		return "HPA cannot currently compute a scaling recommendation from metrics.", "dir_inactive"
	}
	if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas > 0 {
		if minReplicas == 0 {
			return "HPA wants to scale to zero (cold start will occur on next scale-up).", "dir_scale_to_zero"
		}
		return "HPA has no visible desired replica recommendation in status.", "dir_no_recommendation"
	}
	if minReplicas == 0 && hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas == 0 {
		return "HPA is scaled to zero (minReplicas=0); awaiting trigger to scale up.", "dir_scaled_to_zero"
	}

	return summarizeDirectionFromReplicas(hpa.Status.CurrentReplicas, hpa.Status.DesiredReplicas, hpa.Spec.MaxReplicas, minReplicas)
}

func summarizeDirectionFromReplicas(current, desired, maxReplicas, minReplicas int32) (string, string) {
	switch {
	case desired == maxReplicas:
		return "HPA is at maxReplicas.", "dir_at_max"
	case desired > current:
		return "HPA currently wants to scale up.", "dir_scale_up"
	case desired < current:
		return "HPA currently wants to scale down.", "dir_scale_down"
	case desired == minReplicas && minReplicas == 0:
		return "HPA is at minReplicas (scale-to-zero enabled).", "dir_at_min_scale_to_zero"
	case desired == minReplicas:
		return "HPA is at minReplicas.", "dir_at_min"
	default:
		return "HPA currently keeps the replica count unchanged.", "dir_unchanged"
	}
}

// FindCondition is re-exported from pkg/hpa/internal/conditions; see
// conditions.go for the canonical implementation.

// calculateRatioAndNote derives the utilization/quantity ratio and the
// human-readable comparison note from the numeric current value and target.
// Both inputs come straight from the HPA status/spec — no formatted-string
// round trips, so an unparseable display format can never silently drop a
// ratio.
func calculateRatioAndNote(currentVal autoscalingv2.MetricValueStatus, targetVal autoscalingv2.MetricTarget) (*float64, string) {
	var ratio *float64
	var note string

	switch {
	case currentVal.AverageUtilization != nil:
		ratio = utilizationRatio(currentVal.AverageUtilization, targetVal.AverageUtilization)
		note = CompareMetricToTarget(currentVal.AverageUtilization, targetVal.AverageUtilization)
	case currentVal.AverageValue != nil && targetVal.AverageValue != nil:
		ratio = quantityRatio(currentVal.AverageValue, targetVal.AverageValue)
		note = CompareQuantityToTarget(currentVal.AverageValue, targetVal.AverageValue)
	case currentVal.Value != nil && targetVal.Value != nil:
		ratio = quantityRatio(currentVal.Value, targetVal.Value)
		note = CompareQuantityToTarget(currentVal.Value, targetVal.Value)
	}
	return ratio, note
}

// numericReading extracts the typed numeric current/target pair that the
// formatted Current/Target strings render. Utilization becomes a percent
// value; quantities become their canonical decimal value, so readings stay
// comparable across snapshots even when the server formats them with
// different suffixes ("812m" and "0.812" both yield 0.812). A nil value means
// the status carried no numeric field.
func numericReading(currentVal autoscalingv2.MetricValueStatus, targetVal autoscalingv2.MetricTarget) (value, target *float64, unit string) {
	numeric := func(v float64) *float64 { return &v }
	switch {
	case currentVal.AverageUtilization != nil:
		value = numeric(float64(*currentVal.AverageUtilization))
		if targetVal.AverageUtilization != nil {
			target = numeric(float64(*targetVal.AverageUtilization))
		}
		return value, target, "%"
	case currentVal.AverageValue != nil:
		value = numeric(currentVal.AverageValue.AsApproximateFloat64())
		if targetVal.AverageValue != nil {
			target = numeric(targetVal.AverageValue.AsApproximateFloat64())
		}
		return value, target, ""
	case currentVal.Value != nil:
		value = numeric(currentVal.Value.AsApproximateFloat64())
		if targetVal.Value != nil {
			target = numeric(targetVal.Value.AsApproximateFloat64())
		}
		return value, target, ""
	}
	return nil, nil, ""
}

// CompareMetricToTarget returns a comparison description for utilization vs
// target. Both values are the numeric AverageUtilization fields straight off
// the HPA status/spec; a nil target means the spec does not carry a utilization
// target, in which case there is nothing to compare.
func CompareMetricToTarget(utilization, target *int32) string {
	if utilization == nil || target == nil {
		return ""
	}

	switch {
	case *utilization > *target:
		return "current value is above target"
	case *utilization < *target:
		return "current value is below target"
	default:
		return "current value equals target"
	}
}

// metricTargetUtilization returns the numeric utilization target of the named
// resource metric, or nil when the spec carries no utilization target.
func metricTargetUtilization(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) *int32 {
	return FindResourceTargetSpec(hpa, name).AverageUtilization
}

// MetricOutsideTarget finds a resource metric whose ratio differs from 1.0.
func MetricOutsideTarget(hpa *autoscalingv2.HorizontalPodAutoscaler) (MetricImpactGuess, bool) {
	if hpa == nil {
		return MetricImpactGuess{}, false
	}
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type != autoscalingv2.ResourceMetricSourceType || metric.Resource == nil {
			continue
		}
		ratio := utilizationRatio(metric.Resource.Current.AverageUtilization, metricTargetUtilization(hpa, string(metric.Resource.Name)))
		if ratio != nil && *ratio != 1 {
			return MetricImpactGuess{Name: string(metric.Resource.Name), Ratio: *ratio}, true
		}
	}

	return MetricImpactGuess{}, false
}

// MostInfluentialMetric estimates the metric that would request the largest
// desired replica count across Resource, ContainerResource, External, Pods,
// and Object metrics. Kubernetes selects the maximum recommendation rather
// than the metric with the largest absolute distance from target.
func MostInfluentialMetric(hpa *autoscalingv2.HorizontalPodAutoscaler) (MetricImpactGuess, bool) {
	if hpa == nil {
		return MetricImpactGuess{}, false
	}
	var best MetricImpactGuess
	var bestDesired int32
	found := false
	hasDeviation := false

	for _, metric := range hpa.Status.CurrentMetrics {
		name, ratio := metricImpactRatio(hpa, metric)
		if ratio == nil {
			continue
		}
		if *ratio != 1 {
			hasDeviation = true
		}
		desired := estimatedDesiredForRatio(hpa, *ratio)
		if !found || desired > bestDesired {
			bestDesired = desired
			found = true
			note := fmt.Sprintf("largest estimated desired replica count %d (ceil of %d current replicas x %.3f ratio, subject to directional tolerance)",
				desired, hpa.Status.CurrentReplicas, *ratio)
			best = MetricImpactGuess{
				Name:  name,
				Ratio: *ratio,
				Note:  note,
			}
		}
	}

	return best, found && hasDeviation
}

func prioritizedConditions(conditions []autoscalingv2.HorizontalPodAutoscalerCondition) []autoscalingv2.HorizontalPodAutoscalerCondition {
	out := append([]autoscalingv2.HorizontalPodAutoscalerCondition(nil), conditions...)
	priority := map[autoscalingv2.HorizontalPodAutoscalerConditionType]int{
		ConditionScalingActive:  0,
		ConditionAbleToScale:    1,
		ConditionScalingLimited: 2,
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := priority[out[i].Type]
		right := priority[out[j].Type]
		if _, ok := priority[out[i].Type]; !ok {
			left = 100
		}
		if _, ok := priority[out[j].Type]; !ok {
			right = 100
		}
		return left < right
	})
	return out
}

func utilizationRatio(utilization, target *int32) *float64 {
	if utilization == nil || target == nil || *target == 0 {
		return nil
	}
	ratio := float64(*utilization) / float64(*target)
	return &ratio
}

func quantityRatio(current, target *resource.Quantity) *float64 {
	if current == nil || target == nil || target.IsZero() {
		return nil
	}
	ratio := current.AsApproximateFloat64() / target.AsApproximateFloat64()
	return &ratio
}

// CompareQuantityToTarget returns a comparison description for quantity values.
func CompareQuantityToTarget(current, target *resource.Quantity) string {
	if current == nil || target == nil {
		return ""
	}
	cmp := current.Cmp(*target)
	switch {
	case cmp > 0:
		return "current value is above target"
	case cmp < 0:
		return "current value is below target"
	default:
		return "current value equals target"
	}
}

func selectorSuffix(selector *metav1.LabelSelector) string {
	formatted := FormatMetricSelector(selector)
	if formatted == "" {
		return ""
	}
	return fmt.Sprintf(" selector=%q", formatted)
}
