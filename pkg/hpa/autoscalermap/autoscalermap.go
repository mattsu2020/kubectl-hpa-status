// Package autoscalermap visualizes the HPA-to-Node-Autoscaler relationship
// chain (HPA -> workload -> pods -> nodes -> autoscaler), identifying
// blockers at each layer. It depends only on pkg/hpa/internal/util for the
// shared PendingPodInfo type; the cmd/ layer calls it directly
// (autoscalermap.Analyze, autoscalermap.WriteText).
package autoscalermap

import (
	"fmt"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

// Analyze produces a visualization of the HPA-to-Node Autoscaler
// relationship chain, identifying blockers at each layer.
func Analyze(input Input) *Map {
	am := &Map{
		Namespace:       input.Namespace,
		HPAName:         input.HPAName,
		Target:          input.Target,
		CurrentReplicas: input.CurrentReplicas,
		DesiredReplicas: input.DesiredReplicas,
		MaxReplicas:     input.MaxReplicas,
		Layers:          []Layer{},
		Blockers:        []Blocker{},
		NextActions:     []string{},
		NextChecks:      []string{},
		Warnings:        []string{},
	}

	// Layer 1: HPA.
	am.Layers = append(am.Layers, buildHPALayer(input, am))

	// Layer 2: Workload.
	am.Layers = append(am.Layers, buildWorkloadLayer(input, am))

	// Layer 3: Pods.
	am.Layers = append(am.Layers, buildPodsLayer(input, am))

	// Layer 4: Nodes.
	am.Layers = append(am.Layers, buildNodesLayer(input, am))

	// Layer 5: Autoscaler.
	am.Layers = append(am.Layers, buildAutoscalerLayer(input, am))

	// Layer 6: External scaler (KEDA).
	appendKEDALayer(input, am)

	// Layer 7: Constraints (VPA, PDB, Quota).
	appendConstraintsLayer(input, am)

	// Build summary, recommendation, next actions, risk, and next checks.
	am.Summary, am.Recommendation, am.NextActions, am.Risk = buildAutoscalerMapSummaryEnhanced(am, input)

	return am
}

// buildHPALayer constructs the HPA layer and records a high-severity blocker when scaling is inactive.
func buildHPALayer(input Input, am *Map) Layer {
	hpaLayer := Layer{
		Name:     "hpa",
		Resource: fmt.Sprintf("%s/%s", input.Namespace, input.HPAName),
		Status:   fmt.Sprintf("current=%d desired=%d max=%d", input.CurrentReplicas, input.DesiredReplicas, input.MaxReplicas),
		Healthy:  input.ScalingActive,
	}
	if !input.ScalingActive {
		hpaLayer.Details = append(hpaLayer.Details, "ScalingActive is False")
		am.Blockers = append(am.Blockers, Blocker{
			Layer:    "hpa",
			Severity: "high",
			Message:  "HPA ScalingActive is False; cannot compute scaling recommendations",
		})
	}
	return hpaLayer
}

// buildWorkloadLayer constructs the workload layer and records a medium-severity blocker when pods are not converged.
func buildWorkloadLayer(input Input, am *Map) Layer {
	workloadHealthy := input.WorkloadReadyReplicas >= input.WorkloadDesiredReplicas
	workloadLayer := Layer{
		Name:     "workload",
		Resource: input.Target,
		Status:   fmt.Sprintf("desired=%d ready=%d", input.WorkloadDesiredReplicas, input.WorkloadReadyReplicas),
		Healthy:  workloadHealthy,
	}
	if !workloadHealthy {
		workloadLayer.Details = append(workloadLayer.Details,
			fmt.Sprintf("workload not converged: %d/%d pods ready", input.WorkloadReadyReplicas, input.WorkloadDesiredReplicas))
		am.Blockers = append(am.Blockers, Blocker{
			Layer:    "workload",
			Severity: "medium",
			Message:  fmt.Sprintf("Workload has %d ready pods but desires %d", input.WorkloadReadyReplicas, input.WorkloadDesiredReplicas),
		})
	}
	return workloadLayer
}

// buildPodsLayer constructs the pods layer and records a high-severity blocker when pods are pending.
func buildPodsLayer(input Input, am *Map) Layer {
	podsHealthy := input.PodSummary.Pending == 0
	podLayer := Layer{
		Name:     "pods",
		Resource: fmt.Sprintf("%d pods", input.PodSummary.Total),
		Status:   fmt.Sprintf("running=%d pending=%d ready=%d", input.PodSummary.Running, input.PodSummary.Pending, input.PodSummary.Ready),
		Healthy:  podsHealthy,
	}
	if input.PodSummary.Pending > 0 {
		pendReasons := summarizePendingPods(input.PendingPods)
		podLayer.Details = append(podLayer.Details, pendReasons...)
		am.Blockers = append(am.Blockers, Blocker{
			Layer:    "pods",
			Severity: "high",
			Message:  fmt.Sprintf("%d pods are Pending", input.PodSummary.Pending),
			Detail:   strings.Join(pendReasons, "; "),
		})
	}
	return podLayer
}

// buildNodesLayer constructs the nodes layer, including capacity details, tainted-node hints, and a blocker when no nodes are found.
func buildNodesLayer(input Input, am *Map) Layer {
	if input.NodeFetchError != "" {
		am.Warnings = append(am.Warnings, "node capacity unavailable: "+input.NodeFetchError)
		return Layer{
			Name:     "nodes",
			Resource: "unknown",
			Status:   "node data unavailable",
			Details:  []string{input.NodeFetchError},
			Healthy:  false,
		}
	}
	nodesHealthy := input.NodeSummary.TotalNodes > 0
	nodeLayer := Layer{
		Name:     "nodes",
		Resource: fmt.Sprintf("%d nodes", input.NodeSummary.TotalNodes),
		Healthy:  nodesHealthy,
	}
	if input.NodeSummary.TotalNodes > 0 {
		nodeLayer.Status = nodeCapacityStatus(input.NodeSummary)
		nodeLayer.Details = append(nodeLayer.Details, nodeCapacityDetails(input.NodeSummary)...)
	} else {
		nodeLayer.Status = "no nodes found"
		am.Blockers = append(am.Blockers, Blocker{
			Layer:    "nodes",
			Severity: "high",
			Message:  "No schedulable nodes found in cluster",
		})
	}
	return nodeLayer
}

// nodeCapacityStatus builds the comma-separated allocatable capacity summary for the nodes layer.
func nodeCapacityStatus(s NodeSummary) string {
	parts := []string{}
	if s.AllocatableCPU != "" {
		parts = append(parts, fmt.Sprintf("CPU %s", s.AllocatableCPU))
	}
	if s.AllocatableMemory != "" {
		parts = append(parts, fmt.Sprintf("memory %s", s.AllocatableMemory))
	}
	return strings.Join(parts, ", ")
}

// nodeCapacityDetails builds human-readable detail lines covering taints, matching node pools, and per-pod requests.
func nodeCapacityDetails(s NodeSummary) []string {
	var details []string
	if s.TaintedNodes > 0 {
		details = append(details, fmt.Sprintf("%d tainted node(s) with NoSchedule/NoExecute", s.TaintedNodes))
	}
	if len(s.MatchingNodePools) > 0 {
		details = append(details, fmt.Sprintf("matching node pools: %s", strings.Join(s.MatchingNodePools, ", ")))
	}
	if s.PodCPURequest != "" || s.PodMemoryRequest != "" {
		podParts := []string{}
		if s.PodCPURequest != "" {
			podParts = append(podParts, fmt.Sprintf("CPU %s/pod", s.PodCPURequest))
		}
		if s.PodMemoryRequest != "" {
			podParts = append(podParts, fmt.Sprintf("memory %s/pod", s.PodMemoryRequest))
		}
		details = append(details, fmt.Sprintf("pod requests: %s", strings.Join(podParts, ", ")))
	}
	return details
}

// buildAutoscalerLayer constructs the autoscaler layer and records a blocker when an autoscaler is missing despite pending pods.
func buildAutoscalerLayer(input Input, am *Map) Layer {
	autoscalerDetected := input.ClusterAutoscaler || input.Karpenter
	autoscalerLayer := Layer{
		Name:     "autoscaler",
		Resource: detectAutoscalerType(input.ClusterAutoscaler, input.Karpenter),
		Healthy:  autoscalerDetected,
	}
	if autoscalerDetected {
		autoscalerLayer.Status = "provisioner ready"
		if input.Karpenter {
			autoscalerLayer.Details = append(autoscalerLayer.Details, "Karpenter detected")
		}
		if input.ClusterAutoscaler {
			autoscalerLayer.Details = append(autoscalerLayer.Details, "Cluster Autoscaler detected")
		}
	} else {
		autoscalerLayer.Status = "not detected"
		if input.PodSummary.Pending > 0 {
			am.Blockers = append(am.Blockers, Blocker{
				Layer:    "autoscaler",
				Severity: "medium",
				Message:  "No node autoscaler detected; pending pods may not be scheduled",
				Detail:   "Consider installing Cluster Autoscaler or Karpenter to handle node provisioning",
			})
		}
	}
	return autoscalerLayer
}

// appendKEDALayer appends the KEDA external-scaler layer and records a high-severity blocker when triggers are inactive.
func appendKEDALayer(input Input, am *Map) {
	if input.KEDAInfo == nil {
		return
	}
	kedaLayer := Layer{
		Name:     "external-scaler",
		Resource: fmt.Sprintf("ScaledObject/%s", input.KEDAInfo.ScaledObjectName),
		Status:   fmt.Sprintf("triggers=%d active=%t", input.KEDAInfo.TriggerCount, input.KEDAInfo.Active),
		Healthy:  input.KEDAInfo.Active,
	}
	if !input.KEDAInfo.Active {
		kedaLayer.Details = append(kedaLayer.Details, "KEDA ScaledObject triggers are inactive")
		am.Blockers = append(am.Blockers, Blocker{
			Layer:    "external-scaler",
			Severity: "high",
			Message:  fmt.Sprintf("KEDA ScaledObject %s triggers are inactive", input.KEDAInfo.ScaledObjectName),
			Detail:   "KEDA will not signal the HPA to scale; check trigger configuration and external metric source",
		})
	}
	kedaLayer.Details = append(kedaLayer.Details, fmt.Sprintf("owns HPA %s", input.HPAName))
	am.Layers = append(am.Layers, kedaLayer)
	am.NextChecks = append(am.NextChecks,
		fmt.Sprintf("kubectl describe scaledobject %s -n %s", input.KEDAInfo.ScaledObjectName, input.Namespace))
}

// appendConstraintsLayer appends the constraints layer (VPA, PDB, Quota) and records associated blockers and next-checks.
func appendConstraintsLayer(input Input, am *Map) {
	constraintDetails := []string{}
	constraintHealthy := true

	if input.VPAInfo != nil {
		constraintDetails, constraintHealthy = appendVPAConstraint(input, am, constraintDetails), false
	}
	for _, pdb := range input.PDBs {
		constraintDetails = appendPDBConstraint(input, am, constraintDetails, pdb)
	}
	for _, quota := range input.Quotas {
		constraintDetails, constraintHealthy = appendAutoscalerMapQuota(input, am, constraintDetails, quota, constraintHealthy)
	}
	if len(input.Quotas) > 0 {
		am.NextChecks = append(am.NextChecks,
			fmt.Sprintf("kubectl get resourcequota -n %s", input.Namespace))
	}

	if len(constraintDetails) > 0 || input.VPAInfo != nil || len(input.PDBs) > 0 || len(input.Quotas) > 0 {
		constraintLayer := Layer{
			Name:     "constraints",
			Resource: "VPA, PDB, Quota",
			Status:   fmt.Sprintf("vpa=%t pdbs=%d quotas=%d", input.VPAInfo != nil, len(input.PDBs), len(input.Quotas)),
			Healthy:  constraintHealthy,
			Details:  constraintDetails,
		}
		if constraintHealthy && len(constraintDetails) > 0 {
			constraintLayer.Status = "constraints present, no conflicts"
		}
		am.Layers = append(am.Layers, constraintLayer)
	}
}

func appendVPAConstraint(input Input, am *Map, constraintDetails []string) []string {
	conflictStr := strings.Join(input.VPAInfo.ConflictResources, ", ")
	constraintDetails = append(constraintDetails,
		fmt.Sprintf("VPA/%s (%s) controls %s — may conflict with HPA",
			input.VPAInfo.VPAName, input.VPAInfo.UpdateMode, conflictStr))
	am.Blockers = append(am.Blockers, Blocker{
		Layer:    "constraints",
		Severity: "medium",
		Message: fmt.Sprintf("VPA/%s (%s mode) controls %s; may conflict with HPA CPU/memory targets",
			input.VPAInfo.VPAName, input.VPAInfo.UpdateMode, conflictStr),
		Detail: "HPA and VPA both managing CPU/memory can cause oscillation. Consider switching VPA to Off or Auto mode with different resources.",
	})
	am.NextChecks = append(am.NextChecks,
		fmt.Sprintf("kubectl describe vpa %s -n %s", input.VPAInfo.VPAName, input.Namespace))
	return constraintDetails
}

func appendPDBConstraint(input Input, am *Map, constraintDetails []string, pdb PDB) []string {
	pdbDesc := pdb.Name
	if pdb.MinAvailable != "" {
		pdbDesc += fmt.Sprintf(" (minAvailable=%s)", pdb.MinAvailable)
	}
	if pdb.MaxUnavailable != "" {
		pdbDesc += fmt.Sprintf(" (maxUnavailable=%s)", pdb.MaxUnavailable)
	}
	constraintDetails = append(constraintDetails,
		fmt.Sprintf("PDB %s may limit scale-down velocity", pdbDesc))
	am.Blockers = append(am.Blockers, Blocker{
		Layer:    "constraints",
		Severity: "low",
		Message:  fmt.Sprintf("PDB %s may block pod eviction during scale-down", pdb.Name),
	})
	am.NextChecks = append(am.NextChecks,
		fmt.Sprintf("kubectl describe pdb %s -n %s", pdb.Name, input.Namespace))
	return constraintDetails
}

func appendAutoscalerMapQuota(input Input, am *Map, constraintDetails []string, quota Quota, constraintHealthy bool) ([]string, bool) {
	constraintDetails = append(constraintDetails,
		fmt.Sprintf("Quota %s/%s at %.0f%% (%s/%s)", quota.Name, quota.Resource, quota.Ratio*100, quota.Used, quota.Hard))
	if quota.Ratio < 0.9 {
		return constraintDetails, constraintHealthy
	}
	am.Blockers = append(am.Blockers, Blocker{
		Layer:    "constraints",
		Severity: "high",
		Message:  fmt.Sprintf("Quota %s/%s at %.0f%% — HPA scale-up may exceed quota", quota.Name, quota.Resource, quota.Ratio*100),
		Detail:   fmt.Sprintf("maxReplicas=%d may exceed namespace quota for %s", input.MaxReplicas, quota.Resource),
	})
	return constraintDetails, false
}

// summarizePendingPods extracts scheduling failure reasons from pending pods.
func summarizePendingPods(pods []util.PendingPodInfo) []string {
	var reasons []string
	seen := make(map[string]struct{})
	for _, p := range pods {
		if !p.Unschedulable {
			continue
		}
		for _, r := range p.Reasons {
			if _, ok := seen[r]; !ok {
				seen[r] = struct{}{}
				reasons = append(reasons, r)
			}
		}
	}
	if len(reasons) == 0 && len(pods) > 0 {
		reasons = append(reasons, fmt.Sprintf("%d pod(s) pending", len(pods)))
	}
	return reasons
}

// detectAutoscalerType returns the autoscaler type string.
func detectAutoscalerType(ca, karpenter bool) string {
	switch {
	case ca && karpenter:
		return "Cluster Autoscaler + Karpenter"
	case karpenter:
		return "Karpenter"
	case ca:
		return "Cluster Autoscaler"
	default:
		return "none"
	}
}

// buildAutoscalerMapSummaryEnhanced produces the overall summary, recommendation,
// next actions, and risk assessment.
func buildAutoscalerMapSummaryEnhanced(am *Map, input Input) (string, string, []string, string) {
	blockerCount := len(am.Blockers)
	highCount, mediumCount, lowCount := countBlockerSeverities(am.Blockers)

	summary := "autoscaling chain is healthy"
	if blockerCount > 0 {
		summary = fmt.Sprintf("%d blocker(s) detected in autoscaling chain", blockerCount)
	}

	rec := ""
	if highCount > 0 {
		rec = fmt.Sprintf("Address %d high-severity blocker(s) to restore autoscaling health.", highCount)
	} else if blockerCount > 0 {
		rec = "Minor blockers detected; monitor for escalation."
	}

	actions := []string{fmt.Sprintf("kubectl get hpa %s -n %s", input.HPAName, input.Namespace)}
	if input.PodSummary.Pending > 0 {
		actions = append(actions, fmt.Sprintf("kubectl get pods -n %s -l <selector> | grep Pending", input.Namespace))
	}
	if !input.ClusterAutoscaler && !input.Karpenter && input.PodSummary.Pending > 0 {
		actions = append(actions, "Consider installing Cluster Autoscaler or Karpenter")
	}

	// Compute risk level.
	risk := "none"
	switch {
	case highCount > 0:
		risk = "high"
	case mediumCount > 0:
		risk = "medium"
	case lowCount > 0:
		risk = "low"
	}

	return summary, rec, actions, risk
}

// countBlockerSeverities tallies blockers by their high/medium/low severity.
func countBlockerSeverities(blockers []Blocker) (high, medium, low int) {
	for _, b := range blockers {
		switch b.Severity {
		case "high":
			high++
		case "medium":
			medium++
		case "low":
			low++
		}
	}
	return high, medium, low
}
