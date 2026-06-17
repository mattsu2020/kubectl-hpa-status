package hpa

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SummarizeDirection returns a one-line summary of the HPA scaling direction.
func SummarizeDirection(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) string {
	if hpa == nil {
		return "HPA data is unavailable."
	}
	if condition := FindCondition(hpa, ConditionScalingActive); condition != nil && condition.Status != corev1.ConditionTrue {
		return "HPA cannot currently compute a scaling recommendation from metrics."
	}
	if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas > 0 {
		if minReplicas == 0 {
			return "HPA wants to scale to zero (cold start will occur on next scale-up)."
		}
		return "HPA has no visible desired replica recommendation in status."
	}
	if minReplicas == 0 && hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas == 0 {
		return "HPA is scaled to zero (minReplicas=0); awaiting trigger to scale up."
	}

	return summarizeDirectionFromReplicas(hpa.Status.CurrentReplicas, hpa.Status.DesiredReplicas, hpa.Spec.MaxReplicas, minReplicas)
}

func summarizeDirectionFromReplicas(current, desired, maxReplicas, minReplicas int32) string {
	switch {
	case desired > current:
		return "HPA currently wants to scale up."
	case desired < current:
		return "HPA currently wants to scale down."
	case desired == maxReplicas:
		return "HPA is at maxReplicas."
	case desired == minReplicas && minReplicas == 0:
		return "HPA is at minReplicas (scale-to-zero enabled)."
	case desired == minReplicas:
		return "HPA is at minReplicas."
	default:
		return "HPA currently keeps the replica count unchanged."
	}
}

// FindCondition returns the HPA condition matching the given type, or nil.
// Returns nil safely when hpa is nil.
func FindCondition(hpa *autoscalingv2.HorizontalPodAutoscaler, conditionType string) *autoscalingv2.HorizontalPodAutoscalerCondition {
	if hpa == nil {
		return nil
	}
	for i := range hpa.Status.Conditions {
		if string(hpa.Status.Conditions[i].Type) == conditionType {
			return &hpa.Status.Conditions[i]
		}
	}
	return nil
}

func calculateRatioAndNote(currentVal autoscalingv2.MetricValueStatus, targetVal autoscalingv2.MetricTarget, targetStr string) (*float64, string) {
	var ratio *float64
	var note string

	switch {
	case currentVal.AverageUtilization != nil:
		ratio = utilizationRatio(currentVal.AverageUtilization, targetStr)
		note = CompareMetricToTarget(currentVal.AverageUtilization, targetStr)
	case currentVal.AverageValue != nil && targetVal.AverageValue != nil:
		ratio = quantityRatio(currentVal.AverageValue, targetVal.AverageValue)
		note = CompareQuantityToTarget(currentVal.AverageValue, targetVal.AverageValue)
	case currentVal.Value != nil && targetVal.Value != nil:
		ratio = quantityRatio(currentVal.Value, targetVal.Value)
		note = CompareQuantityToTarget(currentVal.Value, targetVal.Value)
	}
	return ratio, note
}

// CompareMetricToTarget returns a comparison description for utilization vs target.
func CompareMetricToTarget(utilization *int32, target string) string {
	if utilization == nil || !strings.HasSuffix(target, "%") {
		return ""
	}

	targetUtilization, ok := parsePercent(target)
	if !ok {
		return ""
	}

	switch {
	case *utilization > targetUtilization:
		return "current value is above target"
	case *utilization < targetUtilization:
		return "current value is below target"
	default:
		return "current value equals target"
	}
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
		ratio := utilizationRatio(metric.Resource.Current.AverageUtilization, FindResourceTarget(hpa, string(metric.Resource.Name)))
		if ratio != nil && *ratio != 1 {
			return MetricImpactGuess{Name: string(metric.Resource.Name), Ratio: *ratio}, true
		}
	}

	return MetricImpactGuess{}, false
}

// MostInfluentialMetric estimates which metric has the largest scaling impact
// across all metric types: Resource, ContainerResource, External, Pods, and Object.
func MostInfluentialMetric(hpa *autoscalingv2.HorizontalPodAutoscaler) (MetricImpactGuess, bool) {
	if hpa == nil {
		return MetricImpactGuess{}, false
	}
	var best MetricImpactGuess
	var bestScore float64

	for _, metric := range hpa.Status.CurrentMetrics {
		name, ratio := metricImpactRatio(hpa, metric)
		if ratio == nil {
			continue
		}
		distance := *ratio - 1
		if distance < 0 {
			distance = -distance
		}

		// Score by estimated replica impact: ratio distance * currentReplicas gives
		// a rough estimate of how many replicas this metric would want.
		// Higher impact = more likely to be the winner.
		replicaImpact := distance * float64(hpa.Status.CurrentReplicas)

		if replicaImpact > bestScore {
			bestScore = replicaImpact
			note := "largest visible ratio distance from target"
			if hpa.Status.CurrentReplicas > 0 {
				note = fmt.Sprintf("estimated replica impact %.1f (ratio distance %.3f x %d current replicas)", replicaImpact, distance, hpa.Status.CurrentReplicas)
			}
			best = MetricImpactGuess{
				Name:  name,
				Ratio: *ratio,
				Note:  note,
			}
		}
	}

	return best, bestScore > 0
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

func utilizationRatio(utilization *int32, target string) *float64 {
	if utilization == nil {
		return nil
	}
	targetUtilization, ok := parsePercent(target)
	if !ok || targetUtilization == 0 {
		return nil
	}
	ratio := float64(*utilization) / float64(targetUtilization)
	return &ratio
}

func parsePercent(value string) (int32, bool) {
	if !strings.HasSuffix(value, "%") {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSuffix(value, "%"), 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(n), true
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

func scaleDownStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) *int32 {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}
	return hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
}

// estimateStabilizationRemaining estimates how many seconds remain before
// the scale-down stabilization window expires. Returns nil if the HPA is
// not in a ScaleDownStabilized state or required data is unavailable.
//
// Caveat: Kubernetes downscale stabilization uses the max recommendation
// within the window, not simply LastScaleTime. This estimate is approximate
// and based on LastScaleTime as the best available signal.
func estimateStabilizationRemaining(hpa *autoscalingv2.HorizontalPodAutoscaler) *int64 {
	condition := FindCondition(hpa, ConditionAbleToScale)
	if condition == nil || condition.Reason != "ScaleDownStabilized" {
		return nil
	}
	window := scaleDownStabilizationWindow(hpa)
	if window == nil {
		return nil
	}
	if hpa.Status.LastScaleTime == nil {
		return nil
	}
	elapsed := now().Sub(hpa.Status.LastScaleTime.Time).Seconds()
	remaining := int64(float64(*window) - elapsed)
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}

func selectorSuffix(selector *metav1.LabelSelector) string {
	formatted := FormatMetricSelector(selector)
	if formatted == "" {
		return ""
	}
	return fmt.Sprintf(" selector=%q", formatted)
}
