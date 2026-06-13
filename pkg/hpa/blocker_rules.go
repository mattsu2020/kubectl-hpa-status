package hpa

import (
	"fmt"
	"strings"
)

// blockerRule is a pure function that inspects BlockerInput and returns
// zero or more BlockerFindings.
type blockerRule func(input BlockerInput) []BlockerFinding

// coreBlockerRules returns the standard set of blocker detection rules,
// ordered from highest to lowest typical severity.
func coreBlockerRules() []blockerRule {
	return []blockerRule{
		scaleOutDesiredRule,
		pendingPodsRule,
		unschedulableRule,
		containerFailureRule,
		quotaNearLimitRule,
		readinessStalledRule,
		nodeCapacityRule,
		metricsHealthyInfoRule,
	}
}

// scaleOutDesiredRule detects when HPA wants more replicas than currently exist.
func scaleOutDesiredRule(input BlockerInput) []BlockerFinding {
	if input.DesiredReplicas <= input.CurrentReplicas {
		return nil
	}
	return []BlockerFinding{
		{
			ID:       "scale-out-desired",
			Severity: BlockerHigh,
			Category: "scaling",
			Message: fmt.Sprintf("HPA wants %d replicas but only %d are current",
				input.DesiredReplicas, input.CurrentReplicas),
			Detail: "The HPA controller has computed a higher desired replica count. " +
				"If pods are not appearing, investigate scheduling, quota, or application failures.",
		},
	}
}

// pendingPodsRule detects pods stuck in Pending phase.
func pendingPodsRule(input BlockerInput) []BlockerFinding {
	if len(input.PendingPods) == 0 {
		return nil
	}
	return []BlockerFinding{
		{
			ID:       "pending-pods",
			Severity: BlockerHigh,
			Category: "scheduling",
			Message:  fmt.Sprintf("%d pods are Pending", len(input.PendingPods)),
			Detail:   "Pending pods indicate that new replicas were requested but have not been scheduled. Check node capacity, taints, affinity, and namespace quotas.",
			NextCommand: fmt.Sprintf(
				"kubectl get pods -n %s --field-selector status.phase=Pending",
				describeNamespace(input)),
		},
	}
}

// unschedulableRule detects pods that are Pending and marked Unschedulable,
// cross-referenced with FailedScheduling events.
func unschedulableRule(input BlockerInput) []BlockerFinding {
	unschedulableCount := 0
	for _, pod := range input.PendingPods {
		if pod.Unschedulable {
			unschedulableCount++
		}
	}
	if unschedulableCount == 0 && len(input.FailedSchedulingEvents) == 0 {
		return nil
	}

	var findings []BlockerFinding

	if unschedulableCount > 0 {
		detail := "Unschedulable pods cannot be placed on any node. "
		firstReason := firstUnschedulableReasonFromInput(input)
		if firstReason != "" {
			detail += fmt.Sprintf("First reason: %s. ", firstReason)
		}
		detail += "Check node resources, taints/tolerations, nodeSelector, and affinity rules."
		findings = append(findings, BlockerFinding{
			ID:          "unschedulable-pods",
			Severity:    BlockerHigh,
			Category:    "scheduling",
			Message:     fmt.Sprintf("%d pod(s) are Unschedulable", unschedulableCount),
			Detail:      detail,
			NextCommand: fmt.Sprintf("kubectl describe pod %s -n %s", firstUnschedulablePodName(input), describeNamespace(input)),
		})
	}

	for _, eventMsg := range input.FailedSchedulingEvents {
		findings = append(findings, BlockerFinding{
			ID:          "failed-scheduling",
			Severity:    BlockerHigh,
			Category:    "scheduling",
			Message:     fmt.Sprintf("FailedScheduling: %s", truncateBlockerMessage(eventMsg, 120)),
			Detail:      "The Kubernetes scheduler could not find a suitable node for this pod.",
			NextCommand: "kubectl get events --field-selector reason=FailedScheduling",
		})
	}

	return findings
}

// containerFailureRule detects pods with containers in ImagePullBackOff or
// CrashLoopBackOff, which indicate application or image issues rather than
// HPA or infrastructure problems.
func containerFailureRule(input BlockerInput) []BlockerFinding {
	var findings []BlockerFinding
	backOffCount := 0
	crashLoopCount := 0

	for _, cs := range input.ContainerStatuses {
		if !cs.Waiting {
			continue
		}
		switch cs.WaitingReason {
		case "ImagePullBackOff", "ErrImagePull":
			backOffCount++
			findings = append(findings, BlockerFinding{
				ID:          "image-pull-failure",
				Severity:    BlockerHigh,
				Category:    "application",
				Message:     fmt.Sprintf("Pod %s container %s: %s", cs.Pod, cs.Container, cs.WaitingReason),
				Detail:      "The container image cannot be pulled. Verify the image name, tag, registry credentials, and network access.",
				NextCommand: fmt.Sprintf("kubectl describe pod %s", cs.Pod),
			})
		case "CrashLoopBackOff":
			crashLoopCount++
			findings = append(findings, BlockerFinding{
				ID:          "crash-loop",
				Severity:    BlockerHigh,
				Category:    "application",
				Message:     fmt.Sprintf("Pod %s container %s: CrashLoopBackOff (restarts: %d)", cs.Pod, cs.Container, cs.RestartCount),
				Detail:      "The container is crashing repeatedly. Check application logs for the root cause.",
				NextCommand: fmt.Sprintf("kubectl logs %s --previous", cs.Pod),
			})
		}
	}

	// Deduplicate: if many pods have the same issue, emit one summary finding instead.
	if backOffCount > 1 {
		findings = deduplicateFindings(findings, "image-pull-failure",
			fmt.Sprintf("%d pods have image pull failures (ImagePullBackOff/ErrImagePull)", backOffCount),
			"Multiple pods cannot pull their container images. Verify the image registry, credentials, and network.",
			"kubectl get pods --field-selector status.containerStatuses[0].state.waiting.reason=ImagePullBackOff")
	}
	if crashLoopCount > 1 {
		findings = deduplicateFindings(findings, "crash-loop",
			fmt.Sprintf("%d pods are in CrashLoopBackOff", crashLoopCount),
			"Multiple pods are crashing repeatedly. Check application logs and recent deployments.",
			"kubectl get pods --field-selector status.containerStatuses[0].state.waiting.reason=CrashLoopBackOff")
	}

	return findings
}

// quotaNearLimitRule detects ResourceQuotas where usage is at or above 80%.
func quotaNearLimitRule(input BlockerInput) []BlockerFinding {
	var findings []BlockerFinding
	for _, q := range input.Quotas {
		severity := BlockerMedium
		if q.Ratio >= 0.95 {
			severity = BlockerHigh
		}
		findings = append(findings, BlockerFinding{
			ID:          "quota-near-limit",
			Severity:    severity,
			Category:    "quota",
			Message:     fmt.Sprintf("ResourceQuota %q %s is %.0f%% used (%s/%s)", q.Name, q.Resource, q.Ratio*100, q.Used, q.Hard),
			Detail:      "Namespace ResourceQuota is near its limit. New pods may be rejected if the quota is exhausted.",
			NextCommand: fmt.Sprintf("kubectl describe quota %s", q.Name),
		})
	}
	return findings
}

// readinessStalledRule detects when ReadyReplicas is stuck below DesiredReplicas
// but no pods are Pending (suggesting readinessProbe or startup issues).
func readinessStalledRule(input BlockerInput) []BlockerFinding {
	if input.TargetReadyReplicas >= input.DesiredReplicas {
		return nil
	}
	if len(input.PendingPods) > 0 {
		// Pending pods already covered by pendingPodsRule.
		return nil
	}

	gap := input.DesiredReplicas - input.TargetReadyReplicas
	return []BlockerFinding{
		{
			ID:          "readiness-stalled",
			Severity:    BlockerMedium,
			Category:    "readiness",
			Message:     fmt.Sprintf("%d pod(s) are not Ready (%d Ready / %d desired)", gap, input.TargetReadyReplicas, input.DesiredReplicas),
			Detail:      "Pods exist but are not passing readiness probes. This could indicate slow startup, misconfigured probes, or application issues.",
			NextCommand: fmt.Sprintf("kubectl get pods -n %s -o wide", describeNamespace(input)),
		},
	}
}

// nodeCapacityRule provides findings based on node-level capacity analysis.
// Only produces findings when node data is available (--capacity-deep).
func nodeCapacityRule(input BlockerInput) []BlockerFinding {
	if input.NodeCapacity == nil {
		return nil
	}

	var findings []BlockerFinding
	nc := input.NodeCapacity

	if nc.TotalNodes == 0 {
		findings = append(findings, BlockerFinding{
			ID:          "no-nodes",
			Severity:    BlockerHigh,
			Category:    "scheduling",
			Message:     "No nodes found in the cluster",
			Detail:      "The cluster has no schedulable nodes. Scale-out will always fail until nodes are added.",
			NextCommand: "kubectl get nodes",
		})
		return findings
	}

	if nc.TaintedNodes == nc.TotalNodes && nc.TotalNodes > 0 {
		findings = append(findings, BlockerFinding{
			ID:          "all-nodes-tainted",
			Severity:    BlockerHigh,
			Category:    "scheduling",
			Message:     fmt.Sprintf("All %d nodes have NoSchedule/NoExecute taints", nc.TotalNodes),
			Detail:      "Every node has at least one taint that blocks scheduling. Pods without matching tolerations cannot be placed.",
			NextCommand: "kubectl describe nodes | grep -A5 Taints",
		})
	}

	return findings
}

// metricsHealthyInfoRule produces an informational finding when no metric
// retrieval issues are detected.
func metricsHealthyInfoRule(input BlockerInput) []BlockerFinding {
	if !input.ScalingActive {
		return []BlockerFinding{
			{
				ID:          "metrics-healthy",
				Severity:    BlockerInfo,
				Category:    "info",
				Message:     "HPA ScalingActive is False — metrics may not be reaching the controller",
				Detail:      "When ScalingActive is False, the HPA cannot compute reliable scaling decisions. Check the metrics pipeline (Prometheus Adapter, metrics-server, etc.).",
				NextCommand: "kubectl get --raw /apis/metrics.k8s.io/v1beta1/namespaces/" + describeNamespace(input) + "/pods",
			},
		}
	}
	return []BlockerFinding{
		{
			ID:       "metrics-healthy",
			Severity: BlockerInfo,
			Category: "info",
			Message:  "No recent metrics retrieval errors found",
		},
	}
}

// firstUnschedulableReasonFromInput returns the first non-empty reason from
// unschedulable pending pods.
func firstUnschedulableReasonFromInput(input BlockerInput) string {
	for _, pod := range input.PendingPods {
		if !pod.Unschedulable {
			continue
		}
		for _, reason := range pod.Reasons {
			if strings.TrimSpace(reason) != "" {
				return reason
			}
		}
	}
	return ""
}

// firstUnschedulablePodName returns the name of the first unschedulable pod.
func firstUnschedulablePodName(input BlockerInput) string {
	for _, pod := range input.PendingPods {
		if pod.Unschedulable {
			return pod.Name
		}
	}
	if len(input.PendingPods) > 0 {
		return input.PendingPods[0].Name
	}
	return "<pending-pod>"
}

// describeNamespace returns the namespace from the input or "default" as fallback.
func describeNamespace(input BlockerInput) string {
	if input.Namespace != "" {
		return input.Namespace
	}
	return "default"
}

// truncateBlockerMessage truncates a message to maxLen characters.
func truncateBlockerMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}

// deduplicateFindings replaces all findings with a given ID prefix by a single
// summary finding with the provided message, detail, and nextCommand.
func deduplicateFindings(findings []BlockerFinding, id string, message, detail, nextCommand string) []BlockerFinding {
	var kept []BlockerFinding
	var count int
	var category string
	for _, f := range findings {
		if f.ID == id {
			count++
			if category == "" {
				category = f.Category
			}
			continue
		}
		kept = append(kept, f)
	}
	if count > 0 {
		kept = append(kept, BlockerFinding{
			ID:          id,
			Severity:    BlockerHigh,
			Category:    category,
			Message:     message,
			Detail:      detail,
			NextCommand: nextCommand,
		})
	}
	return kept
}
