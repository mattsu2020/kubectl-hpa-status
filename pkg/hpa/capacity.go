package hpa

import (
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/capacityplan"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"k8s.io/apimachinery/pkg/api/resource"
)

// CapacityPlan re-exports the capacity plan types from the capacityplan subpackage.
// This maintains backward compatibility for existing imports.
type CapacityPlan = capacityplan.CapacityPlan

// CapacityPlanInput re-exports the capacity plan input type.
type CapacityPlanInput = capacityplan.Input

// CapacityContext holds infrastructure capacity analysis for the HPA scale target.
type CapacityContext struct {
	PendingPods      []PendingPodInfo  `json:"pendingPods,omitempty" yaml:"pendingPods,omitempty"`
	QuotaConstraints []QuotaConstraint `json:"quotaConstraints,omitempty" yaml:"quotaConstraints,omitempty"`
	PDBInterference  []PDBInterference `json:"pdbInterference,omitempty" yaml:"pdbInterference,omitempty"`
	NodeHints        []string          `json:"nodeHints,omitempty" yaml:"nodeHints,omitempty"`
	Warnings         []string          `json:"warnings,omitempty" yaml:"warnings,omitempty"`
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

// CapacityObservationError re-exports the capacity observation error type.
type CapacityObservationError = capacityplan.CapacityObservationError

// CapacityObservationDomain re-exports the capacity observation domain type.
type CapacityObservationDomain = capacityplan.CapacityObservationDomain

// Capacity observation domain constants re-exported from capacityplan.
const (
	CapacityObservationPlanInput          = capacityplan.CapacityObservationPlanInput
	CapacityObservationScaleTarget        = capacityplan.CapacityObservationScaleTarget
	CapacityObservationPodResources       = capacityplan.CapacityObservationPodResources
	CapacityObservationPendingPods        = capacityplan.CapacityObservationPendingPods
	CapacityObservationResourceQuotas     = capacityplan.CapacityObservationResourceQuotas
	CapacityObservationLimitRanges        = capacityplan.CapacityObservationLimitRanges
	CapacityObservationLimitRangeDefaults = capacityplan.CapacityObservationLimitRangeDefaults
	CapacityObservationNodeCapacity       = capacityplan.CapacityObservationNodeCapacity
	CapacityObservationPDBs               = capacityplan.CapacityObservationPDBs
	CapacityObservationClusterAutoscaler  = capacityplan.CapacityObservationClusterAutoscaler
)

// CapacityContainerResources re-exports the capacity container resources type.
type CapacityContainerResources = capacityplan.CapacityContainerResources

// CapacityQuotaInfo re-exports the capacity quota info type.
type CapacityQuotaInfo = capacityplan.CapacityQuotaInfo

// LimitRangeConstraint re-exports the limit range constraint type.
type LimitRangeConstraint = capacityplan.LimitRangeConstraint

// CapacityCheckID re-exports the capacity check ID type.
type CapacityCheckID = capacityplan.CapacityCheckID

// CapacityCheckStatus re-exports the capacity check status type.
type CapacityCheckStatus = capacityplan.CapacityCheckStatus

// CapacityCheckResult re-exports the capacity check result type.
type CapacityCheckResult = capacityplan.CapacityCheckResult

// AnalyzeCapacityPlan re-exports the capacity plan analysis function.
func AnalyzeCapacityPlan(input CapacityPlanInput) *CapacityPlan {
	return capacityplan.AnalyzeCapacityPlan(input)
}

// WriteCapacityPlanText re-exports the capacity plan text writer.
func WriteCapacityPlanText(w io.Writer, plan *CapacityPlan, theme style.Theme) error {
	return capacityplan.WriteCapacityPlanText(w, plan, theme)
}

// parseQuantityOrZero parses a resource quantity string, returning zero on error.
// This is a helper function shared across the package.
func parseQuantityOrZero(s string) resource.Quantity {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return resource.Quantity{}
	}
	return q
}

// AppendCapacityPlanText appends the capacity plan section to out.
// This is a re-export that converts hpa labels to capacityplan labels.
func AppendCapacityPlanText(out *[]byte, plan *CapacityPlan, theme style.Theme, lbls labels) {
	capacityplan.AppendCapacityPlanText(out, plan, theme, capacityplanLabels(lbls.CapacityPlan))
}

// capacityplanLabels converts hpa CapacityPlan label to capacityplan labels.
func capacityplanLabels(capacityPlanLabel string) capacityplan.Labels {
	return capacityplan.Labels{
		CapacityPlan: capacityPlanLabel,
	}
}

// ConvertPendingPodsToCapacityplan converts hpa PendingPodInfo to capacityplan PendingPodInfo.
// This is exported for use in the cmd package when assembling CapacityPlanInput.
func ConvertPendingPodsToCapacityplan(hpaPods []PendingPodInfo) []capacityplan.PendingPodInfo {
	result := make([]capacityplan.PendingPodInfo, len(hpaPods))
	for i, pod := range hpaPods {
		result[i] = capacityplan.PendingPodInfo{
			Name:          pod.Name,
			Phase:         pod.Phase,
			Unschedulable: pod.Unschedulable,
			Reasons:       append([]string{}, pod.Reasons...),
		}
	}
	return result
}

// ConvertPDBsToCapacityplan converts hpa PDBInterference to capacityplan PDBInterference.
// This is exported for use in the cmd package when assembling CapacityPlanInput.
func ConvertPDBsToCapacityplan(hpaPDBs []PDBInterference) []capacityplan.PDBInterference {
	result := make([]capacityplan.PDBInterference, len(hpaPDBs))
	for i, pdb := range hpaPDBs {
		result[i] = capacityplan.PDBInterference{
			Name:           pdb.Name,
			MinAvailable:   pdb.MinAvailable,
			MaxUnavailable: pdb.MaxUnavailable,
			Disruption:     pdb.Disruption,
		}
	}
	return result
}

// CapacityObservationDomainForError returns the normalized domain for a capacity observation error.
// This re-exports the capacityplan internal function for testing.
func CapacityObservationDomainForError(observationErr CapacityObservationError) CapacityObservationDomain {
	return capacityplan.CapacityObservationDomainForError(observationErr)
}
