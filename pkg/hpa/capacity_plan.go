package hpa

import (
	"fmt"
	"math"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

const capacityMaxReplicasCap int32 = 200

// parseQuantityOrZero is the calculation fallback after
// validateCapacityQuantityInputs has converted malformed values into explicit
// unknown observations. A malformed value must never silently produce a Safe
// recommendation.
func parseQuantityOrZero(s string) resource.Quantity {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return resource.Quantity{}
	}
	return q
}

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
		input,
		demand.perPodCPU,
		demand.perPodMemory,
		demand.perPodLimitCPU,
		demand.perPodLimitMemory,
		demand.totalCPU,
		demand.totalMemory,
		demand.totalLimitCPU,
		demand.totalLimitMemory,
		additionalPods,
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
		totalCPU:          multiplyQuantity(perPodCPU, int64(additionalPods)),
		totalMemory:       multiplyQuantity(perPodMemory, int64(additionalPods)),
		totalLimitCPU:     multiplyQuantity(perPodLimitCPU, int64(additionalPods)),
		totalLimitMemory:  multiplyQuantity(perPodLimitMemory, int64(additionalPods)),
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

func validateCapacityQuantityInputs(input CapacityPlanInput) []CapacityObservationError {
	var validationErrors []CapacityObservationError
	validate := func(domain CapacityObservationDomain, source, value string, allowNegative bool) {
		if value == "" {
			return
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			validationErrors = append(validationErrors, CapacityObservationError{
				Domain:  domain,
				Source:  source,
				Message: "invalid Kubernetes quantity: value must not be whitespace-only",
			})
			return
		}
		quantity, err := resource.ParseQuantity(value)
		switch {
		case err != nil:
			validationErrors = append(validationErrors, CapacityObservationError{
				Domain:  domain,
				Source:  source,
				Message: fmt.Sprintf("invalid Kubernetes quantity %q: %v", value, err),
			})
		case !allowNegative && quantity.Sign() < 0:
			validationErrors = append(validationErrors, CapacityObservationError{
				Domain:  domain,
				Source:  source,
				Message: fmt.Sprintf("invalid Kubernetes quantity %q: value must not be negative", value),
			})
		}
	}

	validate(CapacityObservationPodResources, "PodRequestCPU", input.PodRequestCPU, false)
	validate(CapacityObservationPodResources, "PodRequestMemory", input.PodRequestMemory, false)
	validate(CapacityObservationPodResources, "PodLimitCPU", input.PodLimitCPU, false)
	validate(CapacityObservationPodResources, "PodLimitMemory", input.PodLimitMemory, false)
	validate(CapacityObservationPodResources, "PodLevelRequestCPU", input.PodLevelRequestCPU, false)
	validate(CapacityObservationPodResources, "PodLevelRequestMemory", input.PodLevelRequestMemory, false)
	validate(CapacityObservationPodResources, "PodLevelLimitCPU", input.PodLevelLimitCPU, false)
	validate(CapacityObservationPodResources, "PodLevelLimitMemory", input.PodLevelLimitMemory, false)
	for _, container := range input.ContainerResources {
		validate(CapacityObservationPodResources, fmt.Sprintf("container %q CPU request", container.Name), container.CPU, false)
		validate(CapacityObservationPodResources, fmt.Sprintf("container %q memory request", container.Name), container.Memory, false)
		validate(CapacityObservationPodResources, fmt.Sprintf("container %q CPU limit", container.Name), container.LimitCPU, false)
		validate(CapacityObservationPodResources, fmt.Sprintf("container %q memory limit", container.Name), container.LimitMemory, false)
	}
	for _, quota := range input.Quotas {
		validate(CapacityObservationResourceQuotas, fmt.Sprintf("ResourceQuota %q %s hard", quota.Name, quota.Resource), quota.Hard, false)
		validate(CapacityObservationResourceQuotas, fmt.Sprintf("ResourceQuota %q %s used", quota.Name, quota.Resource), quota.Used, false)
	}
	for _, limitRange := range input.LimitRanges {
		validate(CapacityObservationLimitRanges, fmt.Sprintf("LimitRange %q %s minimum", limitRange.Name, limitRange.Resource), limitRange.Min, false)
		validate(CapacityObservationLimitRanges, fmt.Sprintf("LimitRange %q %s maximum", limitRange.Name, limitRange.Resource), limitRange.Max, false)
		validate(CapacityObservationLimitRanges, fmt.Sprintf("LimitRange %q %s default", limitRange.Name, limitRange.Resource), limitRange.Default, false)
		validate(CapacityObservationLimitRanges, fmt.Sprintf("LimitRange %q %s default request", limitRange.Name, limitRange.Resource), limitRange.DefaultRequest, false)
		validate(CapacityObservationLimitRanges, fmt.Sprintf("LimitRange %q %s max limit/request ratio", limitRange.Name, limitRange.Resource), limitRange.MaxLimitRequestRatio, false)
	}
	if input.NodeCapacity != nil {
		validate(CapacityObservationNodeCapacity, "node allocatable CPU", input.NodeCapacity.AllocCPU, false)
		validate(CapacityObservationNodeCapacity, "node allocatable memory", input.NodeCapacity.AllocMemory, false)
		// Available headroom can legitimately be negative when aggregate
		// scheduled Pod requests exceed allocatable capacity.
		validate(CapacityObservationNodeCapacity, "node available CPU", input.NodeCapacity.AvailableCPU, true)
		validate(CapacityObservationNodeCapacity, "node available memory", input.NodeCapacity.AvailableMemory, true)
		validate(CapacityObservationNodeCapacity, "node requested CPU", input.NodeCapacity.RequestedCPU, false)
		validate(CapacityObservationNodeCapacity, "node requested memory", input.NodeCapacity.RequestedMemory, false)
		for _, node := range input.NodeCapacity.NodeHeadrooms {
			validate(CapacityObservationNodeCapacity, fmt.Sprintf("node %q available CPU", node.Name), node.AvailableCPU, true)
			validate(CapacityObservationNodeCapacity, fmt.Sprintf("node %q available memory", node.Name), node.AvailableMemory, true)
		}
	}
	return validationErrors
}

func unsupportedLimitRangeObservations(limitRanges []LimitRangeConstraint, containers []CapacityContainerResources) []CapacityObservationError {
	var observations []CapacityObservationError
	for _, limitRange := range limitRanges {
		var semantics []string
		requestMissing, limitMissing := limitRangeResourceMissing(containers, limitRange.Resource)
		if limitRange.Default != "" && (limitRange.Type != "Container" || limitMissing) {
			semantics = append(semantics, "default limit")
		}
		if limitRange.DefaultRequest != "" && (limitRange.Type != "Container" || requestMissing) {
			semantics = append(semantics, "default request")
		}
		if len(semantics) == 0 {
			continue
		}
		observations = append(observations, CapacityObservationError{
			Domain: CapacityObservationLimitRangeDefaults,
			Source: fmt.Sprintf("LimitRange %q %s", limitRange.Name, limitRange.Resource),
			Message: fmt.Sprintf(
				"admission-time %s cannot be projected safely",
				strings.Join(semantics, ", "),
			),
		})
	}
	return observations
}

func limitRangeResourceMissing(containers []CapacityContainerResources, resourceName string) (requestMissing, limitMissing bool) {
	if len(containers) == 0 {
		return true, true
	}
	for _, container := range containers {
		request, limit, _ := limitRangeResourceValues(container, LimitRangeConstraint{Resource: resourceName})
		if strings.TrimSpace(request) == "" {
			requestMissing = true
		}
		if strings.TrimSpace(limit) == "" {
			limitMissing = true
		}
	}
	return requestMissing, limitMissing
}

func appendCapacityAnalysisChecks(
	plan *CapacityPlan,
	input CapacityPlanInput,
	perPodCPU, perPodMemory, perPodLimitCPU, perPodLimitMemory,
	totalCPU, totalMemory, totalLimitCPU, totalLimitMemory resource.Quantity,
	additionalPods int32,
) {
	plan.Checks = append(plan.Checks, checkObservationErrors(input.ObservationErrors)...)
	resourceInputsUnknown := hasObservationDomain(input.ObservationErrors, CapacityObservationScaleTarget) ||
		hasObservationDomain(input.ObservationErrors, CapacityObservationPodResources)
	if !resourceInputsUnknown && !hasObservationDomain(input.ObservationErrors, CapacityObservationResourceQuotas) {
		plan.Checks = append(plan.Checks, checkQuotaHeadroom(
			input.Quotas,
			input,
			totalCPU,
			totalMemory,
			totalLimitCPU,
			totalLimitMemory,
			additionalPods,
		)...)
	}
	if !resourceInputsUnknown && !hasObservationDomain(input.ObservationErrors, CapacityObservationLimitRanges) {
		plan.Checks = append(plan.Checks, checkLimitRanges(
			input.LimitRanges,
			input.ContainerResources,
			perPodCPU,
			perPodMemory,
			perPodLimitCPU,
			perPodLimitMemory,
		)...)
	}
	if !resourceInputsUnknown && !hasObservationDomain(input.ObservationErrors, CapacityObservationNodeCapacity) {
		plan.Checks = append(plan.Checks, checkNodeCapacity(
			input.NodeCapacity,
			totalCPU,
			totalMemory,
			perPodCPU,
			perPodMemory,
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

func effectivePerPodResources(input CapacityPlanInput) (resource.Quantity, resource.Quantity) {
	cpu, memory := sumContainerResources(input.ContainerResources)
	if input.PodRequestCPU != "" {
		cpu = parseQuantityOrZero(input.PodRequestCPU)
	}
	if input.PodRequestMemory != "" {
		memory = parseQuantityOrZero(input.PodRequestMemory)
	}
	return cpu, memory
}

func effectivePerPodLimits(input CapacityPlanInput) (resource.Quantity, resource.Quantity) {
	var cpu, memory resource.Quantity
	for _, container := range input.ContainerResources {
		if container.LimitCPU != "" {
			cpu.Add(parseQuantityOrZero(container.LimitCPU))
		}
		if container.LimitMemory != "" {
			memory.Add(parseQuantityOrZero(container.LimitMemory))
		}
	}
	if input.PodLimitCPU != "" {
		cpu = parseQuantityOrZero(input.PodLimitCPU)
	}
	if input.PodLimitMemory != "" {
		memory = parseQuantityOrZero(input.PodLimitMemory)
	}
	return cpu, memory
}

// computeTargetMax returns a default target maxReplicas using the same doubling
// formula as the suggestion engine. The 200-replica safety cap applies while
// the existing maximum is below it; an HPA already above the cap advances by
// one without overflowing int32.
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
	if currentMax < capacityMaxReplicasCap && next > int64(capacityMaxReplicasCap) {
		next = int64(capacityMaxReplicasCap)
	}
	if next > math.MaxInt32 {
		next = math.MaxInt32
	}
	return int32(next)
}

// sumContainerResources sums CPU and memory requests across all containers
// into per-pod totals.
func sumContainerResources(containers []CapacityContainerResources) (resource.Quantity, resource.Quantity) {
	var totalCPU, totalMemory resource.Quantity
	for _, c := range containers {
		if c.CPU != "" && c.CPU != "0" {
			q := parseQuantityOrZero(c.CPU)
			totalCPU.Add(q)
		}
		if c.Memory != "" && c.Memory != "0" {
			q := parseQuantityOrZero(c.Memory)
			totalMemory.Add(q)
		}
	}
	return totalCPU, totalMemory
}

// multiplyQuantity scales a quantity without routing through int64
// MilliValue arithmetic, which can overflow for large but valid quantities.
func multiplyQuantity(q resource.Quantity, multiplier int64) resource.Quantity {
	if multiplier <= 0 || q.IsZero() {
		return resource.Quantity{}
	}
	scaled := q.DeepCopy()
	scaled.Mul(multiplier)
	return scaled
}

// computeSchedulableNow estimates how many additional pods can be scheduled
// with current node capacity. It subtracts resources used by already-running
// pods (ReadyPods * per-pod resources) from total allocatable, then divides
// the remainder by per-pod resources. Returns 0 if node capacity is unavailable
// or per-pod resources cannot be determined.
func computeSchedulableNow(nc *blocker.NodeCapacitySummary, perPodCPU, perPodMemory resource.Quantity, readyPods int32) int32 {
	if nc == nil || nc.TotalNodes == 0 {
		return 0
	}

	if len(nc.NodeHeadrooms) > 0 {
		var total int64
		for _, node := range nc.NodeHeadrooms {
			cpu := parseQuantityOrZero(node.AvailableCPU)
			memory := parseQuantityOrZero(node.AvailableMemory)
			nodeFit := int64(podsFitInResources(cpu, memory, perPodCPU, perPodMemory))
			if node.PodCapacityKnown && nodeFit > node.AvailablePods {
				nodeFit = node.AvailablePods
			}
			if nodeFit > 0 {
				total += nodeFit
			}
			if total >= math.MaxInt32 {
				return math.MaxInt32
			}
		}
		return int32(total)
	}

	var remainingCPU, remainingMem resource.Quantity
	if nc.AvailableCPU != "" || nc.AvailableMemory != "" {
		remainingCPU = parseQuantityOrZero(nc.AvailableCPU)
		remainingMem = parseQuantityOrZero(nc.AvailableMemory)
	} else {
		// Compatibility fallback for callers that only supply aggregate
		// allocatable capacity. Live collection always supplies Available*,
		// based on all scheduled Pods.
		remainingCPU = parseQuantityOrZero(nc.AllocCPU)
		remainingMem = parseQuantityOrZero(nc.AllocMemory)
		remainingCPU.Sub(multiplyQuantity(perPodCPU, int64(readyPods)))
		remainingMem.Sub(multiplyQuantity(perPodMemory, int64(readyPods)))
	}

	fit := podsFitInResources(remainingCPU, remainingMem, perPodCPU, perPodMemory)
	if nc.PodCapacityKnown && int64(fit) > nc.AvailablePods {
		if nc.AvailablePods <= 0 {
			return 0
		}
		return int32(min(nc.AvailablePods, int64(math.MaxInt32)))
	}
	return fit
}

func podsFitInResources(availableCPU, availableMemory, perPodCPU, perPodMemory resource.Quantity) int32 {
	if perPodCPU.IsZero() && perPodMemory.IsZero() {
		// CPU/memory impose no bound for a BestEffort Pod. The caller caps
		// this sentinel with allocatable Pod slots when that observation is
		// available.
		return math.MaxInt32
	}
	fits := func(count int64) bool {
		if !perPodCPU.IsZero() && availableCPU.Cmp(multiplyQuantity(perPodCPU, count)) < 0 {
			return false
		}
		return perPodMemory.IsZero() || availableMemory.Cmp(multiplyQuantity(perPodMemory, count)) >= 0
	}
	var low int64
	high := int64(math.MaxInt32) + 1
	for low+1 < high {
		mid := low + (high-low)/2
		if fits(mid) {
			low = mid
		} else {
			high = mid
		}
	}
	return int32(low)
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

// buildDryRunCommand suggests a kubectl patch command for dry-run testing of
// the maxReplicas change.
func buildDryRunCommand(namespace, hpaName string, targetMax int32) string {
	patch := fmt.Sprintf(`{"spec":{"maxReplicas":%d}}`, targetMax)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: hpaName, Namespace: namespace}}
	return kubectlPatchCommandWithDryRun(hpa, patch, util.DryRunClient)
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

// ---------------------------------------------------------------------------
// Recommendation builder
// ---------------------------------------------------------------------------

func buildRecommendation(plan *CapacityPlan, input CapacityPlanInput) (bool, string, []string) {
	failedChecks := 0
	unknownChecks := 0
	for _, c := range plan.Checks {
		switch c.capacityStatus() {
		case CapacityCheckFail:
			failedChecks++
		case CapacityCheckUnknown:
			if capacityUnknownBlocksDecision(plan, c) {
				unknownChecks++
			}
		}
	}

	if unknownChecks > 0 {
		return false, fmt.Sprintf("Cannot confirm that raising maxReplicas to %d is safe because %d capacity observation(s) are unknown.", plan.TargetMaxReplicas, unknownChecks), []string{
			"Restore Kubernetes API access and rerun the capacity plan",
		}
	}

	if failedChecks == 0 {
		return true, fmt.Sprintf("Safe to raise maxReplicas to %d.", plan.TargetMaxReplicas), nil
	}

	// Determine if only node capacity is the issue (and CA is present).
	onlyNodeCapacity := input.ClusterAutoscaler &&
		failedChecks == countFailingChecks(
			plan.Checks,
			CapacityCheckNodeCPU,
			CapacityCheckNodeMemory,
			CapacityCheckNodePods,
			CapacityCheckNodeSchedulable,
		)
	if onlyNodeCapacity {
		return false, fmt.Sprintf("Cannot confirm that raising maxReplicas to %d is safe; Cluster Autoscaler is present, but compatible node-group capacity and limits were not verified.", plan.TargetMaxReplicas), []string{
			"Monitor node provisioning after raising maxReplicas",
			"Watch for Pending pods with kubectl get pods -w",
		}
	}

	actions := capacityRemediationActions(plan.Checks)
	if len(actions) == 0 {
		actions = append(actions, "Review capacity constraints before raising maxReplicas")
	}

	rec := fmt.Sprintf("Do not raise maxReplicas to %d yet.", plan.TargetMaxReplicas)
	return false, rec, actions
}

func capacityUnknownBlocksDecision(plan *CapacityPlan, check CapacityCheckResult) bool {
	switch check.ObservationDomain {
	case CapacityObservationPDBs:
		// PDBs affect voluntary disruption and scale-down, not whether new
		// replicas can be created and scheduled.
		return false
	case CapacityObservationClusterAutoscaler:
		// Autoscaler presence matters only when observed node headroom is
		// insufficient and autoscaling is the proposed fallback.
		return hasNodeCapacityShortfall(plan.Checks)
	default:
		return true
	}
}

// capacityRemediationActions maps each failing check ID to a suggested action.
func capacityRemediationActions(checks []CapacityCheckResult) []string {
	remediations := map[CapacityCheckID]string{
		CapacityCheckQuotaCPU:          "Increase namespace CPU quota or reduce pod CPU requests",
		CapacityCheckQuotaMemory:       "Increase namespace memory quota or reduce pod memory requests",
		CapacityCheckQuotaPods:         "Increase namespace Pod quota or lower the proposed maxReplicas",
		CapacityCheckQuotaLimitCPU:     "Increase namespace CPU limit quota or reduce pod CPU limits",
		CapacityCheckQuotaLimitMemory:  "Increase namespace memory limit quota or reduce pod memory limits",
		CapacityCheckLimitRangeMaximum: "Adjust pod requests or LimitRange constraints",
		CapacityCheckLimitRangeMinimum: "Raise container requests to meet LimitRange minimums",
		CapacityCheckNodeSchedulable:   "Add nodes or remove blocking taints",
		CapacityCheckNodePods:          "Add nodes or reduce the number of Pods scheduled per node",
		CapacityCheckPendingPods:       "Resolve pending pod scheduling issues before scaling",
	}
	var actions []string
	for _, c := range checks {
		if c.capacityStatus() != CapacityCheckFail {
			continue
		}
		if action, ok := remediations[c.CheckID]; ok {
			actions = append(actions, action)
		}
	}
	return actions
}

func countFailingChecks(checks []CapacityCheckResult, ids ...CapacityCheckID) int {
	wanted := make(map[CapacityCheckID]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	count := 0
	for _, c := range checks {
		if c.capacityStatus() != CapacityCheckFail {
			continue
		}
		if _, ok := wanted[c.CheckID]; ok {
			count++
		}
	}
	return count
}
