package capacityplan

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
)

// PendingPodInfo is a local type for capacityplan to avoid import cycles.
// It matches the structure of hpa.PendingPodInfo but is a separate type.
type PendingPodInfo struct {
	Name          string
	Phase         string
	Unschedulable bool
	Reasons       []string
}

// QuotaConstraint is a local type for capacityplan to avoid import cycles.
// It matches the structure of hpa.QuotaConstraint but is a separate type.
type QuotaConstraint struct {
	Name     string
	Resource string
	Used     string
	Hard     string
	Message  string
}

// PDBInterference is a local type for capacityplan to avoid import cycles.
// It matches the structure of hpa.PDBInterference but is a separate type.
type PDBInterference struct {
	Name           string
	MinAvailable   string
	MaxUnavailable string
	Disruption     string
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
	// TargetMaxReplicas is the proposed new maxReplicas (default:
	// maxReplicas*2, with a 200-replica soft cap for HPAs currently below it).
	TargetMaxReplicas int32

	// ContainerResources holds per-container CPU and memory requests from the
	// scale target's pod template.
	ContainerResources []CapacityContainerResources
	// PodRequestCPU and PodRequestMemory are the effective scheduler requests
	// for one target Pod, including init containers and Pod overhead. When
	// empty, analysis falls back to summing ContainerResources.
	PodRequestCPU    string
	PodRequestMemory string
	// PodLimitCPU and PodLimitMemory are the effective whole-Pod limits used
	// by limits.* ResourceQuota checks.
	PodLimitCPU    string
	PodLimitMemory string
	// PodLevel* preserve spec.resources declarations. ResourceQuota accepts
	// these without requiring every container to repeat the same dimension.
	PodLevelRequestCPU    string
	PodLevelRequestMemory string
	PodLevelLimitCPU      string
	PodLevelLimitMemory   string
	// Quotas holds all ResourceQuota entries (not just near-limit) so the
	// analysis can compute remaining headroom.
	Quotas []CapacityQuotaInfo
	// LimitRanges holds LimitRange min/max constraints for containers and pods.
	LimitRanges []LimitRangeConstraint
	// NodeCapacity holds aggregate node allocatable resources.
	NodeCapacity *blocker.NodeCapacitySummary
	// PendingPods lists pods in Pending phase for the scale target.
	PendingPods []PendingPodInfo
	// PDBs lists PodDisruptionBudgets in the namespace.
	PDBs []PDBInterference
	// ClusterAutoscaler is true when Cluster Autoscaler is detected.
	ClusterAutoscaler bool
	// ReadyPods is the count of pods in Running/Ready state.
	ReadyPods int32
	// ObservationErrors records unavailable or invalid inputs. Errors in
	// decision-relevant scale-up domains make the recommendation unknown;
	// advisory-only domains remain visible without blocking an otherwise
	// supported recommendation.
	ObservationErrors []CapacityObservationError
}

// CapacityObservationError identifies an input that could not be observed.
type CapacityObservationError struct {
	// Domain identifies the analysis input affected by this error. Source is
	// retained as a human-readable label and for compatibility with callers
	// written before typed domains were introduced.
	Domain  CapacityObservationDomain `json:"domain,omitempty" yaml:"domain,omitempty"`
	Source  string                    `json:"source" yaml:"source"`
	Message string                    `json:"message" yaml:"message"`
}

// CapacityObservationDomain identifies a set of capacity inputs whose
// dependent checks must be skipped when observation or validation fails.
type CapacityObservationDomain string

const ( //nolint:revive // CapacityObservationDomain documentation describes this enum.
	// CapacityObservationPlanInput identifies validation of the plan request itself.
	CapacityObservationPlanInput          CapacityObservationDomain = "plan-input"
	CapacityObservationScaleTarget        CapacityObservationDomain = "scale-target" //nolint:revive // Domain enum value.
	CapacityObservationPodResources       CapacityObservationDomain = "pod-resources"
	CapacityObservationPendingPods        CapacityObservationDomain = "pending-pods"
	CapacityObservationResourceQuotas     CapacityObservationDomain = "resource-quotas"
	CapacityObservationLimitRanges        CapacityObservationDomain = "limit-ranges"
	CapacityObservationLimitRangeDefaults CapacityObservationDomain = "limit-range-defaults"
	CapacityObservationNodeCapacity       CapacityObservationDomain = "node-capacity"
	CapacityObservationPDBs               CapacityObservationDomain = "pod-disruption-budgets"
	CapacityObservationClusterAutoscaler  CapacityObservationDomain = "cluster-autoscaler"
)

// CapacityContainerResources holds per-container resource requests for
// capacity projection.
type CapacityContainerResources struct {
	// Name is the container name.
	Name string
	// CPU is the CPU request as a quantity string (e.g. "250m").
	CPU string
	// Memory is the memory request as a quantity string (e.g. "512Mi").
	Memory string
	// LimitCPU and LimitMemory preserve configured container limits for
	// ResourceQuota projection.
	LimitCPU    string
	LimitMemory string
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
	// UsageObserved is nil for compatibility callers, true when status.used
	// contained this resource, and false when quota status was not yet synced.
	UsageObserved *bool
	// HardObserved is nil for compatibility callers, true when status.hard
	// contained this resource, and false while the quota controller has not
	// synchronized spec.hard into status.
	HardObserved *bool
	// Scoped is true when scopes/scopeSelector make applicability Pod-specific.
	Scoped bool
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
	// Default and DefaultRequest are admission-time container defaults.
	Default        string
	DefaultRequest string
	// MaxLimitRequestRatio constrains the limit-to-request ratio.
	MaxLimitRequestRatio string
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

// CapacityCheckID identifies the constraint evaluated by a capacity check.
type CapacityCheckID string

const ( //nolint:revive // CapacityCheckID documentation describes this enum.
	// CapacityCheckObservation records whether required inputs were observable.
	CapacityCheckObservation       CapacityCheckID = "observation"
	CapacityCheckQuota             CapacityCheckID = "quota" //nolint:revive // Check identifier.
	CapacityCheckQuotaCPU          CapacityCheckID = "quota-cpu"
	CapacityCheckQuotaMemory       CapacityCheckID = "quota-memory"
	CapacityCheckQuotaPods         CapacityCheckID = "quota-pods"
	CapacityCheckQuotaLimitCPU     CapacityCheckID = "quota-limit-cpu"
	CapacityCheckQuotaLimitMemory  CapacityCheckID = "quota-limit-memory"
	CapacityCheckLimitRange        CapacityCheckID = "limit-range"
	CapacityCheckLimitRangeMaximum CapacityCheckID = "limit-range-maximum"
	CapacityCheckLimitRangeMinimum CapacityCheckID = "limit-range-minimum"
	CapacityCheckLimitRangeRatio   CapacityCheckID = "limit-range-ratio"
	CapacityCheckNodeCapacity      CapacityCheckID = "node-capacity"
	CapacityCheckNodeCPU           CapacityCheckID = "node-cpu"
	CapacityCheckNodeMemory        CapacityCheckID = "node-memory"
	CapacityCheckNodePods          CapacityCheckID = "node-pods"
	CapacityCheckNodeSchedulable   CapacityCheckID = "node-schedulable"
	CapacityCheckPendingPods       CapacityCheckID = "pending-pods"
	CapacityCheckPDB               CapacityCheckID = "pod-disruption-budget"
	CapacityCheckClusterAutoscaler CapacityCheckID = "cluster-autoscaler"
)

// CapacityCheckStatus is the machine-readable outcome of a capacity check.
type CapacityCheckStatus string

const ( //nolint:revive // CapacityCheckStatus documentation describes this enum.
	// CapacityCheckPass means the constraint is satisfied.
	CapacityCheckPass    CapacityCheckStatus = "pass"
	CapacityCheckFail    CapacityCheckStatus = "fail" //nolint:revive // Check status value.
	CapacityCheckUnknown CapacityCheckStatus = "unknown"
)

// CapacityCheckResult holds a single check result for the capacity plan.
type CapacityCheckResult struct {
	// CheckID and Status are typed decision metadata. Pass and Unknown remain
	// populated for compatibility with existing JSON/YAML consumers.
	CheckID CapacityCheckID     `json:"checkId,omitempty" yaml:"checkId,omitempty"`
	Status  CapacityCheckStatus `json:"status,omitempty" yaml:"status,omitempty"`
	// ObservationDomain identifies the unavailable input behind an unknown
	// observation check. Decision logic uses it to distinguish required
	// scale-up evidence from advisory-only observations.
	ObservationDomain CapacityObservationDomain `json:"observationDomain,omitempty" yaml:"observationDomain,omitempty"`
	// Pass is true when the check succeeds.
	Pass bool `json:"pass" yaml:"pass"`
	// Unknown is true when the check could not be completed because an input
	// observation failed. Decision-relevant unknown checks prevent a Safe
	// recommendation; advisory-only observations stay visible without
	// blocking an otherwise-supported scale-up.
	Unknown bool `json:"unknown,omitempty" yaml:"unknown,omitempty"`
	// Message describes the check outcome.
	Message string `json:"message" yaml:"message"`
}

// labels holds localized labels for capacity plan text output.
type labels struct {
	CapacityPlan      string
	Recommendation   string
	Checks            string
	Safe              string
	Failed            string
	AdditionalPods    string
	RequiredCPU      string
	RequiredMemory   string
	Headroom          string
	NextActions       string
}

// Labels is the exported version of labels for cross-package usage.
type Labels = labels

// maxReplicasCap is the soft upper bound for simulated maxReplicas.
const maxReplicasCap int32 = 200
