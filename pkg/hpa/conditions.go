package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/conditions"
)

// This file re-exports the condition constants and lookup helpers from
// pkg/hpa/internal/conditions so existing call sites in pkg/hpa keep working
// without changes. Sub-packages that need these helpers import
// pkg/hpa/internal/conditions directly.

// HPA condition type constants (re-exported from conditions package).
const (
	// ConditionScalingActive is the HPA condition that reports whether the
	// metrics pipeline can compute a recommendation.
	ConditionScalingActive = conditions.ScalingActive
	// ConditionScalingLimited is the HPA condition that reports whether
	// scaling is capped by minReplicas or maxReplicas.
	ConditionScalingLimited = conditions.ScalingLimited
	// ConditionAbleToScale is the HPA condition that reports whether the
	// controller can act on scaling decisions.
	ConditionAbleToScale = conditions.AbleToScale
)

// Metric type display-name constants. These mirror the string forms of the
// autoscalingv2 MetricSourceType values (Resource, ContainerResource, Pods,
// Object, External) used in the formatted Metric.Type field and in rendering
// across the codebase. Using these avoids repeating the literal "Resource" /
// "External" / etc. in dozens of sites.
const (
	MetricTypeResource          = "Resource"
	MetricTypeContainerResource = "ContainerResource"
	MetricTypePods              = "Pods"
	MetricTypeObject            = "Object"
	MetricTypeExternal          = "External"
)

// FindCondition returns the HPA condition matching the given type, or nil.
// Returns nil safely when hpa is nil. Delegates to conditions.Find.
func FindCondition(hpa *autoscalingv2.HorizontalPodAutoscaler, conditionType string) *autoscalingv2.HorizontalPodAutoscalerCondition {
	return conditions.Find(hpa, conditionType)
}

// scaleDownStabilizationWindow returns the HPA scale-down stabilization window
// in seconds, or nil when no behavior is configured. Delegates to
// conditions.ScaleDownStabilizationWindow.
func scaleDownStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) *int32 {
	return conditions.ScaleDownStabilizationWindow(hpa)
}

// estimateStabilizationRemaining estimates how many seconds remain before the
// scale-down stabilization window expires. Delegates to
// conditions.EstimateStabilizationRemaining.
func estimateStabilizationRemaining(hpa *autoscalingv2.HorizontalPodAutoscaler) *int64 {
	return conditions.EstimateStabilizationRemaining(hpa)
}
