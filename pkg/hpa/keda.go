package hpa

// TargetReplicaInfo holds replica status from the scale target resource.
// When not-ready pods exist, HPA scaling calculations may be affected.
// Kept in pkg/hpa (not keda) because it describes the scale target, not KEDA.
type TargetReplicaInfo struct {
	TotalReplicas int32 `json:"totalReplicas" yaml:"totalReplicas"`
	ReadyReplicas int32 `json:"readyReplicas" yaml:"readyReplicas"`
	NotReady      int32 `json:"notReady" yaml:"notReady"`
	Pending       int32 `json:"pending,omitempty" yaml:"pending,omitempty"`
	Unschedulable int32 `json:"unschedulable,omitempty" yaml:"unschedulable,omitempty"`
}
