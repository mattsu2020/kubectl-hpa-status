package hpa

// BlockerSeverity classifies how significantly a finding blocks scale-out.
type BlockerSeverity string

const (
	// BlockerHigh indicates a definite scale-out blocker requiring immediate attention.
	BlockerHigh BlockerSeverity = "HIGH"
	// BlockerMedium indicates a likely blocker that warrants investigation.
	BlockerMedium BlockerSeverity = "MEDIUM"
	// BlockerInfo indicates an informational finding with no blocking effect.
	BlockerInfo BlockerSeverity = "INFO"
)

// BlockerFinding represents a single detected scale-out blocker.
type BlockerFinding struct {
	// ID is a unique identifier for the detection rule that produced this finding.
	ID string `json:"id" yaml:"id"`
	// Severity is the blocker severity: HIGH, MEDIUM, or INFO.
	Severity BlockerSeverity `json:"severity" yaml:"severity"`
	// Category groups related findings: "scheduling", "quota", "application", "readiness", "info".
	Category string `json:"category" yaml:"category"`
	// Message is a human-readable description of the blocker.
	Message string `json:"message" yaml:"message"`
	// Detail provides additional context about the blocker.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
	// NextCommand suggests a kubectl command to investigate further.
	NextCommand string `json:"nextCommand,omitempty" yaml:"nextCommand,omitempty"`
}

// BlockerReport holds the complete scale-out blocker analysis for an HPA.
type BlockerReport struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA resource name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// HPAWantsScale is true when desiredReplicas > currentReplicas.
	HPAWantsScale bool `json:"hpaWantsScale" yaml:"hpaWantsScale"`
	// DesiredReplicas is the desired replica count from HPA status.
	DesiredReplicas int32 `json:"desiredReplicas" yaml:"desiredReplicas"`
	// ReadyReplicas is the count of ready pods on the scale target.
	ReadyReplicas int32 `json:"readyReplicas" yaml:"readyReplicas"`
	// Summary is a one-line summary of the blocker analysis.
	Summary string `json:"summary" yaml:"summary"`
	// Blockers lists all detected blocker findings sorted by severity.
	Blockers []BlockerFinding `json:"blockers" yaml:"blockers"`
	// Interpretation is a human-readable explanation of the overall situation.
	Interpretation string `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	// NextCommands lists suggested kubectl commands for further investigation.
	NextCommands []string `json:"nextCommands" yaml:"nextCommands"`
}

// ContainerStatusSummary holds container-level status for blocker detection.
type ContainerStatusSummary struct {
	// Pod is the pod name.
	Pod string `json:"pod" yaml:"pod"`
	// Container is the container name.
	Container string `json:"container" yaml:"container"`
	// Waiting is true when the container is in a waiting state.
	Waiting bool `json:"waiting" yaml:"waiting"`
	// WaitingReason is the reason for the waiting state (e.g. ImagePullBackOff, CrashLoopBackOff).
	WaitingReason string `json:"waitingReason,omitempty" yaml:"waitingReason,omitempty"`
	// RestartCount is the number of container restarts.
	RestartCount int32 `json:"restartCount" yaml:"restartCount"`
}

// NodeCapacitySummary holds node-level capacity information for deep analysis.
type NodeCapacitySummary struct {
	// TotalNodes is the total number of nodes in the cluster.
	TotalNodes int32 `json:"totalNodes" yaml:"totalNodes"`
	// AllocCPU is the sum of allocatable CPU across all nodes.
	AllocCPU string `json:"allocatableCpu,omitempty" yaml:"allocatableCpu,omitempty"`
	// AllocMemory is the sum of allocatable memory across all nodes.
	AllocMemory string `json:"allocatableMemory,omitempty" yaml:"allocatableMemory,omitempty"`
	// TaintedNodes is the count of nodes with at least one taint that has NoSchedule or NoExecute effect.
	TaintedNodes int32 `json:"taintedNodes,omitempty" yaml:"taintedNodes,omitempty"`
	// Hints provides actionable hints based on node capacity analysis.
	Hints []string `json:"hints,omitempty" yaml:"hints,omitempty"`
}

// BlockerInput aggregates all observable signals for scale-out blocker analysis.
// The cmd layer assembles this from multiple kube fetchers, keeping the core
// analysis in pkg/hpa free of Kubernetes API dependencies.
type BlockerInput struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string
	// DesiredReplicas is the HPA desired replica count.
	DesiredReplicas int32
	// CurrentReplicas is the HPA current replica count.
	CurrentReplicas int32
	// MinReplicas is the HPA minimum replica count.
	MinReplicas int32
	// MaxReplicas is the HPA maximum replica count.
	MaxReplicas int32
	// TargetReadyReplicas is the ready replica count from the scale target.
	TargetReadyReplicas int32
	// TargetDesiredReplicas is the desired replica count from the scale target.
	TargetDesiredReplicas int32
	// PendingPods lists pods in Pending phase with scheduling details.
	PendingPods []BlockerPodInfo
	// ReadyPods is the count of pods in Running/Ready state.
	ReadyPods int32
	// TotalPods is the total number of pods for the scale target.
	TotalPods int32
	// ContainerStatuses holds container-level status for failure detection.
	ContainerStatuses []ContainerStatusSummary
	// FailedSchedulingEvents lists events with reason FailedScheduling.
	FailedSchedulingEvents []string
	// Quotas lists ResourceQuota constraints near their limits.
	Quotas []BlockerQuotaInfo
	// NodeCapacity holds node-level capacity (only populated with --capacity-deep).
	NodeCapacity *NodeCapacitySummary
	// ScalingActive indicates whether the HPA ScalingActive condition is True.
	ScalingActive bool
}

// BlockerPodInfo holds pod-level information relevant to blocker detection.
type BlockerPodInfo struct {
	// Name is the pod name.
	Name string
	// Phase is the pod phase (Pending, Running, etc.).
	Phase string
	// Unschedulable is true when the pod has an unschedulable condition.
	Unschedulable bool
	// Reasons lists scheduling failure reasons from pod conditions.
	Reasons []string
}

// BlockerQuotaInfo holds ResourceQuota usage information for blocker detection.
type BlockerQuotaInfo struct {
	// Name is the ResourceQuota name.
	Name string
	// Resource is the resource name (e.g. requests.cpu, requests.memory).
	Resource string
	// Used is the current usage value as a string.
	Used string
	// Hard is the hard limit as a string.
	Hard string
	// Ratio is the usage ratio (used/hard), 0 if hard is zero.
	Ratio float64
}
