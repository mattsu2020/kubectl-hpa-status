// Package warmup analyzes pod warmup bottlenecks (image pulls, startup
// probes, scheduling, container crashes) that delay an HPA scale-up from
// reaching its desired replica count. It is a self-contained leaf domain
// depending only on standard library, metav1 types, and the shared
// confidence enums. The cmd/ layer reaches it through the pkg/hpa
// re-export facade.
package warmup

import (
	"fmt"
	"math"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/confidence"
)

// Analysis holds the complete warmup analysis result for an HPA that
// recently scaled out but pods are not yet ready.
type Analysis struct {
	// Summary is the overall warmup state: "capacity_warming_up",
	// "capacity_ready", "insufficient_data".
	Summary string `json:"summary" yaml:"summary"`
	// EffectiveCapacityRatio is the ratio of ready pods to desired replicas (0.0-1.0).
	EffectiveCapacityRatio float64 `json:"effectiveCapacityRatio" yaml:"effectiveCapacityRatio"`
	// DesiredReplicas is the HPA desired replica count.
	DesiredReplicas int32 `json:"desiredReplicas" yaml:"desiredReplicas"`
	// CurrentReplicas is the HPA current replica count.
	CurrentReplicas int32 `json:"currentReplicas" yaml:"currentReplicas"`
	// ReadyPods is the count of pods in Ready state.
	ReadyPods int32 `json:"readyPods" yaml:"readyPods"`
	// AvailablePods is the count from the workload's availableReplicas status.
	AvailablePods int32 `json:"availablePods" yaml:"availablePods"`
	// AvgTimeToReadySeconds is the average time from pod creation to Ready condition.
	// Zero if no pods have become Ready yet.
	AvgTimeToReadySeconds int64 `json:"avgTimeToReadySeconds" yaml:"avgTimeToReadySeconds"`
	// P95TimeToReadySeconds is the p95 time from pod creation to Ready condition.
	P95TimeToReadySeconds int64 `json:"p95TimeToReadySeconds" yaml:"p95TimeToReadySeconds"`
	// MaxTimeToReadySeconds is the maximum observed time-to-ready.
	MaxTimeToReadySeconds int64 `json:"maxTimeToReadySeconds,omitempty" yaml:"maxTimeToReadySeconds,omitempty"`
	// Bottlenecks lists the detected warmup bottlenecks.
	Bottlenecks []Bottleneck `json:"bottlenecks" yaml:"bottlenecks"`
	// Evidence lists human-readable evidence lines.
	Evidence []string `json:"evidence" yaml:"evidence"`
	// Impact is a human-readable description of the current effective capacity.
	Impact string `json:"impact" yaml:"impact"`
	// RecommendedActions lists actionable suggestions.
	RecommendedActions []string `json:"recommendedActions" yaml:"recommendedActions"`
	// PodDetails holds per-pod warmup status for JSON/YAML consumers.
	PodDetails []PodDetail `json:"podDetails,omitempty" yaml:"podDetails,omitempty"`
}

// Bottleneck represents a single detected warmup bottleneck.
type Bottleneck struct {
	// Type classifies the bottleneck: "readiness_probe", "image_pull",
	// "scheduling", "startup_probe", "container_crash", "metrics_inactive", "unknown".
	Type string `json:"type" yaml:"type"`
	// Severity is the bottleneck severity.
	Severity confidence.Severity `json:"severity" yaml:"severity"`
	// Confidence is the analysis confidence.
	Confidence confidence.Confidence `json:"confidence" yaml:"confidence"`
	// Count is how many pods are affected by this bottleneck.
	Count int32 `json:"count" yaml:"count"`
	// Message is a human-readable description.
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// PodDetail holds per-pod warmup status for structured output.
type PodDetail struct {
	// Name is the pod name.
	Name string `json:"name" yaml:"name"`
	// AgeSeconds is the pod age in seconds.
	AgeSeconds int64 `json:"ageSeconds" yaml:"ageSeconds"`
	// Ready indicates whether the pod is Ready.
	Ready bool `json:"ready" yaml:"ready"`
	// ContainerState is the primary container state: "running", "waiting", "terminated".
	ContainerState string `json:"containerState,omitempty" yaml:"containerState,omitempty"`
	// WaitingReason is the container waiting reason (e.g., "ImagePullBackOff").
	WaitingReason string `json:"waitingReason,omitempty" yaml:"waitingReason,omitempty"`
	// RestartCount is the number of container restarts.
	RestartCount int32 `json:"restartCount" yaml:"restartCount"`
	// TimeToReadySeconds is the observed time-to-Ready, or 0 if not ready yet.
	TimeToReadySeconds int64 `json:"timeToReadySeconds,omitempty" yaml:"timeToReadySeconds,omitempty"`
}

// Input aggregates all observable signals for warmup analysis.
// The cmd layer assembles this from multiple kube fetchers, keeping the core
// analysis in pkg/hpa free of Kubernetes API dependencies.
type Input struct {
	// Namespace is the Kubernetes namespace.
	Namespace string
	// DesiredReplicas is the HPA desired replica count.
	DesiredReplicas int32
	// CurrentReplicas is the HPA current replica count.
	CurrentReplicas int32
	// MinReplicas is the HPA minimum replica count.
	MinReplicas int32
	// MaxReplicas is the HPA maximum replica count.
	MaxReplicas int32
	// ScalingActive indicates whether the HPA ScalingActive condition is True.
	ScalingActive bool
	// ScalingLimited indicates whether the HPA is capped by min/max.
	ScalingLimited bool
	// TargetReadyReplicas is the ready replica count from the scale target.
	TargetReadyReplicas int32
	// TargetAvailableReplicas is the available replica count from the scale target.
	TargetAvailableReplicas int32
	// TargetDesiredReplicas is the desired replica count from the scale target.
	TargetDesiredReplicas int32
	// TotalPods is the total number of pods for the scale target.
	TotalPods int32
	// ReadyPods is the count of pods in Running/Ready state.
	ReadyPods int32
	// PodDetails holds per-pod warmup status information.
	PodDetails []PodDetail
	// UnhealthyEvents lists pod-level events with reasons indicating warmup issues.
	UnhealthyEvents []EventInfo
	// ReadinessProbePresent indicates if the pod template has a readinessProbe.
	ReadinessProbePresent bool
	// StartupProbePresent indicates if the pod template has a startupProbe.
	StartupProbePresent bool
	// ReadinessProbeMaxDelaySeconds is the maximum readiness probe delay.
	ReadinessProbeMaxDelaySeconds int32
	// StartupProbeMaxDelaySeconds is the maximum startup probe delay.
	StartupProbeMaxDelaySeconds int32
	// Now is the current time, used for age calculations.
	Now metav1.Time
}

// EventInfo holds a pod-level event relevant to warmup analysis.
type EventInfo struct {
	// Reason is the event reason (e.g., "Unhealthy", "FailedScheduling",
	// "BackOff", "ImagePullBackOff").
	Reason string `json:"reason" yaml:"reason"`
	// Count is the number of times this event occurred.
	Count int32 `json:"count" yaml:"count"`
}

// AnalyzeWarmup evaluates pod warmup bottlenecks for the given input.
// Returns nil when all pods are ready or there is insufficient data.
// This is a pure function with no Kubernetes API dependencies.
func AnalyzeWarmup(input Input) *Analysis {
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

	return &Analysis{
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
func isWarmingUp(input Input) bool {
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
func computeTimeToReady(details []PodDetail) (avg, p95, maxVal int64) {
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
func classifyBottlenecks(input Input) []Bottleneck {
	var bottlenecks []Bottleneck

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
func buildEvidence(input Input, bottlenecks []Bottleneck, avg, p95 int64) []string {
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
func buildImpact(input Input, ratio float64) string {
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
func buildRecommendedActions(bottlenecks []Bottleneck, input Input) []string {
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

// podStateCounts tallies not-ready pods by their failure category.
// Ready pods are skipped entirely.
type podStateCounts struct {
	runningNotReady int32
	imagePull       int32
	scheduling      int32
	crashLoop       int32
	unknown         int32
}

// countPodStates inspects each pod detail and increments the matching
// failure category for pods that are not yet Ready.
func countPodStates(details []PodDetail) podStateCounts {
	var c podStateCounts
	for _, pod := range details {
		if pod.Ready {
			continue
		}
		switch {
		case pod.WaitingReason == "ImagePullBackOff" || pod.WaitingReason == "ErrImagePull":
			c.imagePull++
		case pod.WaitingReason == "CrashLoopBackOff":
			c.crashLoop++
		case pod.ContainerState == "" && !pod.Ready:
			// No container state means pod is Pending (not scheduled yet).
			c.scheduling++
		case pod.ContainerState == "running" && !pod.Ready:
			c.runningNotReady++
		case pod.ContainerState == "terminated" || pod.RestartCount > 3:
			c.crashLoop++
		default:
			c.unknown++
		}
	}
	return c
}

// readinessProbeBottlenecks emits the readiness/unknown bottleneck for pods
// that are Running but not Ready. When a readinessProbe is present the failure
// is attributed to the probe; otherwise it is reported as unknown.
func readinessProbeBottlenecks(runningNotReady int32, probePresent bool) []Bottleneck {
	if runningNotReady <= 0 {
		return nil
	}
	if probePresent {
		return []Bottleneck{{
			Type:       "readiness_probe",
			Severity:   confidence.Warning,
			Confidence: confidence.High,
			Count:      runningNotReady,
			Message:    fmt.Sprintf("%d pods are Running but not Ready (readinessProbe failing)", runningNotReady),
		}}
	}
	return []Bottleneck{{
		Type:       "unknown",
		Severity:   confidence.Warning,
		Confidence: confidence.Medium,
		Count:      runningNotReady,
		Message:    fmt.Sprintf("%d pods are Running but not Ready (no readinessProbe configured)", runningNotReady),
	}}
}

// startupProbeBottleneck emits a startupProbe bottleneck when young running
// pods are likely still inside the startupProbe phase. Returns nil otherwise.
func startupProbeBottleneck(input Input, runningNotReady int32) []Bottleneck {
	if !input.StartupProbePresent || runningNotReady <= 0 {
		return nil
	}
	var startupBlocked int32
	for _, pod := range input.PodDetails {
		if !pod.Ready && pod.ContainerState == "running" && pod.AgeSeconds < int64(input.StartupProbeMaxDelaySeconds) {
			startupBlocked++
		}
	}
	if startupBlocked <= 0 {
		return nil
	}
	return []Bottleneck{{
		Type:       "startup_probe",
		Severity:   confidence.Info,
		Confidence: confidence.Medium,
		Count:      startupBlocked,
		Message:    fmt.Sprintf("%d pods may still be in startupProbe phase", startupBlocked),
	}}
}

// imagePullBottleneck emits an image_pull bottleneck when any pod is in
// ImagePullBackOff or ErrImagePull.
func imagePullBottleneck(count int32) []Bottleneck {
	if count <= 0 {
		return nil
	}
	return []Bottleneck{{
		Type:       "image_pull",
		Severity:   confidence.Error,
		Confidence: confidence.High,
		Count:      count,
		Message:    fmt.Sprintf("%d pods have image pull issues (ImagePullBackOff/ErrImagePull)", count),
	}}
}

// schedulingBottleneck emits a scheduling bottleneck for Pending pods that
// have not been scheduled yet.
func schedulingBottleneck(count int32) []Bottleneck {
	if count <= 0 {
		return nil
	}
	return []Bottleneck{{
		Type:       "scheduling",
		Severity:   confidence.Warning,
		Confidence: confidence.High,
		Count:      count,
		Message:    fmt.Sprintf("%d pods are Pending (not yet scheduled)", count),
	}}
}

// containerCrashBottleneck emits a container_crash bottleneck for crash-looping
// pods (CrashLoopBackOff or high restart count).
func containerCrashBottleneck(count int32) []Bottleneck {
	if count <= 0 {
		return nil
	}
	return []Bottleneck{{
		Type:       "container_crash",
		Severity:   confidence.Error,
		Confidence: confidence.High,
		Count:      count,
		Message:    fmt.Sprintf("%d pods are crash-looping (CrashLoopBackOff or high restart count)", count),
	}}
}

// unknownBottleneck emits an unknown bottleneck for not-ready pods that do not
// match any known failure category.
func unknownBottleneck(count int32) []Bottleneck {
	if count <= 0 {
		return nil
	}
	return []Bottleneck{{
		Type:       "unknown",
		Severity:   confidence.Info,
		Confidence: confidence.Low,
		Count:      count,
		Message:    fmt.Sprintf("%d pods are not Ready for unknown reasons", count),
	}}
}

// metricsInactiveBottleneck emits the metrics_inactive bottleneck when the HPA
// ScalingActive condition is false.
func metricsInactiveBottleneck(scalingActive bool) []Bottleneck {
	if scalingActive {
		return nil
	}
	return []Bottleneck{{
		Type:       "metrics_inactive",
		Severity:   confidence.Error,
		Confidence: confidence.High,
		Message:    "HPA ScalingActive is false; metrics pipeline may be down",
	}}
}
