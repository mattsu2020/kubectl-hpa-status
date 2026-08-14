package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func simulatedConditionTrue(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	conditionType autoscalingv2.HorizontalPodAutoscalerConditionType,
) bool {
	for _, condition := range hpa.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func replaceSimulatedScalingActive(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	status corev1.ConditionStatus,
	reason, message string,
) {
	conditions := make([]autoscalingv2.HorizontalPodAutoscalerCondition, 0, len(hpa.Status.Conditions)+1)
	for _, condition := range hpa.Status.Conditions {
		if condition.Type != autoscalingv2.ScalingActive {
			conditions = append(conditions, condition)
		}
	}
	conditions = append(conditions, autoscalingv2.HorizontalPodAutoscalerCondition{
		Type:    autoscalingv2.ScalingActive,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
	hpa.Status.Conditions = conditions
}

// replaceSimulatedControllerConditions removes controller observations that
// describe the live recommendation and replaces ScalingLimited with a condition
// derived from the projected recommendation.
func replaceSimulatedControllerConditions(hpa *autoscalingv2.HorizontalPodAutoscaler, limited bool, reason string) {
	conditions := make([]autoscalingv2.HorizontalPodAutoscalerCondition, 0, len(hpa.Status.Conditions)+1)
	for _, condition := range hpa.Status.Conditions {
		if condition.Type == autoscalingv2.ScalingLimited || condition.Type == autoscalingv2.ScaledToZero {
			continue
		}
		if condition.Type == autoscalingv2.AbleToScale &&
			(condition.Reason == "ScaleUpStabilized" || condition.Reason == "ScaleDownStabilized") {
			continue
		}
		conditions = append(conditions, condition)
	}

	status := corev1.ConditionFalse
	message := "the projected desired replica count is within the simulated limits"
	if limited {
		status = corev1.ConditionTrue
		message = "the projected desired replica count is constrained by the simulated limits"
	}
	conditions = append(conditions, autoscalingv2.HorizontalPodAutoscalerCondition{
		Type:    autoscalingv2.ScalingLimited,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
	hpa.Status.Conditions = conditions
}
