package retrospective

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// ReplayAnalysis holds the result of replay analysis on a Timeline.
type ReplayAnalysis struct {
	// Bottlenecks lists detected scaling bottlenecks with timestamps and severity.
	Bottlenecks []BottleneckMarker
	// ControlCycles lists detected HPA control cycles with input/output replicas.
	ControlCycles []ControlCycle
	// StabilizationWindows lists periods where scale-down was suppressed.
	StabilizationWindows []StabilizationWindow
	// ReplayToleranceEffects lists metrics whose scaling was suppressed by tolerance.
	ReplayToleranceEffects []ReplayToleranceEffect
	// Summary is a human-readable summary of the analysis.
	Summary string
}

// BottleneckMarker represents a single scaling bottleneck event.
type BottleneckMarker struct {
	// Timestamp is when the bottleneck was detected.
	Timestamp time.Time
	// Type is the bottleneck category: "scheduling", "quota", "metrics", "policy".
	Type string
	// Message is a human-readable description.
	Message string
	// Severity is "high", "medium", or "low".
	Severity string
	// Duration is how long the bottleneck persisted.
	Duration time.Duration
}

// ControlCycle represents a single HPA control cycle decision.
type ControlCycle struct {
	// Start is when the control cycle began.
	Start time.Time
	// End is when the control cycle completed.
	End time.Time
	// InputReplicas is the replica count at cycle start.
	InputReplicas int32
	// OutputReplicas is the replica count at cycle end.
	OutputReplicas int32
	// Decision is the scaling decision: "scale-up", "scale-down", "no-change", "capped".
	Decision string
	// MetricDriver is the metric that drove the decision (estimated).
	MetricDriver string
}

// StabilizationWindow represents a period where scale-down was suppressed.
type StabilizationWindow struct {
	// Start is when the stabilization window began.
	Start time.Time
	// End is when the stabilization window ended.
	End time.Time
	// Duration is the length of the stabilization window.
	Duration time.Duration
	// SuppressedScaleDown is the estimated number of scale-down replicas suppressed.
	SuppressedScaleDown int32
}

// ReplayToleranceEffect represents a metric whose scaling was suppressed by tolerance.
type ReplayToleranceEffect struct {
	// Timestamp is when the tolerance effect was detected.
	Timestamp time.Time
	// MetricName is the name of the metric (e.g. "cpu", "memory").
	MetricName string
	// ActualRatio is the actual metric ratio.
	ActualRatio float64
	// Tolerance is the configured tolerance threshold.
	Tolerance float64
	// Suppressed indicates whether scaling was suppressed by tolerance.
	Suppressed bool
}

// AnalyzeReplay performs deep analysis on a Timeline to extract
// bottleneck markers, control cycles, stabilization windows, and tolerance effects.
func AnalyzeReplay(tl Timeline, hpa *autoscalingv2.HorizontalPodAutoscaler) *ReplayAnalysis {
	analysis := &ReplayAnalysis{
		Bottlenecks:            []BottleneckMarker{},
		ControlCycles:          []ControlCycle{},
		StabilizationWindows:   []StabilizationWindow{},
		ReplayToleranceEffects: []ReplayToleranceEffect{},
	}

	if len(tl.Entries) == 0 {
		analysis.Summary = "No timeline entries to analyze."
		return analysis
	}

	// Track control cycle state.
	var lastRescaleTime time.Time
	var lastRescaleTo int32

	// Track stabilization window state.
	var stabilizationStart time.Time
	var stabilizedReplicas int32

	for i, entry := range tl.Entries {
		switch entry.Category {
		case "rescale":
			lastRescaleTime, lastRescaleTo = analyzeRescaleEntry(
				analysis, tl, hpa, i, entry, lastRescaleTime, lastRescaleTo)
		case "metrics-unavailable":
			analysis.Bottlenecks = append(analysis.Bottlenecks, bottleneckUntilNext(tl, i, entry,
				"metrics", "Metrics unavailable - HPA cannot compute desired replicas", "high"))
		case "scaling-limited":
			analysis.Bottlenecks = append(analysis.Bottlenecks, bottleneckUntilNext(tl, i, entry,
				"policy", fmt.Sprintf("ScalingLimited=True - capped by maxReplicas=%d", hpa.Spec.MaxReplicas), "medium"))
		case "stabilized":
			stabilizationStart, stabilizedReplicas, lastRescaleTo = analyzeStabilizedEntry(
				analysis, tl, i, entry, stabilizationStart, stabilizedReplicas, lastRescaleTo)
		case "metric-change":
			analyzeMetricChangeEntry(analysis, hpa, entry)
		}
	}

	// Build summary.
	analysis.Summary = fmt.Sprintf("Detected %d bottlenecks, %d stabilization windows, %d control cycles",
		len(analysis.Bottlenecks), len(analysis.StabilizationWindows), len(analysis.ControlCycles))

	return analysis
}

// analyzeRescaleEntry extracts a control cycle from a rescale entry, detects maxReplicas capping,
// and records any stabilization-suppressed scale-down. Returns updated rescale-tracking state.
func analyzeRescaleEntry(analysis *ReplayAnalysis, tl Timeline, hpa *autoscalingv2.HorizontalPodAutoscaler, i int, entry Entry, lastRescaleTime time.Time, _ int32) (time.Time, int32) {
	from, to := extractRescaleReplicas(entry.Message)
	decision, metricDriver := classifyRescaleDecision(entry.Message, from, to)
	cycle := buildRescaleControlCycle(entry, tl, lastRescaleTime, from, to, decision, metricDriver)

	if decision == "scale-up" && to >= hpa.Spec.MaxReplicas {
		cycle.Decision = "capped"
		analysis.Bottlenecks = append(analysis.Bottlenecks, BottleneckMarker{
			Timestamp: entry.Timestamp,
			Type:      "policy",
			Message:   fmt.Sprintf("maxReplicas capped scale-up at %d replicas", hpa.Spec.MaxReplicas),
			Severity:  "medium",
			Duration:  0,
		})
	}

	analysis.ControlCycles = append(analysis.ControlCycles, cycle)
	newLastRescaleTime := entry.Timestamp
	newLastRescaleTo := to

	recordStabilizationSuppressedScaleDown(analysis, tl, hpa, i, entry, decision, newLastRescaleTo, to)
	return newLastRescaleTime, newLastRescaleTo
}

// extractRescaleReplicas parses the from/to replica counts from a rescale message,
// falling back to the "desired A -> B" regex format when parseDesiredRange yields zeros.
func extractRescaleReplicas(message string) (int32, int32) {
	from, to := parseDesiredRange(message)
	if from != 0 && to != 0 {
		return from, to
	}
	match := desiredRangeRegex.FindStringSubmatch(message)
	if len(match) < 3 {
		return from, to
	}
	if _, err := fmt.Sscanf(match[1], "%d", &from); err != nil {
		from = 0
	}
	if _, err := fmt.Sscanf(match[2], "%d", &to); err != nil {
		to = 0
	}
	return from, to
}

// classifyRescaleDecision determines the scale direction and metric driver from a rescale message.
func classifyRescaleDecision(message string, from, to int32) (decision, metricDriver string) {
	decision = "no-change"
	metricDriver = "unknown"
	if from > 0 && to > 0 {
		if to > from {
			decision = "scale-up"
		} else if to < from {
			decision = "scale-down"
		}
	}

	upper := strings.ToUpper(message)
	switch {
	case strings.Contains(upper, "CPU"):
		metricDriver = "cpu"
	case strings.Contains(upper, "MEMORY"):
		metricDriver = "memory"
	}
	return decision, metricDriver
}

// buildRescaleControlCycle assembles a ControlCycle, deriving its start from the previous rescale (or timeline start).
func buildRescaleControlCycle(entry Entry, tl Timeline, lastRescaleTime time.Time, from, to int32, decision, metricDriver string) ControlCycle {
	cycleStart := lastRescaleTime
	if lastRescaleTime.IsZero() {
		cycleStart = tl.Since
	}
	return ControlCycle{
		Start:          cycleStart,
		End:            entry.Timestamp,
		InputReplicas:  from,
		OutputReplicas: to,
		Decision:       decision,
		MetricDriver:   metricDriver,
	}
}

// recordStabilizationSuppressedScaleDown records a stabilization window when a scale-down is delayed beyond 60s
// and the HPA has a non-zero scale-down stabilization window configured.
func recordStabilizationSuppressedScaleDown(analysis *ReplayAnalysis, tl Timeline, hpa *autoscalingv2.HorizontalPodAutoscaler, i int, entry Entry, decision string, lastRescaleTo, to int32) {
	if i == 0 {
		return
	}
	prevEntry := tl.Entries[i-1]
	gap := entry.Timestamp.Sub(prevEntry.Timestamp)
	if gap <= 60*time.Second || decision != "scale-down" {
		return
	}
	stabWindow := scaleDownStabilizationWindowSeconds(hpa)
	if stabWindow <= 0 {
		return
	}
	suppressed := int32(0)
	if lastRescaleTo > to {
		suppressed = lastRescaleTo - to
	}
	analysis.StabilizationWindows = append(analysis.StabilizationWindows, StabilizationWindow{
		Start:               prevEntry.Timestamp,
		End:                 entry.Timestamp,
		Duration:            gap,
		SuppressedScaleDown: suppressed,
	})
}

// bottleneckUntilNext builds a bottleneck marker whose duration spans from the current entry to the next entry (or zero if last).
func bottleneckUntilNext(tl Timeline, i int, entry Entry, bType, message, severity string) BottleneckMarker {
	duration := time.Duration(0)
	if i+1 < len(tl.Entries) {
		duration = tl.Entries[i+1].Timestamp.Sub(entry.Timestamp)
	}
	return BottleneckMarker{
		Timestamp: entry.Timestamp,
		Type:      bType,
		Message:   message,
		Severity:  severity,
		Duration:  duration,
	}
}

// analyzeStabilizedEntry tracks stabilization windows across consecutive stabilized entries,
// closing the window when the next entry is not stabilized. Returns updated stabilization state.
func analyzeStabilizedEntry(analysis *ReplayAnalysis, tl Timeline, i int, entry Entry, stabilizationStart time.Time, stabilizedReplicas int32, lastRescaleTo int32) (time.Time, int32, int32) {
	// Track stabilization window.
	if stabilizationStart.IsZero() {
		stabilizationStart = entry.Timestamp
		stabilizedReplicas = lastRescaleTo
	}
	// If this is the last entry or next entry is not stabilized, close the window.
	if i == len(tl.Entries)-1 || (i+1 < len(tl.Entries) && tl.Entries[i+1].Category != "stabilized") {
		endTime := entry.Timestamp
		if i+1 < len(tl.Entries) {
			endTime = tl.Entries[i+1].Timestamp
		}
		analysis.StabilizationWindows = append(analysis.StabilizationWindows, StabilizationWindow{
			Start:               stabilizationStart,
			End:                 endTime,
			Duration:            endTime.Sub(stabilizationStart),
			SuppressedScaleDown: stabilizedReplicas - lastRescaleTo,
		})
		stabilizationStart = time.Time{}
		stabilizedReplicas = 0
	}
	return stabilizationStart, stabilizedReplicas, lastRescaleTo
}

// analyzeMetricChangeEntry detects CPU tolerance effects from a metric-change entry.
func analyzeMetricChangeEntry(analysis *ReplayAnalysis, hpa *autoscalingv2.HorizontalPodAutoscaler, entry Entry) {
	// Detect tolerance effects from metric changes.
	if !strings.Contains(entry.Message, "cpu") && !strings.Contains(entry.Message, "CPU") {
		return
	}
	for _, metric := range hpa.Status.CurrentMetrics {
		if !isCPUResourceMetric(metric) {
			continue
		}
		effect, ok := buildCPUToleranceEffect(entry, hpa, metric)
		if !ok {
			continue
		}
		analysis.ReplayToleranceEffects = append(analysis.ReplayToleranceEffects, effect)
	}
}

func isCPUResourceMetric(metric autoscalingv2.MetricStatus) bool {
	return metric.Type == autoscalingv2.ResourceMetricSourceType &&
		metric.Resource != nil &&
		metric.Resource.Name == corev1.ResourceCPU &&
		metric.Resource.Current.AverageUtilization != nil
}

// buildCPUToleranceEffect constructs a ReplayToleranceEffect for a CPU resource metric, returning ok=false when inputs are missing.
func buildCPUToleranceEffect(entry Entry, hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (ReplayToleranceEffect, bool) {
	ratio := float64(*metric.Resource.Current.AverageUtilization) / 100.0
	target := resourceMetricTargetUtilization(hpa, metric.Resource.Name)
	targetRatio := 0.5 // Default 50% target
	if target != nil {
		targetRatio = float64(*target) / 100.0
	}
	metricRatio := ratio / targetRatio
	tolerance, _ := util.DirectionalTolerance(hpa, metricRatio)
	withinTolerance, _ := util.RatioWithinTolerance(hpa, metricRatio)
	return ReplayToleranceEffect{
		Timestamp:   entry.Timestamp,
		MetricName:  "cpu",
		ActualRatio: ratio,
		Tolerance:   tolerance,
		Suppressed:  withinTolerance,
	}, true
}
