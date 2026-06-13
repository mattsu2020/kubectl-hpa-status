package hpa

// CapacityContext holds infrastructure capacity analysis for the HPA scale target.
type CapacityContext struct {
	PendingPods      []PendingPodInfo  `json:"pendingPods,omitempty" yaml:"pendingPods,omitempty"`
	QuotaConstraints []QuotaConstraint `json:"quotaConstraints,omitempty" yaml:"quotaConstraints,omitempty"`
	PDBInterference  []PDBInterference `json:"pdbInterference,omitempty" yaml:"pdbInterference,omitempty"`
	NodeHints        []string          `json:"nodeHints,omitempty" yaml:"nodeHints,omitempty"`
}

// CapacityHeadroom estimates the extra pod resources required to reach
// maxReplicas and summarizes visible cluster headroom signals.
type CapacityHeadroom struct {
	HPAName                    string   `json:"hpaName,omitempty" yaml:"hpaName,omitempty"`
	Target                     string   `json:"target,omitempty" yaml:"target,omitempty"`
	MaxReplicas                int32    `json:"maxReplicas" yaml:"maxReplicas"`
	CurrentDesired             int32    `json:"currentDesired" yaml:"currentDesired"`
	AdditionalReplicasToMax    int32    `json:"additionalReplicasToMax" yaml:"additionalReplicasToMax"`
	PodRequestCPU              string   `json:"podRequestCpu,omitempty" yaml:"podRequestCpu,omitempty"`
	PodRequestMemory           string   `json:"podRequestMemory,omitempty" yaml:"podRequestMemory,omitempty"`
	AdditionalCPUToMax         string   `json:"additionalCpuToMax,omitempty" yaml:"additionalCpuToMax,omitempty"`
	AdditionalMemoryToMax      string   `json:"additionalMemoryToMax,omitempty" yaml:"additionalMemoryToMax,omitempty"`
	ClusterSchedulableHeadroom string   `json:"clusterSchedulableHeadroom" yaml:"clusterSchedulableHeadroom"`
	Risk                       string   `json:"risk" yaml:"risk"`
	Evidence                   []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
}

// ---------------------------------------------------------------------------
// Capacity Plan types
// ---------------------------------------------------------------------------

// CapacityPlanInput aggregates all observable signals needed to produce a
// capacity plan. The cmd layer assembles this from multiple kube fetchers.
type CapacityPlanInput struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string
	// HPAName is the HPA resource name.
	HPAName string
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string
	// CurrentReplicas is the current replica count from HPA status.
	CurrentReplicas int32
	// MaxReplicas is the current maxReplicas from HPA spec.
	MaxReplicas int32
	// TargetMaxReplicas is the proposed new maxReplicas (default: maxReplicas*2, capped at 200).
	TargetMaxReplicas int32

	// ContainerResources holds per-container CPU and memory requests from the
	// scale target's pod template.
	ContainerResources []CapacityContainerResources
	// Quotas holds all ResourceQuota entries (not just near-limit) so the
	// analysis can compute remaining headroom.
	Quotas []CapacityQuotaInfo
	// LimitRanges holds LimitRange min/max constraints for containers and pods.
	LimitRanges []LimitRangeConstraint
	// NodeCapacity holds aggregate node allocatable resources.
	NodeCapacity *NodeCapacitySummary
	// PendingPods lists pods in Pending phase for the scale target.
	PendingPods []PendingPodInfo
	// PDBs lists PodDisruptionBudgets in the namespace.
	PDBs []PDBInterference
	// ClusterAutoscaler is true when Cluster Autoscaler is detected.
	ClusterAutoscaler bool
	// ReadyPods is the count of pods in Running/Ready state.
	ReadyPods int32
}

// CapacityContainerResources holds per-container resource requests for
// capacity projection.
type CapacityContainerResources struct {
	// Name is the container name.
	Name string
	// CPU is the CPU request as a quantity string (e.g. "250m").
	CPU string
	// Memory is the memory request as a quantity string (e.g. "512Mi").
	Memory string
}

// CapacityQuotaInfo holds full ResourceQuota usage so the capacity plan can
// compute remaining headroom.
type CapacityQuotaInfo struct {
	// Name is the ResourceQuota name.
	Name string
	// Resource is the resource type (e.g. "requests.cpu", "requests.memory").
	Resource string
	// Used is the current usage value as a string.
	Used string
	// Hard is the hard limit as a string.
	Hard string
}

// LimitRangeConstraint describes a LimitRange min/max that applies to pods or
// containers.
type LimitRangeConstraint struct {
	// Name is the LimitRange name.
	Name string
	// Type is the constraint target: "Container" or "Pod".
	Type string
	// Resource is the resource type (e.g. "cpu", "memory").
	Resource string
	// Min is the minimum allowed value (empty if no minimum).
	Min string
	// Max is the maximum allowed value (empty if no maximum).
	Max string
}

// CapacityPlan holds the result of a capacity plan analysis, diagnosing
// whether it is safe to raise HPA maxReplicas.
type CapacityPlan struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA resource name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`

	// Current state.
	CurrentReplicas int32  `json:"currentReplicas" yaml:"currentReplicas"`
	MaxReplicas     int32  `json:"maxReplicas" yaml:"maxReplicas"`
	Issue           string `json:"issue" yaml:"issue"`

	// Projected state if maxReplicas is raised.
	TargetMaxReplicas int32  `json:"targetMaxReplicas" yaml:"targetMaxReplicas"`
	AdditionalPods    int32  `json:"additionalPods" yaml:"additionalPods"`
	RequiredCPU       string `json:"requiredCpu" yaml:"requiredCpu"`
	RequiredMemory    string `json:"requiredMemory" yaml:"requiredMemory"`

	// Checks lists individual check results.
	Checks []CapacityCheckResult `json:"checks" yaml:"checks"`

	// Recommendation is the overall recommendation text.
	Recommendation string `json:"recommendation" yaml:"recommendation"`
	// Safe is true when all checks pass.
	Safe bool `json:"safe" yaml:"safe"`
	// SchedulableNow estimates how many additional pods can be scheduled
	// with current cluster resources. Zero means no headroom.
	SchedulableNow int32 `json:"schedulableNow,omitempty" yaml:"schedulableNow,omitempty"`
	// NodeAutoscalerRequired is true when node autoscaling is needed to
	// accommodate the projected maxReplicas.
	NodeAutoscalerRequired bool `json:"nodeAutoscalerRequired" yaml:"nodeAutoscalerRequired"`
	// DryRunCommand suggests a kubectl command for dry-run testing.
	DryRunCommand string `json:"dryRunCommand,omitempty" yaml:"dryRunCommand,omitempty"`
	// NextActions lists concrete remediation steps when Safe is false.
	NextActions []string `json:"nextActions,omitempty" yaml:"nextActions,omitempty"`
}

// CapacityCheckResult holds a single check result for the capacity plan.
type CapacityCheckResult struct {
	// Pass is true when the check succeeds.
	Pass bool `json:"pass" yaml:"pass"`
	// Message describes the check outcome.
	Message string `json:"message" yaml:"message"`
}
