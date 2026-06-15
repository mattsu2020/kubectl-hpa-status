package hpa

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

func appendScalePathText(out *[]byte, path *ScalePath, theme style.Theme) {
	*out = append(*out, "Scale Path:\n"...)
	for _, step := range path.Steps {
		*out = fmt.Appendf(*out, "  %s: %s\n", step.Name, step.Summary)
	}
	appendScalePathBlockingPoint(out, path, theme)
	appendScalePathEvidence(out, path)
	appendScalePathProbeWarnings(out, path, theme)
	appendScalePathSchedulerInfo(out, path, theme)
	appendScalePathQuotaChecks(out, path)
	appendScalePathAutoscalerEvents(out, path)
	appendScalePathNextActions(out, path, theme)
}

// appendScalePathBlockingPoint appends the blocking point section when present.
func appendScalePathBlockingPoint(out *[]byte, path *ScalePath, theme style.Theme) {
	if path.BlockingPoint == "" {
		return
	}
	*out = append(*out, "\nBlocking point:\n"...)
	*out = fmt.Appendf(*out, "  %s\n", theme.InterpretationLine(path.BlockingPoint))
}

// appendScalePathEvidence appends the evidence section when present.
func appendScalePathEvidence(out *[]byte, path *ScalePath) {
	if len(path.Evidence) == 0 {
		return
	}
	*out = append(*out, "\nEvidence:\n"...)
	for _, evidence := range path.Evidence {
		*out = fmt.Appendf(*out, "  - %s\n", evidence)
	}
}

// appendScalePathProbeWarnings appends the probe warnings section when present.
func appendScalePathProbeWarnings(out *[]byte, path *ScalePath, theme style.Theme) {
	if len(path.ProbeWarnings) == 0 {
		return
	}
	*out = append(*out, "\nProbe warnings:\n"...)
	for _, warning := range path.ProbeWarnings {
		*out = fmt.Appendf(*out, "  - %s\n", theme.InterpretationLine(warning))
	}
}

// appendScalePathSchedulerInfo appends the scheduler constraints section when present.
func appendScalePathSchedulerInfo(out *[]byte, path *ScalePath, theme style.Theme) {
	if path.SchedulerInfo == nil {
		return
	}
	*out = append(*out, "\nScheduler constraints:\n"...)
	if path.SchedulerInfo.NodeSelectorLabels > 0 {
		*out = fmt.Appendf(*out, "  nodeSelector: %d labels\n", path.SchedulerInfo.NodeSelectorLabels)
	}
	for _, ac := range path.SchedulerInfo.AffinityConstraints {
		*out = fmt.Appendf(*out, "  affinity: %s\n", ac)
	}
	for _, ts := range path.SchedulerInfo.TopologySpreadConstraints {
		*out = fmt.Appendf(*out, "  topologySpread: %s\n", ts)
	}
	if path.SchedulerInfo.Warning != "" {
		*out = fmt.Appendf(*out, "  %s\n", theme.InterpretationLine(path.SchedulerInfo.Warning))
	}
}

// appendScalePathQuotaChecks appends the quota checks section when present.
func appendScalePathQuotaChecks(out *[]byte, path *ScalePath) {
	if len(path.QuotaChecks) == 0 {
		return
	}
	*out = append(*out, "\nQuota checks:\n"...)
	for _, q := range path.QuotaChecks {
		status := "OK"
		if q.Blocking {
			status = "BLOCKING"
		}
		*out = fmt.Appendf(*out, "  [%s] %s: %s %s/%s\n", status, q.Name, q.Resource, q.Used, q.Hard)
	}
}

// appendScalePathAutoscalerEvents appends the autoscaler events section when present.
func appendScalePathAutoscalerEvents(out *[]byte, path *ScalePath) {
	if len(path.AutoscalerEvents) == 0 {
		return
	}
	*out = append(*out, "\nAutoscaler events:\n"...)
	for _, event := range path.AutoscalerEvents {
		*out = fmt.Appendf(*out, "  - %s\n", event)
	}
}

// appendScalePathNextActions appends the next actions section when present.
func appendScalePathNextActions(out *[]byte, path *ScalePath, theme style.Theme) {
	if len(path.NextActions) == 0 {
		return
	}
	*out = append(*out, "\nNext actions:\n"...)
	for _, action := range path.NextActions {
		*out = fmt.Appendf(*out, "  - %s\n", theme.ActionLine(action))
	}
}

// appendCapacityHeadroomText renders the capacity headroom section.
func appendCapacityHeadroomText(out *[]byte, headroom *CapacityHeadroom, theme style.Theme) {
	if headroom == nil {
		return
	}
	*out = append(*out, "Capacity headroom:\n"...)
	*out = fmt.Appendf(*out, "  HPA maxReplicas: %d\n", headroom.MaxReplicas)
	*out = fmt.Appendf(*out, "  current desired: %d\n", headroom.CurrentDesired)
	if headroom.PodRequestCPU != "" {
		*out = fmt.Appendf(*out, "  pod request cpu: %s\n", headroom.PodRequestCPU)
	}
	if headroom.PodRequestMemory != "" {
		*out = fmt.Appendf(*out, "  pod request memory: %s\n", headroom.PodRequestMemory)
	}
	if headroom.AdditionalCPUToMax != "" {
		*out = fmt.Appendf(*out, "  additional CPU needed to reach maxReplicas: %s\n", headroom.AdditionalCPUToMax)
	}
	if headroom.AdditionalMemoryToMax != "" {
		*out = fmt.Appendf(*out, "  additional memory needed to reach maxReplicas: %s\n", headroom.AdditionalMemoryToMax)
	}
	*out = fmt.Appendf(*out, "  cluster schedulable headroom: %s\n", headroom.ClusterSchedulableHeadroom)
	*out = fmt.Appendf(*out, "  risk: %s\n", theme.ActionLine(headroom.Risk))
	if len(headroom.Evidence) > 0 {
		*out = append(*out, "  evidence:\n"...)
		for _, evidence := range headroom.Evidence {
			*out = fmt.Appendf(*out, "    - %s\n", evidence)
		}
	}
}

// appendReadinessImpactText renders the readiness impact section.
func appendReadinessImpactText(out *[]byte, impact *ReadinessImpact, theme style.Theme) {
	if impact == nil {
		return
	}
	affected := "no"
	if impact.LikelyAffected {
		affected = "yes"
	}
	*out = append(*out, "Readiness Impact:\n"...)
	*out = fmt.Appendf(*out, "  likely affected: %s\n", affected)
	*out = fmt.Appendf(*out, "  pods: %d total, %d not-yet-ready, %d missing metrics\n",
		impact.TotalPods, impact.NotYetReadyPods, impact.MissingMetricPods)
	*out = fmt.Appendf(*out, "  controller defaults: initial-readiness-delay=%s cpu-initialization-period=%s\n",
		impact.InitialReadinessDelay, impact.CPUInitializationPeriod)
	if len(impact.PossibleEffects) > 0 {
		*out = append(*out, "  possible effect:\n"...)
		for _, effect := range impact.PossibleEffects {
			*out = fmt.Appendf(*out, "    - %s\n", theme.InterpretationLine(effect))
		}
	}
	if len(impact.Evidence) > 0 {
		*out = append(*out, "\nEvidence:\n"...)
		for _, evidence := range impact.Evidence {
			*out = fmt.Appendf(*out, "  - %s\n", evidence)
		}
	}
	if len(impact.NextChecks) > 0 {
		*out = append(*out, "\nNext checks:\n"...)
		for _, check := range impact.NextChecks {
			*out = fmt.Appendf(*out, "  %s\n", theme.ActionLine(check))
		}
	}
}

// appendRolloutDiagnosisText renders the rollout diagnosis section.
func appendRolloutDiagnosisText(out *[]byte, diagnosis *RolloutDiagnosis, theme style.Theme) {
	if diagnosis == nil {
		return
	}
	*out = fmt.Appendf(*out, "Rollout Impact: %s/%s\n", diagnosis.Kind, diagnosis.Name)
	*out = fmt.Appendf(*out, "  rollout in progress: %t\n", diagnosis.InProgress)
	*out = fmt.Appendf(*out, "  desired replicas: %d\n", diagnosis.DesiredReplicas)
	if diagnosis.Kind == "Deployment" {
		*out = fmt.Appendf(*out, "  updated replicas: %d\n", diagnosis.UpdatedReplicas)
	}
	*out = fmt.Appendf(*out, "  ready replicas: %d\n", diagnosis.ReadyReplicas)
	*out = fmt.Appendf(*out, "  available replicas: %d\n", diagnosis.AvailableReplicas)
	*out = fmt.Appendf(*out, "  unavailable replicas: %d\n", diagnosis.UnavailableReplicas)
	if diagnosis.Reason != "" {
		*out = fmt.Appendf(*out, "  reason: %s\n", theme.InterpretationLine(diagnosis.Reason))
	}
	for _, condition := range diagnosis.Conditions {
		*out = fmt.Appendf(*out, "  condition: %s\n", condition)
	}
	for _, issue := range diagnosis.PodIssues {
		*out = fmt.Appendf(*out, "  pod issue: %s\n", theme.ActionLine(issue))
	}
	if len(diagnosis.NextActions) > 0 {
		*out = append(*out, "  next actions:\n"...)
		for _, action := range diagnosis.NextActions {
			*out = fmt.Appendf(*out, "    - %s\n", action)
		}
	}
}

// appendScaleoutBlockersText renders the scale-out blockers section.
func appendScaleoutBlockersText(out *[]byte, report *BlockerReport, theme style.Theme) {
	if report == nil || len(report.Blockers) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = append(*out, "Scale-out blockers:\n"...)
	for i, blocker := range report.Blockers {
		if blocker.Severity == BlockerInfo && i > 2 {
			continue
		}
		*out = fmt.Appendf(*out, "  %d. %s\n", i+1, blocker.Message)
		if blocker.Detail != "" {
			*out = fmt.Appendf(*out, "     evidence: %s\n", blocker.Detail)
		}
		if blocker.NextCommand != "" {
			*out = fmt.Appendf(*out, "     next: %s\n", theme.ActionLine(blocker.NextCommand))
		}
	}
}

// appendControllerProfileText renders the controller profile section.
func appendControllerProfileText(out *[]byte, profile *ControllerProfile) {
	if profile == nil {
		return
	}
	*out = append(*out, "Controller profile:\n"...)
	*out = fmt.Appendf(*out, "  source: %s\n", profile.Source)
	*out = fmt.Appendf(*out, "  sync period: %s\n", profile.SyncPeriod)
	*out = fmt.Appendf(*out, "  downscale stabilization: %s\n", profile.DownscaleStabilization)
	*out = fmt.Appendf(*out, "  initial readiness delay: %s\n", profile.InitialReadinessDelay)
	*out = fmt.Appendf(*out, "  cpu initialization period: %s\n", profile.CPUInitializationPeriod)
	*out = fmt.Appendf(*out, "  tolerance: %s\n", profile.Tolerance)
	for _, warning := range profile.Warnings {
		*out = fmt.Appendf(*out, "  warning: %s\n", warning)
	}
}
