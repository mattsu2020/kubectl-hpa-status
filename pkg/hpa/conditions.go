package hpa

// HPA condition type constants. Kubernetes defines these as string literals
// in the autoscaling/v2 condition types; we mirror them here so callers
// compare against named constants instead of repeating the magic strings
// ("ScalingActive", "ScalingLimited", "AbleToScale") across the codebase.
//
// Values must stay in sync with autoscalingv2.HorizontalPodAutoscalerCondition.
const (
	// ConditionScalingActive is the HPA condition that reports whether the
	// metrics pipeline can compute a recommendation.
	ConditionScalingActive = "ScalingActive"
	// ConditionScalingLimited is the HPA condition that reports whether
	// scaling is capped by minReplicas or maxReplicas.
	ConditionScalingLimited = "ScalingLimited"
	// ConditionAbleToScale is the HPA condition that reports whether the
	// controller can act on scaling decisions.
	ConditionAbleToScale = "AbleToScale"
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
