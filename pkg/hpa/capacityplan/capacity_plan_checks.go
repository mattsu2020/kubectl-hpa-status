package capacityplan

import (
	"fmt"
	"strings"
)

type capacityCheckContext struct {
	input          CapacityPlanInput
	demand         capacityDemand
	additionalPods int32
}

func appendCapacityAnalysisChecks(plan *CapacityPlan, ctx capacityCheckContext) {
	input, demand, additionalPods := ctx.input, ctx.demand, ctx.additionalPods
	plan.Checks = append(plan.Checks, checkObservationErrors(input.ObservationErrors)...)
	resourceInputsUnknown := hasObservationDomain(input.ObservationErrors, CapacityObservationScaleTarget) ||
		hasObservationDomain(input.ObservationErrors, CapacityObservationPodResources)
	if !resourceInputsUnknown && !hasObservationDomain(input.ObservationErrors, CapacityObservationResourceQuotas) {
		plan.Checks = append(plan.Checks, checkQuotaHeadroom(
			input.Quotas,
			input,
			demand.totalCPU,
			demand.totalMemory,
			demand.totalLimitCPU,
			demand.totalLimitMemory,
			additionalPods,
		)...)
	}
	if !resourceInputsUnknown && !hasObservationDomain(input.ObservationErrors, CapacityObservationLimitRanges) {
		plan.Checks = append(plan.Checks, checkLimitRanges(
			input.LimitRanges,
			input.ContainerResources,
			demand.perPodCPU,
			demand.perPodMemory,
			demand.perPodLimitCPU,
			demand.perPodLimitMemory,
		)...)
	}
	if !resourceInputsUnknown && !hasObservationDomain(input.ObservationErrors, CapacityObservationNodeCapacity) {
		plan.Checks = append(plan.Checks, checkNodeCapacity(
			input.NodeCapacity,
			demand.totalCPU,
			demand.totalMemory,
			demand.perPodCPU,
			demand.perPodMemory,
			additionalPods,
			input.ClusterAutoscaler,
		)...)
	}
	if !hasObservationDomain(input.ObservationErrors, CapacityObservationScaleTarget) &&
		!hasObservationDomain(input.ObservationErrors, CapacityObservationPendingPods) {
		plan.Checks = append(plan.Checks, checkPendingPods(input.PendingPods)...)
	}
	if !hasObservationDomain(input.ObservationErrors, CapacityObservationPDBs) {
		plan.Checks = append(plan.Checks, checkPDBs(input.PDBs)...)
	}
	if !hasObservationDomain(input.ObservationErrors, CapacityObservationClusterAutoscaler) {
		plan.Checks = append(plan.Checks, checkClusterAutoscaler(input.ClusterAutoscaler)...)
	}
}

func hasObservationDomain(observationErrors []CapacityObservationError, domain CapacityObservationDomain) bool {
	for _, observationErr := range observationErrors {
		if capacityObservationDomain(observationErr) == domain {
			return true
		}
	}
	return false
}

// capacityObservationDomain normalizes legacy errors that only populated the
// display-oriented Source field. All analysis decisions use the returned
// typed domain; source matching is confined to this compatibility boundary.
func capacityObservationDomain(observationErr CapacityObservationError) CapacityObservationDomain {
	if observationErr.Domain != "" {
		return observationErr.Domain
	}
	switch observationErr.Source {
	case "scale target":
		return CapacityObservationScaleTarget
	case "scale target Pod selector", "scale target Pods":
		return CapacityObservationPendingPods
	case "Pod resource requests":
		return CapacityObservationPodResources
	case "ResourceQuotas":
		return CapacityObservationResourceQuotas
	case "LimitRanges":
		return CapacityObservationLimitRanges
	case "cluster request headroom":
		return CapacityObservationNodeCapacity
	case "PodDisruptionBudgets":
		return CapacityObservationPDBs
	case "Cluster Autoscaler detection":
		return CapacityObservationClusterAutoscaler
	}
	return ""
}

// CapacityObservationDomainForError exports the domain normalization function.
// This is exported for testing in the hpa root package.
func CapacityObservationDomainForError(observationErr CapacityObservationError) CapacityObservationDomain {
	return capacityObservationDomain(observationErr)
}

// hasNodeCapacityShortfall returns true when any check result indicates
// insufficient node allocatable CPU or memory.
func hasNodeCapacityShortfall(checks []CapacityCheckResult) bool {
	for _, c := range checks {
		if c.capacityStatus() == CapacityCheckFail &&
			(c.CheckID == CapacityCheckNodeCPU ||
				c.CheckID == CapacityCheckNodeMemory ||
				c.CheckID == CapacityCheckNodePods ||
				c.CheckID == CapacityCheckNodeSchedulable) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Check functions
// ---------------------------------------------------------------------------

func newCapacityCheckResult(id CapacityCheckID, status CapacityCheckStatus, message string) CapacityCheckResult {
	return CapacityCheckResult{
		CheckID: id,
		Status:  status,
		Pass:    status == CapacityCheckPass,
		Unknown: status == CapacityCheckUnknown,
		Message: message,
	}
}

// capacityStatus resolves legacy values that were decoded from JSON produced
// before the typed Status field was introduced.
func (c CapacityCheckResult) capacityStatus() CapacityCheckStatus {
	switch c.Status {
	case CapacityCheckPass, CapacityCheckFail, CapacityCheckUnknown:
		return c.Status
	case "":
		// Fall through to the legacy Pass/Unknown fields.
	default:
		// Unknown enum values can come from newer or corrupt serialized input.
		// Fail closed instead of accidentally treating the check as successful.
		return CapacityCheckUnknown
	}
	if c.Unknown {
		return CapacityCheckUnknown
	}
	if c.Pass {
		return CapacityCheckPass
	}
	return CapacityCheckFail
}

func checkObservationErrors(observationErrors []CapacityObservationError) []CapacityCheckResult {
	results := make([]CapacityCheckResult, 0, len(observationErrors))
	for _, observationErr := range observationErrors {
		source := strings.TrimSpace(observationErr.Source)
		if source == "" {
			source = "capacity input"
		}
		message := strings.TrimSpace(observationErr.Message)
		if message == "" {
			message = "unavailable"
		}
		result := newCapacityCheckResult(
			CapacityCheckObservation,
			CapacityCheckUnknown,
			fmt.Sprintf("%s unknown: %s", source, message),
		)
		result.ObservationDomain = capacityObservationDomain(observationErr)
		results = append(results, result)
	}
	return results
}
