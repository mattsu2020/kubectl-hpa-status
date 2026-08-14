package simulate

import (
	"fmt"
	"math"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func recomputeSimulatedDesired(hpa *autoscalingv2.HorizontalPodAutoscaler) {
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	scaledToZero := simulatedConditionTrue(hpa, autoscalingv2.ScaledToZero)
	if hpa.Status.CurrentReplicas == 0 &&
		!shouldComputeSimulatedMetricsFromZero(hpa, minReplicas, scaledToZero) {
		hpa.Status.DesiredReplicas = 0
		replaceSimulatedScalingActive(hpa, corev1.ConditionFalse, "ScalingDisabled",
			"scaling is disabled because the target was manually scaled to zero")
		replaceSimulatedControllerConditions(hpa, false, "DesiredWithinRange")
		return
	}

	desired, found := simulatedDesiredFromMetrics(hpa)
	if !found {
		desired = hpa.Status.DesiredReplicas
	}
	// Missing, duplicate, or malformed metrics conservatively block a
	// scale-down. Comparing slice lengths is insufficient because two status
	// entries can describe the same selector/container/object while another
	// spec metric is absent.
	if !hasOneToOneCanonicalMetricStatus(hpa) && desired < hpa.Status.CurrentReplicas {
		desired = hpa.Status.CurrentReplicas
	}

	desired, limited, limitedReason := normalizeSimulatedDesired(
		hpa,
		desired,
		minReplicas,
		hpa.Spec.MaxReplicas,
	)
	if hpa.Status.CurrentReplicas == 0 && minReplicas != 0 && scaledToZero && desired < minReplicas {
		desired = minReplicas
	}
	hpa.Status.DesiredReplicas = desired
	if hpa.Status.CurrentReplicas == 0 && scaledToZero && found {
		replaceSimulatedScalingActive(hpa, corev1.ConditionTrue, "ValidMetricFound",
			"the projected replica count was calculated from visible metric data")
	}
	replaceSimulatedControllerConditions(hpa, limited, limitedReason)
}

// hasOneToOneCanonicalMetricStatus reports whether every spec metric has
// exactly one usable current status entry with the same canonical identity.
// It deliberately fails closed for duplicate identities and malformed
// selectors/source payloads so an estimated simulation cannot project a
// scale-down from incomplete evidence.
func hasOneToOneCanonicalMetricStatus(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	if hpa == nil || len(hpa.Spec.Metrics) != len(hpa.Status.CurrentMetrics) {
		return false
	}

	specTargets := make(map[MetricID]autoscalingv2.MetricTargetType, len(hpa.Spec.Metrics))
	for i := range hpa.Spec.Metrics {
		spec := &hpa.Spec.Metrics[i]
		id, err := metricIDFromSpecInvoker(*spec)
		if err != nil {
			return false
		}
		if _, duplicate := specTargets[id]; duplicate {
			return false
		}
		target := MetricTargetPointer(spec)
		if target == nil {
			return false
		}
		specTargets[id] = target.Type
	}

	statusIDs := make(map[MetricID]struct{}, len(hpa.Status.CurrentMetrics))
	for _, current := range hpa.Status.CurrentMetrics {
		id, err := metricIDFromStatusInvoker(current)
		if err != nil {
			return false
		}
		if _, duplicate := statusIDs[id]; duplicate {
			return false
		}
		targetType, specified := specTargets[id]
		if !specified {
			return false
		}
		value, ok := currentMetricValueStatusInvoker(current)
		if !ok || !hasMetricValueForTargetInvoker(value, targetType) {
			return false
		}
		statusIDs[id] = struct{}{}
	}

	for id := range specTargets {
		if _, found := statusIDs[id]; !found {
			return false
		}
	}
	return true
}

func simulatedDesiredFromMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) (int32, bool) {
	var desired int32
	found := false
	for _, metric := range hpa.Status.CurrentMetrics {
		_, ratio := metricImpactRatioInvoker(hpa, metric)
		if ratio == nil || math.IsNaN(*ratio) || math.IsInf(*ratio, 0) || *ratio < 0 {
			continue
		}
		metricDesired, projectable := estimatedSimulatedMetricDesired(hpa, metric, *ratio)
		if !projectable {
			continue
		}
		if !found || metricDesired > desired {
			desired = metricDesired
			found = true
		}
	}
	return desired, found
}

func estimatedSimulatedMetricDesired(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	metric autoscalingv2.MetricStatus,
	ratio float64,
) (int32, bool) {
	if hpa.Status.CurrentReplicas != 0 {
		return estimatedDesiredForRatioInvoker(hpa, ratio), true
	}

	// At zero replicas, the controller's Object/External Value algorithm uses
	// ceil(currentValue/targetValue), which is exactly ceil(ratio), and skips
	// tolerance. Per-pod targets and pod/resource metrics need pod state that is
	// unavailable at zero and are intentionally not guessed.
	if metric.Type != autoscalingv2.ObjectMetricSourceType &&
		metric.Type != autoscalingv2.ExternalMetricSourceType {
		return 0, false
	}
	target, ok := matchingMetricTargetInvoker(hpa, metric)
	if !ok || target.Type != autoscalingv2.ValueMetricType || target.Value == nil {
		return 0, false
	}
	projected := math.Ceil(ratio)
	if projected > math.MaxInt32 {
		return math.MaxInt32, true
	}
	return int32(projected), true
}

func validateSimulatedZeroProjection(hpa *autoscalingv2.HorizontalPodAutoscaler) error {
	if hpa.Status.CurrentReplicas != 0 {
		return nil
	}
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	scaledToZero := simulatedConditionTrue(hpa, autoscalingv2.ScaledToZero)
	if !shouldComputeSimulatedMetricsFromZero(hpa, minReplicas, scaledToZero) {
		return nil
	}
	for _, metric := range hpa.Status.CurrentMetrics {
		_, ratio := metricImpactRatioInvoker(hpa, metric)
		if ratio == nil || math.IsNaN(*ratio) || math.IsInf(*ratio, 0) || *ratio < 0 {
			continue
		}
		if _, ok := estimatedSimulatedMetricDesired(hpa, metric, *ratio); ok {
			return nil
		}
	}
	return fmt.Errorf("%w: scale-from-zero projection requires a visible Object or External Value metric",
		ErrUnsupportedSimulationSemantics)
}

func shouldComputeSimulatedMetricsFromZero(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	minReplicas int32,
	scaledToZero bool,
) bool {
	if !scaledToZero {
		return false
	}
	return minReplicas != 0 || hasSimulatedObjectOrExternalMetric(hpa)
}

func hasSimulatedObjectOrExternalMetric(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ObjectMetricSourceType ||
			metric.Type == autoscalingv2.ExternalMetricSourceType {
			return true
		}
	}
	return false
}
