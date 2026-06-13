package hpa

import (
	"fmt"
	"strings"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// ReplayAnalysis holds the result of replay analysis on a RetrospectiveTimeline.
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

// AnalyzeReplay performs deep analysis on a RetrospectiveTimeline to extract
// bottleneck markers, control cycles, stabilization windows, and tolerance effects.
func AnalyzeReplay(tl RetrospectiveTimeline, hpa *autoscalingv2.HorizontalPodAutoscaler) *ReplayAnalysis {
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

	// Extract HPA tolerance configuration.
	tolerance := extractHPATolerance(hpa)

	// Track control cycle state.
	var lastRescaleTime time.Time
	var lastRescaleTo int32

	// Track stabilization window state.
	var stabilizationStart time.Time
	var stabilizedReplicas int32

	for i, entry := range tl.Entries {
		switch entry.Category {
		case "rescale":
			// Extract input/output replicas from the message.
			from, to := parseDesiredRange(entry.Message)
			if from == 0 || to == 0 {
				// Try to extract from "desired A -> B" format
				if match := desiredRangeRegex.FindStringSubmatch(entry.Message); len(match) >= 3 {
					if _, err := fmt.Sscanf(match[1], "%d", &from); err != nil {
						from = 0
					}
					if _, err := fmt.Sscanf(match[2], "%d", &to); err != nil {
						to = 0
					}
				}
			}

			// Determine the decision type.
			decision := "no-change"
			metricDriver := "unknown"
			if from > 0 && to > 0 {
				if to > from {
					decision = "scale-up"
				} else if to < from {
					decision = "scale-down"
				}
			}

			// Extract metric driver from message (e.g. "CPU", "MEMORY").
			if strings.Contains(strings.ToUpper(entry.Message), "CPU") {
				metricDriver = "cpu"
			} else if strings.Contains(strings.ToUpper(entry.Message), "MEMORY") {
				metricDriver = "memory"
			}

			// Build control cycle.
			cycleEnd := entry.Timestamp
			var cycleStart time.Time
			if lastRescaleTime.IsZero() {
				cycleStart = tl.Since
			} else {
				cycleStart = lastRescaleTime
			}

			cycle := ControlCycle{
				Start:          cycleStart,
				End:            cycleEnd,
				InputReplicas:  from,
				OutputReplicas: to,
				Decision:       decision,
				MetricDriver:   metricDriver,
			}

			// Check if capped by maxReplicas.
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
			lastRescaleTime = entry.Timestamp
			lastRescaleTo = to

			// Check for suppressed scale-down due to stabilization.
			if !lastRescaleTime.IsZero() && i > 0 {
				prevEntry := tl.Entries[i-1]
				gap := entry.Timestamp.Sub(prevEntry.Timestamp)
				if gap > 60*time.Second && decision == "scale-down" {
					// Estimate suppressed scale-down based on stabilization window.
					stabWindow := scaleDownStabilizationWindowSeconds(hpa)
					if stabWindow > 0 {
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
				}
			}

		case "metrics-unavailable":
			// Metrics unavailable is a high-severity bottleneck.
			duration := time.Duration(0)
			if i+1 < len(tl.Entries) {
				duration = tl.Entries[i+1].Timestamp.Sub(entry.Timestamp)
			}
			analysis.Bottlenecks = append(analysis.Bottlenecks, BottleneckMarker{
				Timestamp: entry.Timestamp,
				Type:      "metrics",
				Message:   "Metrics unavailable - HPA cannot compute desired replicas",
				Severity:  "high",
				Duration:  duration,
			})

		case "scaling-limited":
			// Scaling limited is a medium-severity bottleneck.
			duration := time.Duration(0)
			if i+1 < len(tl.Entries) {
				duration = tl.Entries[i+1].Timestamp.Sub(entry.Timestamp)
			}
			analysis.Bottlenecks = append(analysis.Bottlenecks, BottleneckMarker{
				Timestamp: entry.Timestamp,
				Type:      "policy",
				Message:   fmt.Sprintf("ScalingLimited=True - capped by maxReplicas=%d", hpa.Spec.MaxReplicas),
				Severity:  "medium",
				Duration:  duration,
			})

		case "stabilized":
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

		case "metric-change":
			// Detect tolerance effects from metric changes.
			if strings.Contains(entry.Message, "cpu") || strings.Contains(entry.Message, "CPU") {
				// Extract ratio if available from current metrics.
				for _, metric := range hpa.Status.CurrentMetrics {
					if metric.Type == autoscalingv2.ResourceMetricSourceType && metric.Resource != nil {
						if metric.Resource.Name == corev1.ResourceCPU {
							if metric.Resource.Current.AverageUtilization != nil {
								ratio := float64(*metric.Resource.Current.AverageUtilization) / 100.0
								target := resourceMetricTargetUtilization(hpa, metric.Resource.Name)
								targetRatio := 0.5 // Default 50% target
								if target != nil {
									targetRatio = float64(*target) / 100.0
								}
								withinTolerance := ratio > targetRatio*(1-tolerance) && ratio < targetRatio*(1+tolerance)
								analysis.ReplayToleranceEffects = append(analysis.ReplayToleranceEffects, ReplayToleranceEffect{
									Timestamp:   entry.Timestamp,
									MetricName:  "cpu",
									ActualRatio: ratio,
									Tolerance:   tolerance,
									Suppressed:  withinTolerance,
								})
							}
						}
					}
				}
			}
		}
	}

	// Build summary.
	analysis.Summary = fmt.Sprintf("Detected %d bottlenecks, %d stabilization windows, %d control cycles",
		len(analysis.Bottlenecks), len(analysis.StabilizationWindows), len(analysis.ControlCycles))

	return analysis
}

// extractHPATolerance extracts the HPA tolerance from spec, defaulting to 0.1.
func extractHPATolerance(_ *autoscalingv2.HorizontalPodAutoscaler) float64 {
	// Kubernetes HPA uses a default tolerance of 0.1 (10%).
	// This is not currently exposed in the HPA spec, so we use the default.
	// In the future, this may be configurable via annotations or spec fields.
	return 0.1
}
