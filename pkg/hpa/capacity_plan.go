package hpa

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

const capacityMaxReplicasCap int32 = 200

// AnalyzeCapacityPlan produces a capacity plan that diagnoses whether it is
// safe to raise HPA maxReplicas. It runs 7 checks against namespace quotas,
// LimitRanges, node capacity, pending pods, PDBs, and Cluster Autoscaler
// presence.
func AnalyzeCapacityPlan(input CapacityPlanInput) *CapacityPlan {
	targetMax := input.TargetMaxReplicas
	if targetMax <= input.MaxReplicas {
		targetMax = computeTargetMax(input.MaxReplicas, input.CurrentReplicas)
	}

	additionalPods := targetMax - input.CurrentReplicas
	if additionalPods < 0 {
		additionalPods = 0
	}

	perPodCPU, perPodMemory := sumContainerResources(input.ContainerResources)
	totalCPU := multiplyQuantity(perPodCPU, int64(additionalPods))
	totalMemory := multiplyQuantity(perPodMemory, int64(additionalPods))

	issue := "HPA is not at maxReplicas"
	if input.CurrentReplicas >= input.MaxReplicas {
		issue = "HPA is capped at maxReplicas"
	}

	plan := &CapacityPlan{
		Namespace:         input.Namespace,
		Name:              input.HPAName,
		Target:            input.Target,
		CurrentReplicas:   input.CurrentReplicas,
		MaxReplicas:       input.MaxReplicas,
		Issue:             issue,
		TargetMaxReplicas: targetMax,
		AdditionalPods:    additionalPods,
		RequiredCPU:       totalCPU.String(),
		RequiredMemory:    totalMemory.String(),
	}

	// Run all checks.
	plan.Checks = append(plan.Checks, checkQuotaHeadroom(input.Quotas, totalCPU, totalMemory)...)
	plan.Checks = append(plan.Checks, checkLimitRanges(input.LimitRanges, input.ContainerResources)...)
	plan.Checks = append(plan.Checks, checkNodeCapacity(input.NodeCapacity, totalCPU, totalMemory, input.ClusterAutoscaler)...)
	plan.Checks = append(plan.Checks, checkPendingPods(input.PendingPods)...)
	plan.Checks = append(plan.Checks, checkPDBs(input.PDBs)...)
	plan.Checks = append(plan.Checks, checkClusterAutoscaler(input.ClusterAutoscaler)...)

	// Derive recommendation.
	plan.Safe, plan.Recommendation, plan.NextActions = buildRecommendation(plan, input)

	return plan
}

// computeTargetMax returns a default target maxReplicas using the same doubling
// formula as the suggestion engine, capped at 200.
func computeTargetMax(currentMax, desired int32) int32 {
	next := currentMax * 2
	if desired > next {
		next = desired
	}
	if next <= currentMax {
		next = currentMax + 1
	}
	if next > capacityMaxReplicasCap {
		next = capacityMaxReplicasCap
	}
	if next <= currentMax {
		next = currentMax + 1
	}
	return next
}

// sumContainerResources sums CPU and memory requests across all containers
// into per-pod totals.
func sumContainerResources(containers []CapacityContainerResources) (resource.Quantity, resource.Quantity) {
	var totalCPU, totalMemory resource.Quantity
	for _, c := range containers {
		if c.CPU != "" && c.CPU != "0" {
			q := resource.MustParse(c.CPU)
			totalCPU.Add(q)
		}
		if c.Memory != "" && c.Memory != "0" {
			q := resource.MustParse(c.Memory)
			totalMemory.Add(q)
		}
	}
	return totalCPU, totalMemory
}

// multiplyQuantity scales a quantity by a multiplier using MilliValue to
// preserve sub-core precision (e.g. "250m" * 10 = "2500m"). Safe up to
// ~46000 Ti of memory or ~46000 cores with the current 200-replica cap.
func multiplyQuantity(q resource.Quantity, multiplier int64) resource.Quantity {
	if multiplier <= 0 || q.IsZero() {
		return resource.Quantity{}
	}
	scaled := q.MilliValue() * multiplier
	return *resource.NewMilliQuantity(scaled, q.Format)
}

// ---------------------------------------------------------------------------
// Check functions
// ---------------------------------------------------------------------------

func checkQuotaHeadroom(quotas []CapacityQuotaInfo, requiredCPU, requiredMemory resource.Quantity) []CapacityCheckResult {
	if len(quotas) == 0 {
		return []CapacityCheckResult{
			{Pass: true, Message: "no namespace ResourceQuotas found"},
		}
	}

	// Build a map of remaining (hard - used) per resource.
	remaining := make(map[string]resource.Quantity)
	for _, q := range quotas {
		hard := resource.MustParse(q.Hard)
		used := resource.MustParse(q.Used)
		rem := hard.DeepCopy()
		rem.Sub(used)
		// Keep the largest remaining for each resource type (multiple quotas).
		if existing, ok := remaining[q.Resource]; !ok || rem.Cmp(existing) > 0 {
			remaining[q.Resource] = rem
		}
	}

	var results []CapacityCheckResult

	// Check CPU quota.
	cpuRem := findMatchingRemaining(remaining, "cpu")
	if cpuRem != nil {
		if cpuRem.Cmp(requiredCPU) >= 0 {
			results = append(results, CapacityCheckResult{
				Pass:    true,
				Message: "namespace quota has enough CPU",
			})
		} else {
			results = append(results, CapacityCheckResult{
				Pass:    false,
				Message: fmt.Sprintf("namespace quota CPU remaining: %s, required: %s", cpuRem.String(), requiredCPU.String()),
			})
		}
	}

	// Check memory quota.
	memRem := findMatchingRemaining(remaining, "memory")
	if memRem != nil {
		if memRem.Cmp(requiredMemory) >= 0 {
			results = append(results, CapacityCheckResult{
				Pass:    true,
				Message: "namespace quota has enough memory",
			})
		} else {
			results = append(results, CapacityCheckResult{
				Pass:    false,
				Message: fmt.Sprintf("namespace quota memory remaining: %s, required: %s", memRem.String(), requiredMemory.String()),
			})
		}
	}

	// If no cpu/memory quota found, report pass.
	if len(results) == 0 {
		results = append(results, CapacityCheckResult{
			Pass:    true,
			Message: "namespace quota does not constrain cpu/memory",
		})
	}

	return results
}

// findMatchingRemaining looks up remaining quota for a resource type,
// matching both plain names ("cpu") and request-prefixed names
// ("requests.cpu").
func findMatchingRemaining(remaining map[string]resource.Quantity, resourceType string) *resource.Quantity {
	if q, ok := remaining[resourceType]; ok {
		return &q
	}
	if q, ok := remaining["requests."+resourceType]; ok {
		return &q
	}
	return nil
}

func checkLimitRanges(limitRanges []LimitRangeConstraint, containers []CapacityContainerResources) []CapacityCheckResult {
	if len(limitRanges) == 0 {
		return []CapacityCheckResult{
			{Pass: true, Message: "no LimitRange constraints in namespace"},
		}
	}

	var results []CapacityCheckResult
	violated := false

	for _, c := range containers {
		for _, lr := range limitRanges {
			if lr.Type != "Container" {
				continue
			}
			switch lr.Resource {
			case "cpu":
				if c.CPU != "" && c.CPU != "0" {
					req := resource.MustParse(c.CPU)
					if lr.Max != "" {
						max := resource.MustParse(lr.Max)
						if req.Cmp(max) > 0 {
							violated = true
							results = append(results, CapacityCheckResult{
								Pass:    false,
								Message: fmt.Sprintf("container %q CPU request %s exceeds LimitRange %q max %s", c.Name, c.CPU, lr.Name, lr.Max),
							})
						}
					}
					if lr.Min != "" {
						min := resource.MustParse(lr.Min)
						if req.Cmp(min) < 0 {
							violated = true
							results = append(results, CapacityCheckResult{
								Pass:    false,
								Message: fmt.Sprintf("container %q CPU request %s below LimitRange %q min %s", c.Name, c.CPU, lr.Name, lr.Min),
							})
						}
					}
				}
			case "memory":
				if c.Memory != "" && c.Memory != "0" {
					req := resource.MustParse(c.Memory)
					if lr.Max != "" {
						max := resource.MustParse(lr.Max)
						if req.Cmp(max) > 0 {
							violated = true
							results = append(results, CapacityCheckResult{
								Pass:    false,
								Message: fmt.Sprintf("container %q memory request %s exceeds LimitRange %q max %s", c.Name, c.Memory, lr.Name, lr.Max),
							})
						}
					}
					if lr.Min != "" {
						min := resource.MustParse(lr.Min)
						if req.Cmp(min) < 0 {
							violated = true
							results = append(results, CapacityCheckResult{
								Pass:    false,
								Message: fmt.Sprintf("container %q memory request %s below LimitRange %q min %s", c.Name, c.Memory, lr.Name, lr.Min),
							})
						}
					}
				}
			}
		}
	}

	if !violated {
		results = append(results, CapacityCheckResult{
			Pass:    true,
			Message: "all container requests within LimitRange bounds",
		})
	}

	return results
}

func checkNodeCapacity(nc *NodeCapacitySummary, requiredCPU, requiredMemory resource.Quantity, hasCA bool) []CapacityCheckResult {
	if nc == nil {
		return []CapacityCheckResult{
			{Pass: true, Message: "node capacity not checked (use --capacity-deep for full analysis)"},
		}
	}

	if nc.TotalNodes == 0 {
		return []CapacityCheckResult{
			{Pass: false, Message: "no schedulable nodes found in cluster"},
		}
	}

	var results []CapacityCheckResult
	cpuOK := true
	memOK := true

	if !requiredCPU.IsZero() {
		allocCPU := resource.MustParse(nc.AllocCPU)
		if allocCPU.Cmp(requiredCPU) < 0 {
			cpuOK = false
			msg := fmt.Sprintf("node allocatable CPU: %s, required for additional pods: %s", nc.AllocCPU, requiredCPU.String())
			if hasCA {
				msg += " (Cluster Autoscaler may provision nodes)"
			}
			results = append(results, CapacityCheckResult{Pass: false, Message: msg})
		}
	}
	if !requiredMemory.IsZero() {
		allocMem := resource.MustParse(nc.AllocMemory)
		if allocMem.Cmp(requiredMemory) < 0 {
			memOK = false
			msg := fmt.Sprintf("node allocatable memory: %s, required for additional pods: %s", nc.AllocMemory, requiredMemory.String())
			if hasCA {
				msg += " (Cluster Autoscaler may provision nodes)"
			}
			results = append(results, CapacityCheckResult{Pass: false, Message: msg})
		}
	}

	if cpuOK {
		results = append(results, CapacityCheckResult{Pass: true, Message: "node CPU appears sufficient for additional pods"})
	}
	if memOK {
		results = append(results, CapacityCheckResult{Pass: true, Message: "node memory appears sufficient for additional pods"})
	}

	return results
}

func checkPendingPods(pending []PendingPodInfo) []CapacityCheckResult {
	if len(pending) == 0 {
		return []CapacityCheckResult{
			{Pass: true, Message: "no pending pods for scale target"},
		}
	}

	unschedulable := 0
	var reasons []string
	for _, p := range pending {
		if p.Unschedulable {
			unschedulable++
			if len(p.Reasons) > 0 && len(reasons) < 3 {
				reasons = append(reasons, p.Reasons[0])
			}
		}
	}

	if unschedulable > 0 {
		msg := fmt.Sprintf("%d pod(s) are already Pending; scaling will create more", len(pending))
		if len(reasons) > 0 {
			msg = fmt.Sprintf("%d pod(s) are already Pending due to %s", len(pending), strings.Join(reasons, "; "))
		}
		return []CapacityCheckResult{
			{Pass: false, Message: msg},
		}
	}

	return []CapacityCheckResult{
		{Pass: true, Message: fmt.Sprintf("%d pending pod(s) but none unschedulable", len(pending))},
	}
}

func checkPDBs(pdbs []PDBInterference) []CapacityCheckResult {
	if len(pdbs) == 0 {
		return []CapacityCheckResult{
			{Pass: true, Message: "no PodDisruptionBudgets in namespace"},
		}
	}

	var blocking []string
	for _, pdb := range pdbs {
		if pdb.Disruption == "none" || (pdb.MinAvailable != "" && pdb.MinAvailable != "0") {
			blocking = append(blocking, pdb.Name)
		}
	}

	if len(blocking) > 0 {
		return []CapacityCheckResult{
			{Pass: true, Message: fmt.Sprintf("PDB %s may slow scale-down (informational)", strings.Join(blocking, ", "))},
		}
	}

	return []CapacityCheckResult{
		{Pass: true, Message: "PodDisruptionBudgets will not block scale-down"},
	}
}

func checkClusterAutoscaler(detected bool) []CapacityCheckResult {
	if !detected {
		return nil
	}
	return []CapacityCheckResult{
		{Pass: true, Message: "Cluster Autoscaler detected; node provisioning may handle additional pods"},
	}
}

// ---------------------------------------------------------------------------
// Recommendation builder
// ---------------------------------------------------------------------------

func buildRecommendation(plan *CapacityPlan, input CapacityPlanInput) (bool, string, []string) {
	failedChecks := 0
	for _, c := range plan.Checks {
		if !c.Pass {
			failedChecks++
		}
	}

	if failedChecks == 0 {
		return true, fmt.Sprintf("Safe to raise maxReplicas to %d.", plan.TargetMaxReplicas), nil
	}

	// Determine if only node capacity is the issue (and CA is present).
	onlyNodeCapacity := input.ClusterAutoscaler && failedChecks == countFailingByPrefix(plan.Checks, "node allocatable")
	if onlyNodeCapacity {
		return true, fmt.Sprintf("Likely safe to raise maxReplicas to %d; Cluster Autoscaler will provision nodes, but monitor for provisioning delays.", plan.TargetMaxReplicas), []string{
			"Monitor node provisioning after raising maxReplicas",
			"Watch for Pending pods with kubectl get pods -w",
		}
	}

	var actions []string
	for _, c := range plan.Checks {
		if c.Pass {
			continue
		}
		if strings.Contains(c.Message, "quota CPU remaining") {
			actions = append(actions, "Increase namespace CPU quota or reduce pod CPU requests")
		}
		if strings.Contains(c.Message, "quota memory remaining") {
			actions = append(actions, "Increase namespace memory quota or reduce pod memory requests")
		}
		if strings.Contains(c.Message, "exceeds LimitRange") {
			actions = append(actions, "Adjust pod requests or LimitRange constraints")
		}
		if strings.Contains(c.Message, "below LimitRange") {
			actions = append(actions, "Raise container requests to meet LimitRange minimums")
		}
		if strings.Contains(c.Message, "no schedulable nodes") {
			actions = append(actions, "Add nodes or remove blocking taints")
		}
		if strings.Contains(c.Message, "already Pending") {
			actions = append(actions, "Resolve pending pod scheduling issues before scaling")
		}
	}

	if len(actions) == 0 {
		actions = append(actions, "Review capacity constraints before raising maxReplicas")
	}

	rec := fmt.Sprintf("Do not raise maxReplicas to %d yet.", plan.TargetMaxReplicas)
	return false, rec, actions
}

// countFailingByPrefix counts failed checks whose message starts with prefix.
func countFailingByPrefix(checks []CapacityCheckResult, prefix string) int {
	count := 0
	for _, c := range checks {
		if !c.Pass && strings.HasPrefix(c.Message, prefix) {
			count++
		}
	}
	return count
}
