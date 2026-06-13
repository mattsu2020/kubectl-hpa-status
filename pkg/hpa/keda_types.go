package hpa

// KEDAAnalysis holds KEDA-specific information attached to an HPA Analysis.
// Populated only when --keda is enabled and the HPA is KEDA-managed.
type KEDAAnalysis struct {
	ScaledObjectName string               `json:"scaledObjectName" yaml:"scaledObjectName"`
	Triggers         []KEDATriggerSummary `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	PollingInterval  *int32               `json:"pollingInterval,omitempty" yaml:"pollingInterval,omitempty"`
	CooldownPeriod   *int32               `json:"cooldownPeriod,omitempty" yaml:"cooldownPeriod,omitempty"`
	MinReplicaCount  *int32               `json:"minReplicaCount,omitempty" yaml:"minReplicaCount,omitempty"`
	MaxReplicaCount  *int32               `json:"maxReplicaCount,omitempty" yaml:"maxReplicaCount,omitempty"`
	Lines            []string             `json:"lines,omitempty" yaml:"lines,omitempty"`
	Fallback         *KEDAFallbackInfo    `json:"fallback,omitempty" yaml:"fallback,omitempty"`
}

// KEDATriggerSummary is a display-oriented summary of a KEDA trigger.
type KEDATriggerSummary struct {
	Type         string `json:"type" yaml:"type"`
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
	Status       string `json:"status,omitempty" yaml:"status,omitempty"`
	Message      string `json:"message,omitempty" yaml:"message,omitempty"`
	MetricName   string `json:"metricName,omitempty" yaml:"metricName,omitempty"`
	Threshold    string `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	CurrentValue string `json:"currentValue,omitempty" yaml:"currentValue,omitempty"`
	AuthRef      string `json:"authRef,omitempty" yaml:"authRef,omitempty"`
}

// KEDAFallbackInfo holds fallback information for display.
type KEDAFallbackInfo struct {
	FailureThreshold int32 `json:"failureThreshold" yaml:"failureThreshold"`
	Replicas         int32 `json:"replicas" yaml:"replicas"`
}

// TargetReplicaInfo holds replica status from the scale target resource.
// When not-ready pods exist, HPA scaling calculations may be affected.
type TargetReplicaInfo struct {
	TotalReplicas int32 `json:"totalReplicas" yaml:"totalReplicas"`
	ReadyReplicas int32 `json:"readyReplicas" yaml:"readyReplicas"`
	NotReady      int32 `json:"notReady" yaml:"notReady"`
	Pending       int32 `json:"pending,omitempty" yaml:"pending,omitempty"`
	Unschedulable int32 `json:"unschedulable,omitempty" yaml:"unschedulable,omitempty"`
}
