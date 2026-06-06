package hpa

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// AnalyzeScalePath builds a best-effort explanation of where an HPA scale-up
// request currently stops, using only visible Kubernetes API status and events.
func AnalyzeScalePath(hpa *autoscalingv2.HorizontalPodAutoscaler, input ScalePathInput) *ScalePath {
	if hpa == nil {
		return nil
	}

	target := input.Target
	targetKind := hpa.Spec.ScaleTargetRef.Kind
	targetName := hpa.Spec.ScaleTargetRef.Name
	targetDesired := hpa.Status.DesiredReplicas
	targetCurrent := hpa.Status.CurrentReplicas
	targetReady := int32(0)
	if target != nil {
		if target.Kind != "" {
			targetKind = target.Kind
		}
		if target.Name != "" {
			targetName = target.Name
		}
		if target.DesiredReplicas > 0 {
			targetDesired = target.DesiredReplicas
		}
		targetCurrent = target.CurrentReplicas
		targetReady = target.ReadyReplicas
	}

	counts := countScalePathPods(input.Pods)
	rsDesired, rsCurrent := sumScalePathReplicaSets(input.ReplicaSets)
	if rsDesired == 0 {
		rsDesired = targetDesired
	}
	if rsCurrent == 0 {
		rsCurrent = targetCurrent
	}

	path := &ScalePath{
		Steps: []ScalePathStep{
			{Name: "HPA", Summary: fmt.Sprintf("wants %d replicas", hpa.Status.DesiredReplicas)},
			{Name: "Target", Summary: fmt.Sprintf("%s/%s desired=%d", targetKind, targetName, targetDesired)},
			{Name: "ReplicaSet", Summary: fmt.Sprintf("created %d pods", rsCurrent)},
			{Name: "Pods", Summary: fmt.Sprintf("%d Ready / %d desired", counts.readyOrTarget(targetReady), targetDesired)},
		},
	}
	if counts.pending > 0 {
		path.Steps = append(path.Steps, ScalePathStep{Name: "Pending Pods", Summary: fmt.Sprintf("%d", counts.pending)})
	}

	addScalePathAssessment(path, hpa, input, counts, targetDesired)
	return path
}

type scalePathPodCounts struct {
	total         int32
	ready         int32
	pending       int32
	unschedulable int32
}

func (c scalePathPodCounts) readyOrTarget(targetReady int32) int32 {
	if c.total > 0 {
		return c.ready
	}
	return targetReady
}

func countScalePathPods(pods []ScalePathPod) scalePathPodCounts {
	var counts scalePathPodCounts
	for _, pod := range pods {
		counts.total++
		if pod.Ready {
			counts.ready++
		}
		if strings.EqualFold(pod.Phase, "Pending") {
			counts.pending++
			if pod.Unschedulable {
				counts.unschedulable++
			}
		}
	}
	return counts
}

func sumScalePathReplicaSets(replicaSets []ScalePathReplicaSet) (int32, int32) {
	var desired, current int32
	for _, rs := range replicaSets {
		desired += rs.DesiredReplicas
		current += rs.CurrentReplicas
	}
	return desired, current
}

func addScalePathAssessment(path *ScalePath, hpa *autoscalingv2.HorizontalPodAutoscaler, input ScalePathInput, counts scalePathPodCounts, desired int32) {
	schedulingEvent := firstSchedulingEvent(input.Events)
	reason := firstUnschedulableReason(input.Pods)

	if counts.pending > 0 {
		path.Evidence = append(path.Evidence, fmt.Sprintf("%d pods are Pending", counts.pending))
		if reason != "" {
			path.Evidence = append(path.Evidence, reason)
		}
		if schedulingEvent != "" {
			path.Evidence = append(path.Evidence, fmt.Sprintf("recent event: %s", schedulingEvent))
		}
		if desired >= hpa.Spec.MaxReplicas {
			path.Evidence = append(path.Evidence, "maxReplicas is not the current blocker")
		}
	}

	switch {
	case counts.unschedulable > 0 || (counts.pending > 0 && schedulingEvent != ""):
		path.BlockingPoint = fmt.Sprintf("Scheduler cannot place %d pods", counts.pending)
		path.NextActions = append(path.NextActions,
			"Check node capacity or Cluster Autoscaler/Karpenter",
			"Check pod requests, node selectors, affinity, taints, and namespace quotas",
		)
		if desired >= hpa.Spec.MaxReplicas {
			path.NextActions = append(path.NextActions, "Consider raising node group limit before raising maxReplicas")
		}
	case counts.pending > 0:
		path.BlockingPoint = fmt.Sprintf("%d pods are still Pending", counts.pending)
		path.NextActions = append(path.NextActions,
			"Describe pending pods and inspect scheduling, image pull, and admission events",
			"Check rollout status for the scale target",
		)
	case counts.total > 0 && counts.ready < desired:
		path.BlockingPoint = fmt.Sprintf("Pods are created but only %d of %d are Ready", counts.ready, desired)
		path.Evidence = append(path.Evidence, fmt.Sprintf("%d pods are not Ready", desired-counts.ready))
		path.NextActions = append(path.NextActions,
			"Check pod readiness probes, container restarts, and application startup latency",
			"Inspect recent pod events for readiness or image pull failures",
		)
	case hpa.Status.DesiredReplicas >= hpa.Spec.MaxReplicas && hpa.Status.CurrentReplicas >= hpa.Spec.MaxReplicas:
		path.BlockingPoint = "HPA is capped by maxReplicas"
		path.Evidence = append(path.Evidence, fmt.Sprintf("desiredReplicas equals maxReplicas=%d", hpa.Spec.MaxReplicas))
		path.NextActions = append(path.NextActions, "Review whether maxReplicas should be raised after capacity is confirmed")
	default:
		path.BlockingPoint = "No blocking point visible from current Kubernetes status"
		path.Evidence = append(path.Evidence, "HPA, scale target, and pod status do not expose an active scale-path blocker")
		path.NextActions = append(path.NextActions, "Check workload logs and metrics if user-visible capacity is still insufficient")
	}
}

func firstUnschedulableReason(pods []ScalePathPod) string {
	for _, pod := range pods {
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

func firstSchedulingEvent(events []Event) string {
	for _, event := range events {
		if event.Reason == "FailedScheduling" || strings.Contains(strings.ToLower(event.Message), "nodes available") {
			return event.Message
		}
	}
	return ""
}
