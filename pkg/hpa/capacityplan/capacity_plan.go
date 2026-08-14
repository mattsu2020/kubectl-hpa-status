package capacityplan

import (
	"fmt"
	"math"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/resourceutil"
)

// AnalyzeCapacityPlan produces a capacity plan that diagnoses whether it is
// safe to raise HPA maxReplicas. It runs 7 checks against namespace quotas,
// LimitRanges, node capacity, pending pods, PDBs, and Cluster Autoscaler
// presence.
func AnalyzeCapacityPlan(input CapacityPlanInput) *CapacityPlan {
	input.ObservationErrors = append([]CapacityObservationError(nil), input.ObservationErrors...)
	input.ObservationErrors = append(input.ObservationErrors, validateCapacityQuantityInputs(input)...)
	input.ObservationErrors = append(input.ObservationErrors, unsupportedLimitRangeObservations(input.LimitRanges, input.ContainerResources)...)
	resourceInputsUnknown := hasObservationDomain(input.ObservationErrors, CapacityObservationScaleTarget) ||
		hasObservationDomain(input.ObservationErrors, CapacityObservationPodResources)

	targetMax, targetErrs := resolveTargetMax(input)
	input.ObservationErrors = append(input.ObservationErrors, targetErrs...)

	additionalPods := computeAdditionalPods(targetMax, input.CurrentReplicas)
	demand := computeCapacityDemand(input, additionalPods, resourceInputsUnknown)
	plan := newCapacityPlan(input, targetMax, additionalPods, demand)

	// Run all checks.
	appendCapacityAnalysisChecks(
		plan,
		capacityCheckContext{input: input, demand: demand, additionalPods: additionalPods},
	)

	finalizeCapacityPlan(plan, input, demand, resourceInputsUnknown)

	return plan
}

// resolveTargetMax determines the target maxReplicas for the plan, returning
// plan-input observation errors when the target cannot be used.
func resolveTargetMax(input CapacityPlanInput) (int32, []CapacityObservationError) {
	targetMax := input.TargetMaxReplicas
	switch {
	case targetMax == 0:
		targetMax = computeTargetMax(input.MaxReplicas, input.CurrentReplicas)
		if targetMax <= input.MaxReplicas {
			return targetMax, []CapacityObservationError{{
				Domain:  CapacityObservationPlanInput,
				Source:  "target maxReplicas",
				Message: "maxReplicas is already at the int32 limit and cannot be raised",
			}}
		}
	case targetMax <= input.MaxReplicas:
		return targetMax, []CapacityObservationError{{
			Domain: CapacityObservationPlanInput,
			Source: "target maxReplicas",
			Message: fmt.Sprintf(
				"must be greater than the current maxReplicas (%d), got %d",
				input.MaxReplicas,
				targetMax,
			),
		}}
	}
	return targetMax, nil
}

// computeAdditionalPods clamps the pod delta implied by the target into the
// int32 range.
func computeAdditionalPods(targetMax, currentReplicas int32) int32 {
	additionalPods64 := int64(targetMax) - int64(currentReplicas)
	if additionalPods64 < 0 {
		additionalPods64 = 0
	}
	if additionalPods64 > math.MaxInt32 {
		additionalPods64 = math.MaxInt32
	}
	return int32(additionalPods64)
}

// capacityDemand carries the per-pod and aggregate resource demand the plan's
// checks compare against cluster headroom.
type capacityDemand struct {
	perPodCPU         resource.Quantity
	perPodMemory      resource.Quantity
	perPodLimitCPU    resource.Quantity
	perPodLimitMemory resource.Quantity
	totalCPU          resource.Quantity
	totalMemory       resource.Quantity
	totalLimitCPU     resource.Quantity
	totalLimitMemory  resource.Quantity
}

// computeCapacityDemand derives per-pod and total resource demand for the
// additional pods, leaving requests unknown when the observation inputs
// themselves were unusable.
func computeCapacityDemand(input CapacityPlanInput, additionalPods int32, resourceInputsUnknown bool) capacityDemand {
	var perPodCPU, perPodMemory resource.Quantity
	if !resourceInputsUnknown {
		perPodCPU, perPodMemory = effectivePerPodResources(input)
	}
	perPodLimitCPU, perPodLimitMemory := effectivePerPodLimits(input)
	return capacityDemand{
		perPodCPU:         perPodCPU,
		perPodMemory:      perPodMemory,
		perPodLimitCPU:    perPodLimitCPU,
		perPodLimitMemory: perPodLimitMemory,
		totalCPU:          resourceutil.Multiply(perPodCPU, int64(additionalPods)),
		totalMemory:       resourceutil.Multiply(perPodMemory, int64(additionalPods)),
		totalLimitCPU:     resourceutil.Multiply(perPodLimitCPU, int64(additionalPods)),
		totalLimitMemory:  resourceutil.Multiply(perPodLimitMemory, int64(additionalPods)),
	}
}

// newCapacityPlan builds the plan skeleton shared by every check before the
// checks and derived fields are populated.
func newCapacityPlan(input CapacityPlanInput, targetMax, additionalPods int32, demand capacityDemand) *CapacityPlan {
	issue := "HPA is not at maxReplicas"
	if input.CurrentReplicas >= input.MaxReplicas {
		issue = "HPA is capped at maxReplicas"
	}
	return &CapacityPlan{
		Namespace:         input.Namespace,
		Name:              input.HPAName,
		Target:            input.Target,
		CurrentReplicas:   input.CurrentReplicas,
		MaxReplicas:       input.MaxReplicas,
		Issue:             issue,
		TargetMaxReplicas: targetMax,
		AdditionalPods:    additionalPods,
		RequiredCPU:       demand.totalCPU.String(),
		RequiredMemory:    demand.totalMemory.String(),
	}
}

// finalizeCapacityPlan fills the derived fields that depend on check results
// and on which observation domains came back unknown.
func finalizeCapacityPlan(plan *CapacityPlan, input CapacityPlanInput, demand capacityDemand, resourceInputsUnknown bool) {
	// Estimate schedulable now from remaining node capacity.
	nodeCapacityUnknown := hasObservationDomain(input.ObservationErrors, CapacityObservationNodeCapacity)
	if !resourceInputsUnknown && !nodeCapacityUnknown {
		plan.SchedulableNow = computeSchedulableNow(input.NodeCapacity, demand.perPodCPU, demand.perPodMemory, input.ReadyPods)
	}

	// Detect node autoscaler requirement.
	plan.NodeAutoscalerRequired = hasNodeCapacityShortfall(plan.Checks)

	// Suggest dry-run command.
	if !hasObservationDomain(input.ObservationErrors, CapacityObservationPlanInput) {
		plan.DryRunCommand = buildDryRunCommand(input.Namespace, input.HPAName, plan.TargetMaxReplicas)
	}

	// Derive recommendation.
	plan.Safe, plan.Recommendation, plan.NextActions = buildRecommendation(plan, input)
}

// computeTargetMax returns a default target maxReplicas using the same doubling
// formula as the suggestion engine. The 200-replica safety cap applies while
// the existing maximum is below it; an HPA already above the cap advances by
// one (or saturates at math.MaxInt32) without overflowing int32.
func computeTargetMax(currentMax, desired int32) int32 {
	if currentMax == math.MaxInt32 {
		return math.MaxInt32
	}
	next := int64(currentMax) * 2
	if int64(desired) > next {
		next = int64(desired)
	}
	if next <= int64(currentMax) {
		next = int64(currentMax) + 1
	}
	if currentMax < maxReplicasCap && next > int64(maxReplicasCap) {
		next = int64(maxReplicasCap)
	}
	if next > math.MaxInt32 {
		next = math.MaxInt32
	}
	return int32(next)
}

// buildDryRunCommand suggests a kubectl patch command for dry-run testing of
// the maxReplicas change.
func buildDryRunCommand(namespace, hpaName string, targetMax int32) string {
	patch := fmt.Sprintf(`{"spec":{"maxReplicas":%d}}`, targetMax)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: hpaName, Namespace: namespace}}
	return util.KubectlPatchCommandWithDryRun(hpa, patch, util.DryRunClient)
}
