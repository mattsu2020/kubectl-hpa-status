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
	bottlenecks = append(bottlenecks, metricsInactiveBottleneck(input.ScalingActive)...)

	// Count pods by state for classification.
	counts := countPodStates(input.PodDetails)

	// Readiness probe bottleneck: Running but not Ready with probe present.
	bottlenecks = append(bottlenecks, readinessProbeBottlenecks(counts.runningNotReady, input.ReadinessProbePresent)...)

	// Startup probe bottleneck: pods young enough to still be in startup phase.
	bottlenecks = append(bottlenecks, startupProbeBottleneck(input, counts.runningNotReady)...)

	// Image pull bottleneck.
	bottlenecks = append(bottlenecks, imagePullBottleneck(counts.imagePull)...)

	// Scheduling bottleneck.
	bottlenecks = append(bottlenecks, schedulingBottleneck(counts.scheduling)...)

	// Container crash bottleneck.
	bottlenecks = append(bottlenecks, containerCrashBottleneck(counts.crashLoop)...)

	// Unknown bottleneck: pods not ready with no clear reason.
	bottlenecks = append(bottlenecks, unknownBottleneck(counts.unknown)...)

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

// warmupActionTemplates maps bottleneck types to (dedup key, recommendation).
// Order matters: first occurrence of a key wins, mirroring the original switch.
var warmupActionTemplates = []struct {
	bottleneckType string
	key            string
	message        string
}{
	{"readiness_probe", "probe", "Check readinessProbe and startupProbe configuration (initialDelaySeconds, periodSeconds, failureThreshold)"},
	{"image_pull", "image", "Check image pull latency and registry access (ImagePullBackOff indicates pull failures)"},
	{"scheduling", "schedule", "Check node capacity and scheduling constraints (Pending pods indicate resource pressure)"},
	{"container_crash", "crash", "Check container logs for crash reasons (CrashLoopBackOff indicates application errors)"},
	{"startup_probe", "startup", "Consider increasing startupProbe failureThreshold or periodSeconds"},
	{"metrics_inactive", "metrics", "Check metrics server and HPA metrics pipeline availability"},
}

// buildRecommendedActions generates actionable recommendations based on bottlenecks.
func buildRecommendedActions(bottlenecks []WarmupBottleneck, input WarmupInput) []string {
	seen := make(map[string]bool)
	var actions []string

	for _, b := range bottlenecks {
		for _, tmpl := range warmupActionTemplates {
			if b.Type == tmpl.bottleneckType && !seen[tmpl.key] {
				actions = append(actions, tmpl.message)
				seen[tmpl.key] = true
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
