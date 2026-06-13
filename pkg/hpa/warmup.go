package hpa

import (
	"fmt"
	"math"
	"sort"
)

// AnalyzeWarmup evaluates warmup state after HPA scale-out. It classifies
// bottlenecks, estimates time-to-ready, and produces actionable recommendations.
// Returns nil when all pods are ready or there is insufficient data.
// This is a pure function with no Kubernetes API dependencies.
func AnalyzeWarmup(input WarmupInput) *WarmupAnalysis {
	if !isWarmingUp(input) {
		return nil
	}

	ratio := effectiveCapacityRatio(input.ReadyPods, input.DesiredReplicas)
	avg, p95, maxVal := computeTimeToReady(input.PodDetails)
	bottlenecks := classifyBottlenecks(input)
	evidence := buildEvidence(input, bottlenecks, avg, p95)
	impact := buildImpact(input, ratio)
	actions := buildRecommendedActions(bottlenecks, input)

	summary := "capacity_warming_up"
	if ratio >= 1.0 {
		summary = "capacity_ready"
	}

	return &WarmupAnalysis{
		Summary:                summary,
		EffectiveCapacityRatio: ratio,
		DesiredReplicas:        input.DesiredReplicas,
		CurrentReplicas:        input.CurrentReplicas,
		ReadyPods:              input.ReadyPods,
		AvailablePods:          input.TargetAvailableReplicas,
		AvgTimeToReadySeconds:  avg,
		P95TimeToReadySeconds:  p95,
		MaxTimeToReadySeconds:  maxVal,
		Bottlenecks:            bottlenecks,
		Evidence:               evidence,
		Impact:                 impact,
		RecommendedActions:     actions,
		PodDetails:             input.PodDetails,
	}
}

// isWarmingUp returns true when ReadyPods < DesiredReplicas and
// DesiredReplicas > MinReplicas (i.e., the HPA has scaled out).
func isWarmingUp(input WarmupInput) bool {
	if input.DesiredReplicas <= input.MinReplicas {
		return false
	}
	if input.ReadyPods >= input.DesiredReplicas {
		return false
	}
	return input.TotalPods > 0
}

// effectiveCapacityRatio returns the ratio of ready pods to desired replicas,
// clamped to [0.0, 1.0].
func effectiveCapacityRatio(readyPods, desiredReplicas int32) float64 {
	if desiredReplicas <= 0 {
		return 1.0
	}
	ratio := float64(readyPods) / float64(desiredReplicas)
	return math.Min(ratio, 1.0)
}

// computeTimeToReady calculates avg, p95, and max time-to-ready from pods
// that have become Ready. Returns 0, 0, 0 if no pods have ready times.
func computeTimeToReady(details []WarmupPodDetail) (avg, p95, maxVal int64) {
	var times []int64
	for _, d := range details {
		if d.Ready && d.TimeToReadySeconds > 0 {
			times = append(times, d.TimeToReadySeconds)
		}
	}
	if len(times) == 0 {
		return 0, 0, 0
	}

	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })

	var sum int64
	for _, t := range times {
		sum += t
		if t > maxVal {
			maxVal = t
		}
	}
	avg = sum / int64(len(times))

	p95Idx := int(math.Ceil(float64(len(times))*0.95)) - 1
	if p95Idx < 0 {
		p95Idx = 0
	}
	if p95Idx >= len(times) {
		p95Idx = len(times) - 1
	}
	p95 = times[p95Idx]

	return avg, p95, maxVal
}

// classifyBottlenecks runs classification rules against the input and returns
// detected bottlenecks sorted by severity (critical first).
func classifyBottlenecks(input WarmupInput) []WarmupBottleneck {
	var bottlenecks []WarmupBottleneck

	// Metrics inactive is the most critical issue.
	if !input.ScalingActive {
		bottlenecks = append(bottlenecks, WarmupBottleneck{
			Type:       "metrics_inactive",
			Severity:   SeverityError,
			Confidence: ConfidenceHigh,
			Message:    "HPA ScalingActive is false; metrics pipeline may be down",
		})
	}

	// Count pods by state for classification.
	var (
		runningNotReady int32
		imagePull       int32
		scheduling      int32
		crashLoop       int32
		unknown         int32
	)

	for _, pod := range input.PodDetails {
		if pod.Ready {
			continue
		}

		switch {
		case pod.WaitingReason == "ImagePullBackOff" || pod.WaitingReason == "ErrImagePull":
			imagePull++
		case pod.WaitingReason == "CrashLoopBackOff":
			crashLoop++
		case pod.ContainerState == "" && !pod.Ready:
			// No container state means pod is Pending (not scheduled yet).
			scheduling++
		case pod.ContainerState == "running" && !pod.Ready:
			runningNotReady++
		case pod.ContainerState == "terminated" || pod.RestartCount > 3:
			crashLoop++
		default:
			unknown++
		}
	}

	// Readiness probe bottleneck: Running but not Ready with probe present.
	if runningNotReady > 0 && input.ReadinessProbePresent {
		bottlenecks = append(bottlenecks, WarmupBottleneck{
			Type:       "readiness_probe",
			Severity:   SeverityWarning,
			Confidence: ConfidenceHigh,
			Count:      runningNotReady,
			Message:    fmt.Sprintf("%d pods are Running but not Ready (readinessProbe failing)", runningNotReady),
		})
	} else if runningNotReady > 0 {
		bottlenecks = append(bottlenecks, WarmupBottleneck{
			Type:       "unknown",
			Severity:   SeverityWarning,
			Confidence: ConfidenceMedium,
			Count:      runningNotReady,
			Message:    fmt.Sprintf("%d pods are Running but not Ready (no readinessProbe configured)", runningNotReady),
		})
	}

	// Startup probe bottleneck: pods young enough to still be in startup phase.
	if input.StartupProbePresent && runningNotReady > 0 {
		var startupBlocked int32
		for _, pod := range input.PodDetails {
			if !pod.Ready && pod.ContainerState == "running" && pod.AgeSeconds < int64(input.StartupProbeMaxDelaySeconds) {
				startupBlocked++
			}
		}
		if startupBlocked > 0 {
			bottlenecks = append(bottlenecks, WarmupBottleneck{
				Type:       "startup_probe",
				Severity:   SeverityInfo,
				Confidence: ConfidenceMedium,
				Count:      startupBlocked,
				Message:    fmt.Sprintf("%d pods may still be in startupProbe phase", startupBlocked),
			})
		}
	}

	// Image pull bottleneck.
	if imagePull > 0 {
		bottlenecks = append(bottlenecks, WarmupBottleneck{
			Type:       "image_pull",
			Severity:   SeverityError,
			Confidence: ConfidenceHigh,
			Count:      imagePull,
			Message:    fmt.Sprintf("%d pods have image pull issues (ImagePullBackOff/ErrImagePull)", imagePull),
		})
	}

	// Scheduling bottleneck.
	if scheduling > 0 {
		bottlenecks = append(bottlenecks, WarmupBottleneck{
			Type:       "scheduling",
			Severity:   SeverityWarning,
			Confidence: ConfidenceHigh,
			Count:      scheduling,
			Message:    fmt.Sprintf("%d pods are Pending (not yet scheduled)", scheduling),
		})
	}

	// Container crash bottleneck.
	if crashLoop > 0 {
		bottlenecks = append(bottlenecks, WarmupBottleneck{
			Type:       "container_crash",
			Severity:   SeverityError,
			Confidence: ConfidenceHigh,
			Count:      crashLoop,
			Message:    fmt.Sprintf("%d pods are crash-looping (CrashLoopBackOff or high restart count)", crashLoop),
		})
	}

	// Unknown bottleneck: pods not ready with no clear reason.
	if unknown > 0 {
		bottlenecks = append(bottlenecks, WarmupBottleneck{
			Type:       "unknown",
			Severity:   SeverityInfo,
			Confidence: ConfidenceLow,
			Count:      unknown,
			Message:    fmt.Sprintf("%d pods are not Ready for unknown reasons", unknown),
		})
	}

	return bottlenecks
}

// buildEvidence creates evidence lines from the analysis data.
func buildEvidence(input WarmupInput, bottlenecks []WarmupBottleneck, avg, p95 int64) []string {
	var evidence []string

	// Time-to-ready stats.
	if avg > 0 {
		evidence = append(evidence, fmt.Sprintf("avg time-to-ready: %ds", avg))
	}
	if p95 > 0 {
		evidence = append(evidence, fmt.Sprintf("p95 time-to-ready: %ds", p95))
	}

	// Bottleneck-specific evidence.
	for _, b := range bottlenecks {
		switch b.Type {
		case "readiness_probe":
			evidence = append(evidence, fmt.Sprintf("%d pods are NotReady due to readiness probe failures", b.Count))
		case "image_pull":
			evidence = append(evidence, fmt.Sprintf("%d pod(s) waiting: ImagePullBackOff or ErrImagePull", b.Count))
		case "scheduling":
			evidence = append(evidence, fmt.Sprintf("%d pod(s) are Pending (unscheduled)", b.Count))
		case "container_crash":
			evidence = append(evidence, fmt.Sprintf("%d pod(s) are crash-looping", b.Count))
		case "startup_probe":
			evidence = append(evidence, fmt.Sprintf("%d pod(s) may still be in startupProbe phase (age < %ds)", b.Count, input.StartupProbeMaxDelaySeconds))
		}
	}

	// Unhealthy events evidence.
	for _, evt := range input.UnhealthyEvents {
		if evt.Count > 0 {
			evidence = append(evidence, fmt.Sprintf("event %s seen %d times", evt.Reason, evt.Count))
		}
	}

	return evidence
}

// buildImpact creates a human-readable impact description.
func buildImpact(input WarmupInput, ratio float64) string {
	pct := int(math.Round(ratio * 100))
	notReady := input.DesiredReplicas - input.ReadyPods

	return fmt.Sprintf(
		"HPA has already requested %d replicas, but effective capacity is only %d%% (%d of %d pods ready, %d still warming up).",
		input.DesiredReplicas, pct, input.ReadyPods, input.DesiredReplicas, notReady,
	)
}

// buildRecommendedActions generates actionable recommendations based on bottlenecks.
func buildRecommendedActions(bottlenecks []WarmupBottleneck, input WarmupInput) []string {
	seen := make(map[string]bool)
	var actions []string

	for _, b := range bottlenecks {
		switch b.Type {
		case "readiness_probe":
			if !seen["probe"] {
				actions = append(actions, "Check readinessProbe and startupProbe configuration (initialDelaySeconds, periodSeconds, failureThreshold)")
				seen["probe"] = true
			}
		case "image_pull":
			if !seen["image"] {
				actions = append(actions, "Check image pull latency and registry access (ImagePullBackOff indicates pull failures)")
				seen["image"] = true
			}
		case "scheduling":
			if !seen["schedule"] {
				actions = append(actions, "Check node capacity and scheduling constraints (Pending pods indicate resource pressure)")
				seen["schedule"] = true
			}
		case "container_crash":
			if !seen["crash"] {
				actions = append(actions, "Check container logs for crash reasons (CrashLoopBackOff indicates application errors)")
				seen["crash"] = true
			}
		case "startup_probe":
			if !seen["startup"] {
				actions = append(actions, "Consider increasing startupProbe failureThreshold or periodSeconds")
				seen["startup"] = true
			}
		case "metrics_inactive":
			if !seen["metrics"] {
				actions = append(actions, "Check metrics server and HPA metrics pipeline availability")
				seen["metrics"] = true
			}
		}
	}

	// General recommendation when warming up.
	if input.ReadinessProbePresent && !seen["prewarm"] {
		actions = append(actions, "Consider pre-warming or raising minReplicas to reduce cold-start impact")
		seen["prewarm"] = true
	}
	if input.ScalingLimited && !seen["scale_limit"] {
		actions = append(actions, "HPA is at maxReplicas; consider raising maxReplicas if demand requires it")
		seen["scale_limit"] = true
	}

	return actions
}
