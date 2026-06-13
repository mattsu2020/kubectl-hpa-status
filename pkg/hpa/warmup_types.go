package hpa

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// WarmupAnalysis holds the complete warmup analysis result for an HPA that
// recently scaled out but pods are not yet ready.
type WarmupAnalysis struct {
	// Summary is the overall warmup state: "capacity_warming_up",
	// "capacity_ready", "insufficient_data".
	Summary string `json:"summary" yaml:"summary"`
	// EffectiveCapacityRatio is the ratio of ready pods to desired replicas (0.0-1.0).
	EffectiveCapacityRatio float64 `json:"effectiveCapacityRatio" yaml:"effectiveCapacityRatio"`
	// DesiredReplicas is the HPA desired replica count.
	DesiredReplicas int32 `json:"desiredReplicas" yaml:"desiredReplicas"`
	// CurrentReplicas is the HPA current replica count.
	CurrentReplicas int32 `json:"currentReplicas" yaml:"currentReplicas"`
	// ReadyPods is the count of pods in Ready state.
	ReadyPods int32 `json:"readyPods" yaml:"readyPods"`
	// AvailablePods is the count from the workload's availableReplicas status.
	AvailablePods int32 `json:"availablePods" yaml:"availablePods"`
	// AvgTimeToReadySeconds is the average time from pod creation to Ready condition.
	// Zero if no pods have become Ready yet.
	AvgTimeToReadySeconds int64 `json:"avgTimeToReadySeconds" yaml:"avgTimeToReadySeconds"`
	// P95TimeToReadySeconds is the p95 time from pod creation to Ready condition.
	P95TimeToReadySeconds int64 `json:"p95TimeToReadySeconds" yaml:"p95TimeToReadySeconds"`
	// MaxTimeToReadySeconds is the maximum observed time-to-ready.
	MaxTimeToReadySeconds int64 `json:"maxTimeToReadySeconds,omitempty" yaml:"maxTimeToReadySeconds,omitempty"`
	// Bottlenecks lists the detected warmup bottlenecks.
	Bottlenecks []WarmupBottleneck `json:"bottlenecks" yaml:"bottlenecks"`
	// Evidence lists human-readable evidence lines.
	Evidence []string `json:"evidence" yaml:"evidence"`
	// Impact is a human-readable description of the current effective capacity.
	Impact string `json:"impact" yaml:"impact"`
	// RecommendedActions lists actionable suggestions.
	RecommendedActions []string `json:"recommendedActions" yaml:"recommendedActions"`
	// PodDetails holds per-pod warmup status for JSON/YAML consumers.
	PodDetails []WarmupPodDetail `json:"podDetails,omitempty" yaml:"podDetails,omitempty"`
}

// WarmupBottleneck represents a single detected warmup bottleneck.
type WarmupBottleneck struct {
	// Type classifies the bottleneck: "readiness_probe", "image_pull",
	// "scheduling", "startup_probe", "container_crash", "metrics_inactive", "unknown".
	Type string `json:"type" yaml:"type"`
	// Severity is the bottleneck severity.
	Severity Severity `json:"severity" yaml:"severity"`
	// Confidence is the analysis confidence.
	Confidence Confidence `json:"confidence" yaml:"confidence"`
	// Count is how many pods are affected by this bottleneck.
	Count int32 `json:"count" yaml:"count"`
	// Message is a human-readable description.
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// WarmupPodDetail holds per-pod warmup status for structured output.
type WarmupPodDetail struct {
	// Name is the pod name.
	Name string `json:"name" yaml:"name"`
	// AgeSeconds is the pod age in seconds.
	AgeSeconds int64 `json:"ageSeconds" yaml:"ageSeconds"`
	// Ready indicates whether the pod is Ready.
	Ready bool `json:"ready" yaml:"ready"`
	// ContainerState is the primary container state: "running", "waiting", "terminated".
	ContainerState string `json:"containerState,omitempty" yaml:"containerState,omitempty"`
	// WaitingReason is the container waiting reason (e.g., "ImagePullBackOff").
	WaitingReason string `json:"waitingReason,omitempty" yaml:"waitingReason,omitempty"`
	// RestartCount is the number of container restarts.
	RestartCount int32 `json:"restartCount" yaml:"restartCount"`
	// TimeToReadySeconds is the observed time-to-Ready, or 0 if not ready yet.
	TimeToReadySeconds int64 `json:"timeToReadySeconds,omitempty" yaml:"timeToReadySeconds,omitempty"`
}

// WarmupInput aggregates all observable signals for warmup analysis.
// The cmd layer assembles this from multiple kube fetchers, keeping the core
// analysis in pkg/hpa free of Kubernetes API dependencies.
type WarmupInput struct {
	// Namespace is the Kubernetes namespace.
	Namespace string
	// DesiredReplicas is the HPA desired replica count.
	DesiredReplicas int32
	// CurrentReplicas is the HPA current replica count.
	CurrentReplicas int32
	// MinReplicas is the HPA minimum replica count.
	MinReplicas int32
	// MaxReplicas is the HPA maximum replica count.
	MaxReplicas int32
	// ScalingActive indicates whether the HPA ScalingActive condition is True.
	ScalingActive bool
	// ScalingLimited indicates whether the HPA is capped by min/max.
	ScalingLimited bool
	// TargetReadyReplicas is the ready replica count from the scale target.
	TargetReadyReplicas int32
	// TargetAvailableReplicas is the available replica count from the scale target.
	TargetAvailableReplicas int32
	// TargetDesiredReplicas is the desired replica count from the scale target.
	TargetDesiredReplicas int32
	// TotalPods is the total number of pods for the scale target.
	TotalPods int32
	// ReadyPods is the count of pods in Running/Ready state.
	ReadyPods int32
	// PodDetails holds per-pod warmup status information.
	PodDetails []WarmupPodDetail
	// UnhealthyEvents lists pod-level events with reasons indicating warmup issues.
	UnhealthyEvents []WarmupEventInfo
	// ReadinessProbePresent indicates if the pod template has a readinessProbe.
	ReadinessProbePresent bool
	// StartupProbePresent indicates if the pod template has a startupProbe.
	StartupProbePresent bool
	// ReadinessProbeMaxDelaySeconds is the maximum readiness probe delay.
	ReadinessProbeMaxDelaySeconds int32
	// StartupProbeMaxDelaySeconds is the maximum startup probe delay.
	StartupProbeMaxDelaySeconds int32
	// Now is the current time, used for age calculations.
	Now metav1.Time
}

// WarmupEventInfo holds a pod-level event relevant to warmup analysis.
type WarmupEventInfo struct {
	// Reason is the event reason (e.g., "Unhealthy", "FailedScheduling",
	// "BackOff", "ImagePullBackOff").
	Reason string `json:"reason" yaml:"reason"`
	// Count is the number of times this event occurred.
	Count int32 `json:"count" yaml:"count"`
}
