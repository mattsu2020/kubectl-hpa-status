package hpa

import "fmt"

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
