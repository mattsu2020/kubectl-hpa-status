package retrospective

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
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
	// End is the end of the observed interval. When no closing entry exists,
	// this is Timeline.Until and does not claim the controller window ended.
	End time.Time
	// Duration is the length of the observed interval.
	Duration time.Duration
	// SuppressedScaleDown is the number of replicas known to have been
	// suppressed. Kubernetes HPA events normally do not expose this value, in
	// which case it is nil.
	SuppressedScaleDown *int32
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
	for i, entry := range tl.Entries {
		switch entry.Category {
		case "rescale":
			lastRescaleTime, lastRescaleTo = analyzeRescaleEntry(
				analysis, tl, i, entry, lastRescaleTime, lastRescaleTo)
		case "metrics-unavailable":
			analysis.Bottlenecks = append(analysis.Bottlenecks, bottleneckUntilNext(tl, i, entry,
				"metrics", "Metrics unavailable - HPA cannot compute desired replicas", "high"))
		case "scaling-limited":
			message := strings.TrimSpace(entry.Message)
			if message == "" {
				message = "Scaling constraint recorded; exact historical limit unavailable"
			}
			analysis.Bottlenecks = append(analysis.Bottlenecks, bottleneckUntilNext(tl, i, entry,
				"policy", message, "medium"))
		case "stabilized":
			stabilizationStart = analyzeStabilizedEntry(
				analysis, tl, i, entry, stabilizationStart)
		}
	}

	// The current HPA snapshot is intentionally not used as evidence for past
	// controller decisions. Keep the parameter for API compatibility.
	_ = hpa

	// Build summary.
	analysis.Summary = fmt.Sprintf("Detected %d bottlenecks, %d stabilization windows, %d control cycles",
		len(analysis.Bottlenecks), len(analysis.StabilizationWindows), len(analysis.ControlCycles))

	return analysis
}

// analyzeRescaleEntry extracts a control cycle from a rescale entry, detects maxReplicas capping,
// and records any stabilization-suppressed scale-down. Returns updated rescale-tracking state.
func analyzeRescaleEntry(analysis *ReplayAnalysis, tl Timeline, i int, entry Entry, lastRescaleTime time.Time, lastRescaleTo int32) (time.Time, int32) {
	from, to, valid := entryReplicaRange(entry)
	if !valid {
		// The first SuccessfulRescale event is commonly destination-only. It is
		// a useful boundary for subsequent cycles, not a controller bottleneck.
		if entry.FromReplicas == nil && entry.ToReplicas != nil {
			return entry.Timestamp, *entry.ToReplicas
		}
		if strings.HasPrefix(strings.ToLower(entry.Message), "failed to rescale:") {
			analysis.Bottlenecks = append(analysis.Bottlenecks, bottleneckUntilNext(
				tl, i, entry, "rescale", entry.Message, "high"))
			return lastRescaleTime, lastRescaleTo
		}
		analysis.Bottlenecks = append(analysis.Bottlenecks, bottleneckUntilNext(
			tl, i, entry, "rescale", "Rescale event could not be reconstructed: "+entry.Message, "medium"))
		return lastRescaleTime, lastRescaleTo
	}
	decision, metricDriver := classifyRescaleDecision(entry, from, to, valid)
	cycle := buildRescaleControlCycle(entry, tl, lastRescaleTime, from, to, decision, metricDriver)

	if decision == "scale-up" && hasRecordedMaxReplicasCapEvidence(entry) {
		cycle.Decision = "capped"
		analysis.Bottlenecks = append(analysis.Bottlenecks, BottleneckMarker{
			Timestamp: entry.Timestamp,
			Type:      "policy",
			Message:   fmt.Sprintf("recorded maxReplicas evidence capped scale-up at %d replicas", to),
			Severity:  "medium",
			Duration:  0,
		})
	}

	analysis.ControlCycles = append(analysis.ControlCycles, cycle)
	newLastRescaleTime := entry.Timestamp
	newLastRescaleTo := to

	return newLastRescaleTime, newLastRescaleTo
}

var (
	requestedReplicasEvidenceRegex = regexp.MustCompile(`(?i)\brequested(?:\s+(?:replicas?|replica\s+count))?\s*(?:=|:|is)?\s*(\d+)\b`)
	maxReplicasEvidenceRegex       = regexp.MustCompile(`(?i)\bmax(?:imum)?(?:replicas?|\s+replica\s+count)?\s*(?:=|:|is)?\s*(\d+)\b`)
)

// hasRecordedMaxReplicasCapEvidence uses only data retained on the historical
// entry. Merely reaching the current HPA maxReplicas value is not proof that a
// past recommendation was capped.
func hasRecordedMaxReplicasCapEvidence(entry Entry) bool {
	evidence := entry.Message + " " + entry.MetricContext
	lower := strings.ToLower(evidence)
	for _, phrase := range []string{
		"scalinglimited",
		"toomanyreplicas",
		"capped by max",
		"limited by max",
		"exceeds max",
		"greater than the maximum",
		"more than the maximum",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}

	requested, requestedOK := parseReplicaEvidence(requestedReplicasEvidenceRegex, evidence)
	maximum, maximumOK := parseReplicaEvidence(maxReplicasEvidenceRegex, evidence)
	return requestedOK && maximumOK && requested > maximum
}

func parseReplicaEvidence(pattern *regexp.Regexp, evidence string) (int32, bool) {
	match := pattern.FindStringSubmatch(evidence)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseInt(match[1], 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(value), true
}

// classifyRescaleDecision determines the scale direction and metric driver from a rescale message.
func classifyRescaleDecision(entry Entry, from, to int32, valid bool) (decision, metricDriver string) {
	decision = "no-change"
	metricDriver = "unknown"
	if valid {
		if to > from {
			decision = "scale-up"
		} else if to < from {
			decision = "scale-down"
		}
	}

	upper := strings.ToUpper(entry.MetricContext)
	if upper == "" {
		upper = strings.ToUpper(entry.Message)
	}
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

// bottleneckUntilNext builds a bottleneck marker whose duration spans from the
// current entry to the next entry, or through the observed timeline end for
// the final entry.
func bottleneckUntilNext(tl Timeline, i int, entry Entry, bType, message, severity string) BottleneckMarker {
	end := tl.Until
	if i+1 < len(tl.Entries) {
		end = tl.Entries[i+1].Timestamp
	}
	duration := end.Sub(entry.Timestamp)
	if duration < 0 {
		duration = 0
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
func analyzeStabilizedEntry(analysis *ReplayAnalysis, tl Timeline, i int, entry Entry, stabilizationStart time.Time) time.Time {
	// Track stabilization window.
	if stabilizationStart.IsZero() {
		stabilizationStart = entry.Timestamp
	}
	// If this is the last entry or next entry is not stabilized, close the
	// observed window. For a final entry, observation continues through the
	// timeline boundary; the controller's actual end remains unknown.
	if i == len(tl.Entries)-1 || (i+1 < len(tl.Entries) && tl.Entries[i+1].Category != "stabilized") {
		endTime := entry.Timestamp
		if i+1 < len(tl.Entries) {
			endTime = tl.Entries[i+1].Timestamp
		} else if !tl.Until.IsZero() && tl.Until.After(entry.Timestamp) {
			endTime = tl.Until
		}
		analysis.StabilizationWindows = append(analysis.StabilizationWindows, StabilizationWindow{
			Start:               stabilizationStart,
			End:                 endTime,
			Duration:            endTime.Sub(stabilizationStart),
			SuppressedScaleDown: nil,
		})
		stabilizationStart = time.Time{}
	}
	return stabilizationStart
}
