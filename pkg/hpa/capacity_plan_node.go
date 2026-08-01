package hpa

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
)

// This file holds the node-level capacity checks (allocatable headroom, Pod
// slots, per-node fragmentation) plus the pending-Pod, PDB, and Cluster
// Autoscaler checks, split out of capacity_plan.go so that file stays within
// the project's file-size guidance.

// checkNodeCapacity emits one typed check per node-capacity dimension. Each
// dimension is evaluated by its own helper below; this function only decides
// which dimensions apply and in what order they are reported.
//
// Result order is part of the rendered output and is asserted by tests: any
// resource shortfalls first (CPU, then memory), then the per-resource pass
// results, then Pod slots, then per-node fragmentation.
func checkNodeCapacity(
	nc *blocker.NodeCapacitySummary,
	requiredCPU, requiredMemory, perPodCPU, perPodMemory resource.Quantity,
	additionalPods int32,
	hasCA bool,
) []CapacityCheckResult {
	if nc == nil {
		return []CapacityCheckResult{
			newCapacityCheckResult(CapacityCheckNodeCapacity, CapacityCheckPass, "node capacity not checked (use --capacity-deep for full analysis)"),
		}
	}

	if noSchedulableNodes(nc) {
		return []CapacityCheckResult{
			newCapacityCheckResult(CapacityCheckNodeSchedulable, CapacityCheckFail, "no schedulable nodes found in cluster"),
		}
	}

	var results []CapacityCheckResult
	if !nc.PodCapacityKnown {
		result := newCapacityCheckResult(
			CapacityCheckObservation,
			CapacityCheckUnknown,
			"node allocatable Pod slots are unavailable",
		)
		result.ObservationDomain = CapacityObservationNodeCapacity
		results = append(results, result)
	}

	headroom := nodeHeadroomFor(nc)
	cpuShortfall := checkNodeResourceHeadroom(headroom.cpu, "CPU", CapacityCheckNodeCPU, requiredCPU, headroom.afterRequests, hasCA)
	memoryShortfall := checkNodeResourceHeadroom(headroom.memory, "memory", CapacityCheckNodeMemory, requiredMemory, headroom.afterRequests, hasCA)

	if cpuShortfall != nil {
		results = append(results, *cpuShortfall)
	}
	if memoryShortfall != nil {
		results = append(results, *memoryShortfall)
	}
	if cpuShortfall == nil {
		results = append(results, newCapacityCheckResult(CapacityCheckNodeCPU, CapacityCheckPass, "node CPU appears sufficient for additional pods"))
	}
	if memoryShortfall == nil {
		results = append(results, newCapacityCheckResult(CapacityCheckNodeMemory, CapacityCheckPass, "node memory appears sufficient for additional pods"))
	}

	if nc.PodCapacityKnown {
		results = append(results, checkNodePodSlots(nc, additionalPods, hasCA))
	}
	if fragmentation := checkNodeFragmentation(nc, perPodCPU, perPodMemory, additionalPods, hasCA); fragmentation != nil {
		results = append(results, *fragmentation)
	}

	return results
}

// noSchedulableNodes reports whether the cluster has no node that could accept
// a new Pod. When the schedulable count is unknown, an all-tainted cluster is
// treated the same way.
func noSchedulableNodes(nc *blocker.NodeCapacitySummary) bool {
	if nc.TotalNodes == 0 {
		return true
	}
	if nc.SchedulableNodesKnown {
		return nc.SchedulableNodes == 0
	}
	return nc.TaintedNodes >= nc.TotalNodes
}

// nodeHeadroomView names which node-level figure the resource checks compare
// against: raw allocatable, or allocatable minus already-scheduled Pod
// requests when the deeper per-node read supplied it.
type nodeHeadroomView struct {
	cpu           string
	memory        string
	afterRequests bool
}

func nodeHeadroomFor(nc *blocker.NodeCapacitySummary) nodeHeadroomView {
	if nc.AvailableCPU != "" || nc.AvailableMemory != "" {
		return nodeHeadroomView{cpu: nc.AvailableCPU, memory: nc.AvailableMemory, afterRequests: true}
	}
	return nodeHeadroomView{cpu: nc.AllocCPU, memory: nc.AllocMemory}
}

// checkNodeResourceHeadroom compares one resource dimension against what the
// additional Pods need. It returns nil when the dimension is not constrained
// (nothing required, or enough headroom), which the caller reads as "pass".
func checkNodeResourceHeadroom(
	available, resourceName string,
	id CapacityCheckID,
	required resource.Quantity,
	afterRequests, hasCA bool,
) *CapacityCheckResult {
	if required.IsZero() {
		return nil
	}
	// Cmp has a pointer receiver, so the parsed quantity needs a name.
	headroom := parseQuantityOrZero(available)
	if headroom.Cmp(required) >= 0 {
		return nil
	}

	label := "node allocatable " + resourceName
	if afterRequests {
		label += " headroom after scheduled Pod requests"
	}
	message := withClusterAutoscalerNote(
		fmt.Sprintf("%s: %s, required for additional pods: %s", label, available, required.String()),
		hasCA,
		"nodes",
	)
	result := newCapacityCheckResult(id, CapacityCheckFail, message)
	return &result
}

// checkNodePodSlots compares the cluster-wide remaining Pod slots against the
// additional Pods. Only called when the slot count is known.
func checkNodePodSlots(nc *blocker.NodeCapacitySummary, additionalPods int32, hasCA bool) CapacityCheckResult {
	if nc.AvailablePods >= int64(additionalPods) {
		return newCapacityCheckResult(
			CapacityCheckNodePods,
			CapacityCheckPass,
			fmt.Sprintf("node Pod slots appear sufficient for %d additional pod(s)", additionalPods),
		)
	}
	message := withClusterAutoscalerNote(
		fmt.Sprintf("node Pod slots remaining: %d, required for additional pods: %d", nc.AvailablePods, additionalPods),
		hasCA,
		"nodes",
	)
	return newCapacityCheckResult(CapacityCheckNodePods, CapacityCheckFail, message)
}

// checkNodeFragmentation detects the case where the cluster has enough total
// headroom but no single node can take a Pod. It returns nil when there is no
// per-node data to reason about.
func checkNodeFragmentation(
	nc *blocker.NodeCapacitySummary,
	perPodCPU, perPodMemory resource.Quantity,
	additionalPods int32,
	hasCA bool,
) *CapacityCheckResult {
	if len(nc.NodeHeadrooms) == 0 || additionalPods <= 0 {
		return nil
	}

	var fit int64
	for _, node := range nc.NodeHeadrooms {
		nodeFit := int64(podsFitInResources(
			parseQuantityOrZero(node.AvailableCPU),
			parseQuantityOrZero(node.AvailableMemory),
			perPodCPU,
			perPodMemory,
		))
		if node.PodCapacityKnown && nodeFit > node.AvailablePods {
			nodeFit = node.AvailablePods
		}
		if nodeFit > 0 {
			fit += nodeFit
		}
		if fit >= int64(additionalPods) {
			break
		}
	}

	if fit >= int64(additionalPods) {
		result := newCapacityCheckResult(
			CapacityCheckNodeSchedulable,
			CapacityCheckPass,
			fmt.Sprintf("per-node resource headroom fits all %d additional pod(s)", additionalPods),
		)
		return &result
	}

	message := withClusterAutoscalerNote(
		fmt.Sprintf("per-node resource headroom fits %d of %d additional pod(s); aggregate capacity is fragmented", fit, additionalPods),
		hasCA,
		"a suitable node",
	)
	result := newCapacityCheckResult(CapacityCheckNodeSchedulable, CapacityCheckFail, message)
	return &result
}

// withClusterAutoscalerNote appends the "CA may provision ..." caveat so a
// node shortfall is not read as a hard blocker on a cluster that can grow.
func withClusterAutoscalerNote(message string, hasCA bool, provisions string) string {
	if !hasCA {
		return message
	}
	return message + " (Cluster Autoscaler may provision " + provisions + ")"
}

func checkPendingPods(pending []PendingPodInfo) []CapacityCheckResult {
	if len(pending) == 0 {
		return []CapacityCheckResult{
			newCapacityCheckResult(CapacityCheckPendingPods, CapacityCheckPass, "no pending pods for scale target"),
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
			newCapacityCheckResult(CapacityCheckPendingPods, CapacityCheckFail, msg),
		}
	}

	return []CapacityCheckResult{
		newCapacityCheckResult(CapacityCheckPendingPods, CapacityCheckPass, fmt.Sprintf("%d pending pod(s) but none unschedulable", len(pending))),
	}
}

func checkPDBs(pdbs []PDBInterference) []CapacityCheckResult {
	if len(pdbs) == 0 {
		return []CapacityCheckResult{
			newCapacityCheckResult(CapacityCheckPDB, CapacityCheckPass, "no PodDisruptionBudgets in namespace"),
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
			newCapacityCheckResult(CapacityCheckPDB, CapacityCheckPass, fmt.Sprintf("PDB %s may slow scale-down (informational)", strings.Join(blocking, ", "))),
		}
	}

	return []CapacityCheckResult{
		newCapacityCheckResult(CapacityCheckPDB, CapacityCheckPass, "PodDisruptionBudgets will not block scale-down"),
	}
}

func checkClusterAutoscaler(detected bool) []CapacityCheckResult {
	if !detected {
		return nil
	}
	return []CapacityCheckResult{
		newCapacityCheckResult(CapacityCheckClusterAutoscaler, CapacityCheckPass, "Cluster Autoscaler detected; node provisioning may handle additional pods"),
	}
}
