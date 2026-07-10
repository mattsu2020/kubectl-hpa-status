package hpa

// ---------------------------------------------------------------------------
// Autoscaler Map types (autoscaler-map command)
// ---------------------------------------------------------------------------

// AutoscalerMap holds the complete HPA-to-Node Autoscaler relationship
// visualization for a single HPA.
type AutoscalerMap struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// HPAName is the HPA resource name.
	HPAName string `json:"hpaName" yaml:"hpaName"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// CurrentReplicas is the current replica count.
	CurrentReplicas int32 `json:"currentReplicas" yaml:"currentReplicas"`
	// DesiredReplicas is the desired replica count from HPA.
	DesiredReplicas int32 `json:"desiredReplicas" yaml:"desiredReplicas"`
	// MaxReplicas is the HPA maxReplicas.
	MaxReplicas int32 `json:"maxReplicas" yaml:"maxReplicas"`
	// Summary is a one-line overall assessment.
	Summary string `json:"summary" yaml:"summary"`
	// Layers describes the HPA -> Deployment -> Pods -> Nodes -> Autoscaler layers.
	Layers []AutoscalerMapLayer `json:"layers" yaml:"layers"`
	// Blockers lists detected blockers in the autoscaling chain.
	Blockers []AutoscalerMapBlocker `json:"blockers,omitempty" yaml:"blockers,omitempty"`
	// Recommendation is the overall recommendation text.
	Recommendation string `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	// NextActions lists concrete actions to take.
	NextActions []string `json:"nextActions,omitempty" yaml:"nextActions,omitempty"`
	// Risk is the overall risk level: "high", "medium", "low", or "none".
	Risk string `json:"risk" yaml:"risk"`
	// NextChecks lists kubectl verification commands for detected resources.
	NextChecks []string `json:"nextChecks,omitempty" yaml:"nextChecks,omitempty"`
	// Warnings records Kubernetes API reads that could not be completed. Missing
	// data must not be presented as a confirmed blocker.
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// AutoscalerMapLayer describes one layer in the autoscaling chain.
type AutoscalerMapLayer struct {
	// Name is the layer name: "hpa", "workload", "pods", "nodes", "autoscaler", "external-scaler", "constraints".
	Name string `json:"name" yaml:"name"`
	// Resource is the resource identifier at this layer.
	Resource string `json:"resource" yaml:"resource"`
	// Status is the status summary at this layer.
	Status string `json:"status" yaml:"status"`
	// Details provides additional information about this layer.
	Details []string `json:"details,omitempty" yaml:"details,omitempty"`
	// Healthy indicates whether this layer is functioning correctly.
	Healthy bool `json:"healthy" yaml:"healthy"`
}

// AutoscalerMapBlocker represents a detected blocker in the autoscaling chain.
type AutoscalerMapBlocker struct {
	// Layer is the layer where the blocker was detected.
	Layer string `json:"layer" yaml:"layer"`
	// Severity is the blocker severity: "high", "medium", "low".
	Severity string `json:"severity" yaml:"severity"`
	// Message describes the blocker.
	Message string `json:"message" yaml:"message"`
	// Detail provides additional context.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// AutoscalerMapInput aggregates all observable signals for autoscaler map
// analysis. The cmd layer assembles this from Kubernetes API calls.
type AutoscalerMapInput struct {
	// Namespace is the Kubernetes namespace.
	Namespace string
	// HPAName is the HPA resource name.
	HPAName string
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string
	// CurrentReplicas is the current replica count from HPA status.
	CurrentReplicas int32
	// DesiredReplicas is the desired replica count from HPA status.
	DesiredReplicas int32
	// MaxReplicas is the HPA maxReplicas.
	MaxReplicas int32
	// WorkloadReadyReplicas is the ready replica count from the workload.
	WorkloadReadyReplicas int32
	// WorkloadDesiredReplicas is the desired replica count from the workload.
	WorkloadDesiredReplicas int32
	// PodSummary holds pod-level summary information.
	PodSummary AutoscalerMapPodSummary
	// NodeSummary holds node-level summary information.
	NodeSummary AutoscalerMapNodeSummary
	// NodeFetchError distinguishes an unavailable node list from a confirmed
	// empty cluster.
	NodeFetchError string
	// ClusterAutoscaler indicates whether Cluster Autoscaler is detected.
	ClusterAutoscaler bool
	// Karpenter indicates whether Karpenter is detected.
	Karpenter bool
	// PendingPods lists pending pods for the scale target.
	PendingPods []PendingPodInfo
	// ScalingActive indicates whether the HPA ScalingActive condition is True.
	ScalingActive bool
	// KEDAInfo holds KEDA ScaledObject information if detected.
	KEDAInfo *AutoscalerMapKEDAInfo
	// VPAInfo holds VPA conflict information if detected.
	VPAInfo *AutoscalerMapVPAInfo
	// PDBs lists PodDisruptionBudgets in the namespace affecting the scale target.
	PDBs []AutoscalerMapPDB
	// Quotas lists ResourceQuotas near their limits in the namespace.
	Quotas []AutoscalerMapQuota
}

// AutoscalerMapKEDAInfo holds KEDA ScaledObject information for the autoscaler map.
type AutoscalerMapKEDAInfo struct {
	// ScaledObjectName is the name of the KEDA ScaledObject.
	ScaledObjectName string `json:"scaledObjectName" yaml:"scaledObjectName"`
	// TriggerCount is the number of triggers configured.
	TriggerCount int `json:"triggerCount" yaml:"triggerCount"`
	// Active indicates whether the ScaledObject is active.
	Active bool `json:"active" yaml:"active"`
}

// AutoscalerMapVPAInfo holds VPA conflict information for the autoscaler map.
type AutoscalerMapVPAInfo struct {
	// VPAName is the name of the conflicting VPA.
	VPAName string `json:"vpaName" yaml:"vpaName"`
	// TargetRef is the VPA target reference.
	TargetRef string `json:"targetRef" yaml:"targetRef"`
	// UpdateMode is the VPA update mode.
	UpdateMode string `json:"updateMode" yaml:"updateMode"`
	// ControlledResources lists the resource types controlled by VPA.
	ControlledResources []string `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	// ConflictResources lists the resource types that conflict with HPA.
	ConflictResources []string `json:"conflictResources,omitempty" yaml:"conflictResources,omitempty"`
}

// AutoscalerMapPDB represents a PodDisruptionBudget relevant to the autoscaler map.
type AutoscalerMapPDB struct {
	// Name is the PDB name.
	Name string `json:"name" yaml:"name"`
	// MinAvailable is the minAvailable setting if set.
	MinAvailable string `json:"minAvailable,omitempty" yaml:"minAvailable,omitempty"`
	// MaxUnavailable is the maxUnavailable setting if set.
	MaxUnavailable string `json:"maxUnavailable,omitempty" yaml:"maxUnavailable,omitempty"`
}

// AutoscalerMapQuota represents a ResourceQuota near its limit.
type AutoscalerMapQuota struct {
	// Name is the quota name.
	Name string `json:"name" yaml:"name"`
	// Resource is the resource type (e.g. "limits.cpu").
	Resource string `json:"resource" yaml:"resource"`
	// Used is the current usage.
	Used string `json:"used" yaml:"used"`
	// Hard is the hard limit.
	Hard string `json:"hard" yaml:"hard"`
	// Ratio is the usage ratio (0.0 to 1.0+).
	Ratio float64 `json:"ratio" yaml:"ratio"`
}

// AutoscalerMapPodSummary holds pod-level summary information.
type AutoscalerMapPodSummary struct {
	// Total is the total number of pods.
	Total int32 `json:"total" yaml:"total"`
	// Running is the count of running pods.
	Running int32 `json:"running" yaml:"running"`
	// Pending is the count of pending pods.
	Pending int32 `json:"pending" yaml:"pending"`
	// Ready is the count of ready pods.
	Ready int32 `json:"ready" yaml:"ready"`
}

// AutoscalerMapNodeSummary holds node-level summary information.
type AutoscalerMapNodeSummary struct {
	// TotalNodes is the total number of nodes.
	TotalNodes int32 `json:"totalNodes" yaml:"totalNodes"`
	// AllocatableCPU is the total allocatable CPU across all nodes.
	AllocatableCPU string `json:"allocatableCpu,omitempty" yaml:"allocatableCpu,omitempty"`
	// AllocatableMemory is the total allocatable memory across all nodes.
	AllocatableMemory string `json:"allocatableMemory,omitempty" yaml:"allocatableMemory,omitempty"`
	// TaintedNodes is the count of tainted nodes.
	TaintedNodes int32 `json:"taintedNodes,omitempty" yaml:"taintedNodes,omitempty"`
	// MatchingNodePools lists node pools that match the workload's scheduling constraints.
	MatchingNodePools []string `json:"matchingNodePools,omitempty" yaml:"matchingNodePools,omitempty"`
	// PodCPURequest is the CPU request per pod.
	PodCPURequest string `json:"podCpuRequest,omitempty" yaml:"podCpuRequest,omitempty"`
	// PodMemoryRequest is the memory request per pod.
	PodMemoryRequest string `json:"podMemoryRequest,omitempty" yaml:"podMemoryRequest,omitempty"`
}
