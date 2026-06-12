package hpa

import (
	"fmt"
	"strings"
)

// AnalyzeAutoscalerMap produces a visualization of the HPA-to-Node Autoscaler
// relationship chain, identifying blockers at each layer.
func AnalyzeAutoscalerMap(input AutoscalerMapInput) *AutoscalerMap {
	am := &AutoscalerMap{
		Namespace:       input.Namespace,
		HPAName:         input.HPAName,
		Target:          input.Target,
		CurrentReplicas: input.CurrentReplicas,
		DesiredReplicas: input.DesiredReplicas,
		MaxReplicas:     input.MaxReplicas,
		Layers:          []AutoscalerMapLayer{},
		Blockers:        []AutoscalerMapBlocker{},
		NextActions:     []string{},
	}

	// Layer 1: HPA.
	hpaLayer := AutoscalerMapLayer{
		Name:     "hpa",
		Resource: fmt.Sprintf("%s/%s", input.Namespace, input.HPAName),
		Status:   fmt.Sprintf("current=%d desired=%d max=%d", input.CurrentReplicas, input.DesiredReplicas, input.MaxReplicas),
		Healthy:  input.ScalingActive,
	}
	if !input.ScalingActive {
		hpaLayer.Details = append(hpaLayer.Details, "ScalingActive is False")
		am.Blockers = append(am.Blockers, AutoscalerMapBlocker{
			Layer:   "hpa",
			Severity: "high",
			Message:  "HPA ScalingActive is False; cannot compute scaling recommendations",
		})
	}
	am.Layers = append(am.Layers, hpaLayer)

	// Layer 2: Workload.
	workloadHealthy := input.WorkloadReadyReplicas >= input.WorkloadDesiredReplicas
	workloadLayer := AutoscalerMapLayer{
		Name:     "workload",
		Resource: input.Target,
		Status:   fmt.Sprintf("desired=%d ready=%d", input.WorkloadDesiredReplicas, input.WorkloadReadyReplicas),
		Healthy:  workloadHealthy,
	}
	if !workloadHealthy {
		workloadLayer.Details = append(workloadLayer.Details,
			fmt.Sprintf("workload not converged: %d/%d pods ready", input.WorkloadReadyReplicas, input.WorkloadDesiredReplicas))
		am.Blockers = append(am.Blockers, AutoscalerMapBlocker{
			Layer:   "workload",
			Severity: "medium",
			Message:  fmt.Sprintf("Workload has %d ready pods but desires %d", input.WorkloadReadyReplicas, input.WorkloadDesiredReplicas),
		})
	}
	am.Layers = append(am.Layers, workloadLayer)

	// Layer 3: Pods.
	podsHealthy := input.PodSummary.Pending == 0
	podLayer := AutoscalerMapLayer{
		Name:     "pods",
		Resource: fmt.Sprintf("%d pods", input.PodSummary.Total),
		Status:   fmt.Sprintf("running=%d pending=%d ready=%d", input.PodSummary.Running, input.PodSummary.Pending, input.PodSummary.Ready),
		Healthy:  podsHealthy,
	}
	if input.PodSummary.Pending > 0 {
		pendReasons := summarizePendingPods(input.PendingPods)
		podLayer.Details = append(podLayer.Details, pendReasons...)
		am.Blockers = append(am.Blockers, AutoscalerMapBlocker{
			Layer:   "pods",
			Severity: "high",
			Message:  fmt.Sprintf("%d pods are Pending", input.PodSummary.Pending),
			Detail:   strings.Join(pendReasons, "; "),
		})
	}
	am.Layers = append(am.Layers, podLayer)

	// Layer 4: Nodes.
	nodesHealthy := input.NodeSummary.TotalNodes > 0
	nodeLayer := AutoscalerMapLayer{
		Name:     "nodes",
		Resource: fmt.Sprintf("%d nodes", input.NodeSummary.TotalNodes),
		Healthy:  nodesHealthy,
	}
	if input.NodeSummary.TotalNodes > 0 {
		parts := []string{}
		if input.NodeSummary.AllocatableCPU != "" {
			parts = append(parts, fmt.Sprintf("CPU %s", input.NodeSummary.AllocatableCPU))
		}
		if input.NodeSummary.AllocatableMemory != "" {
			parts = append(parts, fmt.Sprintf("memory %s", input.NodeSummary.AllocatableMemory))
		}
		nodeLayer.Status = strings.Join(parts, ", ")
		if input.NodeSummary.TaintedNodes > 0 {
			nodeLayer.Details = append(nodeLayer.Details,
				fmt.Sprintf("%d tainted node(s) with NoSchedule/NoExecute", input.NodeSummary.TaintedNodes))
		}
		if len(input.NodeSummary.MatchingNodePools) > 0 {
			nodeLayer.Details = append(nodeLayer.Details,
				fmt.Sprintf("matching node pools: %s", strings.Join(input.NodeSummary.MatchingNodePools, ", ")))
		}
		if input.NodeSummary.PodCPURequest != "" || input.NodeSummary.PodMemoryRequest != "" {
			podParts := []string{}
			if input.NodeSummary.PodCPURequest != "" {
				podParts = append(podParts, fmt.Sprintf("CPU %s/pod", input.NodeSummary.PodCPURequest))
			}
			if input.NodeSummary.PodMemoryRequest != "" {
				podParts = append(podParts, fmt.Sprintf("memory %s/pod", input.NodeSummary.PodMemoryRequest))
			}
			nodeLayer.Details = append(nodeLayer.Details, fmt.Sprintf("pod requests: %s", strings.Join(podParts, ", ")))
		}
	} else {
		nodeLayer.Status = "no nodes found"
		am.Blockers = append(am.Blockers, AutoscalerMapBlocker{
			Layer:   "nodes",
			Severity: "high",
			Message:  "No schedulable nodes found in cluster",
		})
	}
	am.Layers = append(am.Layers, nodeLayer)

	// Layer 5: Autoscaler.
	autoscalerDetected := input.ClusterAutoscaler || input.Karpenter
	autoscalerLayer := AutoscalerMapLayer{
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
			am.Blockers = append(am.Blockers, AutoscalerMapBlocker{
				Layer:   "autoscaler",
				Severity: "medium",
				Message:  "No node autoscaler detected; pending pods may not be scheduled",
				Detail:   "Consider installing Cluster Autoscaler or Karpenter to handle node provisioning",
			})
		}
	}
	am.Layers = append(am.Layers, autoscalerLayer)

	// Build summary and recommendation.
	am.Summary, am.Recommendation, am.NextActions = buildAutoscalerMapSummary(am, input)

	return am
}

// summarizePendingPods extracts scheduling failure reasons from pending pods.
func summarizePendingPods(pods []PendingPodInfo) []string {
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

// buildAutoscalerMapSummary produces the overall summary and recommendations.
func buildAutoscalerMapSummary(am *AutoscalerMap, input AutoscalerMapInput) (string, string, []string) {
	blockerCount := len(am.Blockers)
	highCount := 0
	for _, b := range am.Blockers {
		if b.Severity == "high" {
			highCount++
		}
	}

	summary := "autoscaling chain is healthy"
	if blockerCount > 0 {
		summary = fmt.Sprintf("%d blocker(s) detected in autoscaling chain", blockerCount)
	}

	rec := ""
	var actions []string

	if highCount > 0 {
		rec = fmt.Sprintf("Address %d high-severity blocker(s) to restore autoscaling health.", highCount)
	} else if blockerCount > 0 {
		rec = "Minor blockers detected; monitor for escalation."
	}

	actions = append(actions, fmt.Sprintf("kubectl get hpa %s -n %s", input.HPAName, input.Namespace))
	if input.PodSummary.Pending > 0 {
		actions = append(actions, fmt.Sprintf("kubectl get pods -n %s -l <selector> | grep Pending", input.Namespace))
	}
	if !input.ClusterAutoscaler && !input.Karpenter && input.PodSummary.Pending > 0 {
		actions = append(actions, "Consider installing Cluster Autoscaler or Karpenter")
	}

	return summary, rec, actions
}
