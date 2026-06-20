// Package flapping detects HPA scaling flapping (rapid scale-up/scale-down
// oscillation) and recommends stabilization-window adjustments. It is a
// self-contained leaf domain depending only on autoscaling/v2 types, the
// shared event/confidence types, and the util helpers. The cmd/ layer
// reaches it through the pkg/hpa re-export facade.
package flapping

import (
	"fmt"
	"regexp"
	"sort"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

// PreventionReport holds the result of flapping prevention analysis
// with what-if simulations for different stabilization window values.
type PreventionReport struct {
	// CurrentWindow is the current stabilization window in seconds.
	CurrentWindow int32 `json:"currentWindow" yaml:"currentWindow"`
	// CurrentDirectionFlips is the number of direction changes observed.
	CurrentDirectionFlips int `json:"currentDirectionFlips" yaml:"currentDirectionFlips"`
	// ObservationWindow is the time range analyzed.
	ObservationWindow string `json:"observationWindow" yaml:"observationWindow"`
	// Recommendations holds the what-if simulation results for different window values.
	Recommendations []Simulation `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	// Summary is a human-readable summary of the analysis.
	Summary string `json:"summary" yaml:"summary"`
}

// Simulation holds a single what-if simulation result for a specific
// stabilization window value.
type Simulation struct {
	// WindowSeconds is the simulated stabilization window duration.
	WindowSeconds int32 `json:"windowSeconds" yaml:"windowSeconds"`
	// EstimatedFlapReduction is the estimated percentage reduction in flapping.
	EstimatedFlapReduction float64 `json:"estimatedFlapReduction" yaml:"estimatedFlapReduction"`
	// EstimatedDirectionFlips is the estimated number of direction flips at this window.
	EstimatedDirectionFlips int `json:"estimatedDirectionFlips" yaml:"estimatedDirectionFlips"`
	// Rationale explains why this window value would reduce flapping.
	Rationale string `json:"rationale" yaml:"rationale"`
	// Patch is the JSON merge patch to apply this window value.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Confidence is the confidence level for this estimate.
	Confidence string `json:"confidence" yaml:"confidence"`
}

// Diagnosis holds the result of event-based flapping detection with
// root-cause analysis. Unlike PreventionReport which simulates window
// changes, Diagnosis identifies *why* flapping occurs and produces
// actionable recommendations with patches.
type Diagnosis struct {
	// Detected indicates whether flapping was observed.
	Detected bool `json:"detected" yaml:"detected"`
	// Severity classifies the flapping: "LOW", "MEDIUM", "HIGH", "CRITICAL".
	Severity string `json:"severity" yaml:"severity"`
	// Pattern describes the oscillation pattern (e.g. "up-down-up in 3 minutes").
	Pattern string `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	// FlipCount is the number of direction changes observed.
	FlipCount int `json:"flipCount" yaml:"flipCount"`
	// WindowSeconds is the time span of the observed flapping.
	WindowSeconds int `json:"windowSeconds" yaml:"windowSeconds"`
	// EstimatedCauses lists the likely root causes of the flapping.
	EstimatedCauses []Cause `json:"estimatedCauses,omitempty" yaml:"estimatedCauses,omitempty"`
	// Recommendations lists actionable suggestions to stop flapping.
	Recommendations []Fix `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	// EventTTLLimitation warns about the Event TTL constraint.
	EventTTLLimitation string `json:"eventTtlLimitation,omitempty" yaml:"eventTtlLimitation,omitempty"`
}

// Cause describes a likely root cause of HPA replica flapping.
type Cause struct {
	// Type categorizes the cause: "tight-target", "short-stabilization-window",
	// "missing-scaledown-policy".
	Type string `json:"type" yaml:"type"`
	// Description explains why this cause contributes to flapping.
	Description string `json:"description" yaml:"description"`
	// Confidence is the confidence level: "high", "medium", "low".
	Confidence string `json:"confidence" yaml:"confidence"`
}

// Fix describes an actionable recommendation to stop HPA flapping.
type Fix struct {
	// Action describes what to do.
	Action string `json:"action" yaml:"action"`
	// Patch is an optional JSON merge patch to apply the fix.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Rationale explains why this fix helps.
	Rationale string `json:"rationale" yaml:"rationale"`
}

// AnomalyType identifies the kind of anomaly detected in health score history.
type AnomalyType string

const (
	// AnomalySuddenDegradation indicates a rapid health score drop.
	AnomalySuddenDegradation AnomalyType = "sudden-degradation"
	// AnomalyStuckState indicates the health score has not changed for an extended period.
	AnomalyStuckState AnomalyType = "stuck-state"
	// AnomalyOscillationEscalation indicates increasing oscillation in health scores.
	AnomalyOscillationEscalation AnomalyType = "oscillation-escalation"
)

// AnomalyDetection holds a single anomaly detected in health score history.
type AnomalyDetection struct {
	// Timestamp is when the anomaly was detected.
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`
	// Type is the anomaly type.
	Type AnomalyType `json:"type" yaml:"type"`
	// Severity is the severity: "critical", "warning", or "info".
	Severity string `json:"severity" yaml:"severity"`
	// ScoreBefore is the health score before the anomaly.
	ScoreBefore int `json:"scoreBefore" yaml:"scoreBefore"`
	// ScoreAfter is the health score after the anomaly.
	ScoreAfter int `json:"scoreAfter" yaml:"scoreAfter"`
	// Duration describes how long the anomaly condition persisted.
	Duration string `json:"duration,omitempty" yaml:"duration,omitempty"`
	// CauseEstimate is the estimated root cause of the anomaly.
	CauseEstimate string `json:"causeEstimate,omitempty" yaml:"causeEstimate,omitempty"`
	// Remediation suggests actions to address the anomaly.
	Remediation string `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

const (
	// eventTTLDefault is the default Kubernetes event TTL used in the
	// limitation disclaimer.
	eventTTLDefault = "~1 hour"
)

// DiagnoseFlapping analyzes HPA rescale events for rapid oscillation patterns
// (alternating scale-up/scale-down in short windows) and produces a diagnosis
// with root causes and recommendations. Returns nil if fewer than 3 rescale
// events are available (insufficient data to establish a pattern).
func DiagnoseFlapping(events []event.Event, hpa *autoscalingv2.HorizontalPodAutoscaler) *Diagnosis {
	if hpa == nil || len(events) == 0 {
		return nil
	}

	rescales := extractRescaleEvents(events)
	if len(rescales) < 3 {
		return nil
	}

	sort.Slice(rescales, func(i, j int) bool {
		return rescales[i].Timestamp.Before(rescales[j].Timestamp)
	})

	flips := detectDirectionFlips(rescales)
	if len(flips) == 0 {
		return &Diagnosis{
			Detected:  false,
			Severity:  "LOW",
			FlipCount: 0,
		}
	}

	flipCount := len(flips)
	windowSeconds := windowFromFlips(flips)
	severity := flappingSeverity(flipCount, windowSeconds)
	pattern := describeFlappingPattern(flips)

	causes := diagnoseFlappingCauses(hpa, rescales)
	recommendations := generateFlappingFixes(hpa, causes)

	diagnosis := &Diagnosis{
		Detected:           true,
		Severity:           severity,
		Pattern:            pattern,
		FlipCount:          flipCount,
		WindowSeconds:      windowSeconds,
		EstimatedCauses:    causes,
		Recommendations:    recommendations,
		EventTTLLimitation: fmt.Sprintf("Events are retained for %s by default; older flapping patterns may not be visible. Use 'record' for long-term monitoring.", eventTTLDefault),
	}

	return diagnosis
}

// directionFlip represents a point where the scaling direction changed.
type directionFlip struct {
	timestamp time.Time
	from      int32 // previous replica count
	to        int32 // next replica count
	direction int   // 1 = scale-up, -1 = scale-down
}

// detectDirectionFlips identifies points where the scaling direction changes
// between consecutive rescale events.
func detectDirectionFlips(rescales []event.RescaleData) []directionFlip {
	var flips []directionFlip
	prevDirection := 0

	for i := 1; i < len(rescales); i++ {
		delta := rescales[i].NewSize - rescales[i-1].NewSize
		if delta == 0 {
			continue
		}

		dir := 1
		if delta < 0 {
			dir = -1
		}

		if prevDirection != 0 && dir != prevDirection {
			flips = append(flips, directionFlip{
				timestamp: rescales[i].Timestamp,
				from:      rescales[i-1].NewSize,
				to:        rescales[i].NewSize,
				direction: dir,
			})
		}
		prevDirection = dir
	}

	return flips
}

// windowFromFlips computes the time span in seconds from the first to the last
// direction flip.
func windowFromFlips(flips []directionFlip) int {
	if len(flips) < 2 {
		return 0
	}
	return int(flips[len(flips)-1].timestamp.Sub(flips[0].timestamp).Seconds())
}

// flappingSeverity classifies flapping severity based on flip count and time
// window.
func flappingSeverity(flipCount int, windowSeconds int) string {
	switch {
	case flipCount >= 6 || (flipCount >= 3 && windowSeconds > 0 && windowSeconds < 300):
		return "CRITICAL"
	case flipCount >= 3 || (flipCount >= 2 && windowSeconds > 0 && windowSeconds < 600):
		return "HIGH"
	case flipCount >= 2:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

// describeFlappingPattern produces a human-readable description of the
// oscillation pattern.
func describeFlappingPattern(flips []directionFlip) string {
	if len(flips) == 0 {
		return ""
	}

	windowSeconds := windowFromFlips(flips)
	windowDesc := formatDuration(time.Duration(windowSeconds) * time.Second)

	if len(flips) == 1 {
		dir := "up-down"
		if flips[0].direction == 1 {
			dir = "down-up"
		}
		return fmt.Sprintf("%s reversal in %s", dir, windowDesc)
	}

	return fmt.Sprintf("%d direction flips in %s", len(flips), windowDesc)
}

// diagnoseFlappingCauses identifies likely root causes of the observed
// flapping.
func diagnoseFlappingCauses(hpa *autoscalingv2.HorizontalPodAutoscaler, rescales []event.RescaleData) []Cause {
	var causes []Cause

	// Check for tight target: if rescales involve small replica deltas (1-2),
	// the metric is likely hovering near the target threshold.
	if isTightTarget(rescales) {
		causes = append(causes, Cause{
			Type:        "tight-target",
			Description: "Metric values are oscillating around the target threshold, causing the HPA to repeatedly scale up and down by small amounts.",
			Confidence:  "medium",
		})
	}

	// Check for short stabilization window.
	window := currentStabilizationWindowSeconds(hpa)
	if window < 300 {
		causes = append(causes, Cause{
			Type:        "short-stabilization-window",
			Description: fmt.Sprintf("The scaleDown stabilizationWindowSeconds is %ds (< 300s default). A short window allows the HPA to reverse scaling decisions before metrics stabilize.", window),
			Confidence:  "high",
		})
	}

	// Check for missing scaleDown policies.
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil || len(hpa.Spec.Behavior.ScaleDown.Policies) == 0 {
		causes = append(causes, Cause{
			Type:        "missing-scaledown-policy",
			Description: "No explicit scaleDown policies are configured. The controller uses default behavior which may allow rapid downscaling followed by immediate upscaling.",
			Confidence:  "high",
		})
	}

	return causes
}

// isTightTarget infers whether the metric is hovering near the target by
// checking if rescale events involve small replica deltas (1-2 pods).
func isTightTarget(rescales []event.RescaleData) bool {
	if len(rescales) < 3 {
		return false
	}

	smallDeltas := 0
	for i := 1; i < len(rescales); i++ {
		delta := rescales[i].NewSize - rescales[i-1].NewSize
		if delta < 0 {
			delta = -delta
		}
		if delta <= 2 {
			smallDeltas++
		}
	}

	// If more than half the deltas are small, the metric is likely tight.
	return smallDeltas > (len(rescales)-1)/2
}

// generateFlappingFixes produces actionable recommendations based on the
// diagnosed causes.
func generateFlappingFixes(hpa *autoscalingv2.HorizontalPodAutoscaler, causes []Cause) []Fix {
	if len(causes) == 0 {
		return nil
	}

	var fixes []Fix

	for _, cause := range causes {
		switch cause.Type {
		case "tight-target":
			fixes = append(fixes, Fix{
				Action:    "Increase the scaleDown tolerance or widen the target range",
				Rationale: "A wider tolerance band around the target prevents the HPA from reacting to small metric fluctuations near the threshold.",
			})

		case "short-stabilization-window":
			currentWindow := currentStabilizationWindowSeconds(hpa)
			recommendedWindow := currentWindow * 2
			if recommendedWindow < 300 {
				recommendedWindow = 300
			}
			patch := util.MarshalJSON(map[string]any{
				"spec": map[string]any{
					"behavior": map[string]any{
						"scaleDown": map[string]any{
							"stabilizationWindowSeconds": recommendedWindow,
						},
					},
				},
			})
			fixes = append(fixes, Fix{
				Action:    fmt.Sprintf("Increase scaleDown stabilizationWindowSeconds from %ds to %ds", currentWindow, recommendedWindow),
				Patch:     patch,
				Rationale: "A longer stabilization window gives the HPA more time to observe sustained metric changes before reversing a scaling decision.",
			})

		case "missing-scaledown-policy":
			window := currentStabilizationWindowSeconds(hpa)
			patch := util.MarshalJSON(map[string]any{
				"spec": map[string]any{
					"behavior": map[string]any{
						"scaleDown": map[string]any{
							"stabilizationWindowSeconds": window,
							"selectPolicy":               "Max",
							"policies": []map[string]any{
								{"type": "Percent", "value": 50, "periodSeconds": 60},
							},
						},
					},
				},
			})
			fixes = append(fixes, Fix{
				Action:    "Add explicit scaleDown policy (50%/60s)",
				Patch:     patch,
				Rationale: "Explicit scaleDown policies bound the rate of replica removal, preventing rapid downscale followed by immediate upscale cycles.",
			})
		}
	}

	return fixes
}

// candidateWindowMultipliers defines the multipliers applied to the current
// stabilization window to generate candidate window values for simulation.
var candidateWindowMultipliers = []float64{1.5, 2.0, 3.0}

// fixedCandidateWindows provides additional fixed window values (in seconds)
// that are always evaluated regardless of the current window.
var fixedCandidateWindows = []int32{300, 600}

// AnalyzeFlappingPrevention analyzes HPA rescale events to recommend
// stabilization window changes that reduce replica flapping. It simulates
// how different window values would suppress rapid direction reversals.
//
// Returns nil if fewer than 3 rescale events are available (insufficient
// data to establish a flapping pattern). The function is pure: it does not
// modify the input slices or depend on external state.
func AnalyzeFlappingPrevention(events []event.Event, hpa *autoscalingv2.HorizontalPodAutoscaler) *PreventionReport {
	rescales := extractRescaleEvents(events)
	if len(rescales) < 3 {
		return nil
	}

	sort.Slice(rescales, func(i, j int) bool {
		return rescales[i].Timestamp.Before(rescales[j].Timestamp)
	})

	currentWindow := currentStabilizationWindowSeconds(hpa)
	directionFlips := countDirectionFlips(rescales)
	observationWindow := formatObservationWindow(rescales)

	candidates := buildCandidateWindows(currentWindow)
	recommendations := simulateCandidates(rescales, directionFlips, candidates)

	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].EstimatedFlapReduction > recommendations[j].EstimatedFlapReduction
	})

	summary := buildFlappingSummary(directionFlips, currentWindow, recommendations)

	return &PreventionReport{
		CurrentWindow:         currentWindow,
		CurrentDirectionFlips: directionFlips,
		ObservationWindow:     observationWindow,
		Recommendations:       recommendations,
		Summary:               summary,
	}
}

// extractRescaleEvents parses SuccessfulRescale events and returns rescale
// data with timestamps and new replica counts. Events with unparseable
// replica counts are skipped.
func extractRescaleEvents(events []event.Event) []event.RescaleData {
	var rescales []event.RescaleData
	for _, ev := range events {
		if ev.Reason != "SuccessfulRescale" {
			continue
		}
		size := parseRescaleSize(ev.Message)
		if size == 0 {
			continue
		}
		rescales = append(rescales, event.RescaleData{
			Timestamp: ev.Timestamp,
			NewSize:   size,
		})
	}
	return rescales
}

// countDirectionFlips counts how many times the scaling direction changes
// between consecutive rescale events. A direction flip occurs when a
// scale-up is followed by a scale-down or vice versa.
func countDirectionFlips(rescales []event.RescaleData) int {
	prevDirection := 0
	flips := 0
	for i := 1; i < len(rescales); i++ {
		delta := rescales[i].NewSize - rescales[i-1].NewSize
		var direction int
		switch {
		case delta > 0:
			direction = 1
		case delta < 0:
			direction = -1
		default:
			continue
		}
		if prevDirection != 0 && direction != prevDirection {
			flips++
		}
		prevDirection = direction
	}
	return flips
}

// buildCandidateWindows generates the set of candidate stabilization window
// values to simulate. It includes multiples of the current window plus
// fixed values. Duplicates and values equal to the current window are
// removed. The result is sorted in ascending order.
func buildCandidateWindows(currentWindow int32) []int32 {
	seen := make(map[int32]bool)
	seen[currentWindow] = true

	var candidates []int32

	for _, mult := range candidateWindowMultipliers {
		val := int32(float64(currentWindow) * mult)
		if !seen[val] {
			seen[val] = true
			candidates = append(candidates, val)
		}
	}

	for _, fixed := range fixedCandidateWindows {
		if !seen[fixed] {
			seen[fixed] = true
			candidates = append(candidates, fixed)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i] < candidates[j]
	})

	return candidates
}

// simulateCandidates runs the window simulation for each candidate and
// returns the recommendations with estimated flap reduction.
func simulateCandidates(rescales []event.RescaleData, currentFlips int, candidates []int32) []Simulation {
	var recommendations []Simulation

	for _, windowSec := range candidates {
		remainingFlips := simulateWindow(rescales, windowSec)
		reduction := computeFlapReduction(currentFlips, remainingFlips)

		if remainingFlips >= currentFlips {
			continue
		}

		confidence := flappingConfidence(reduction)
		patch := util.MarshalJSON(map[string]any{
			"spec": map[string]any{
				"behavior": map[string]any{
					"scaleDown": map[string]any{
						"stabilizationWindowSeconds": windowSec,
					},
				},
			},
		})

		recommendations = append(recommendations, Simulation{
			WindowSeconds:           windowSec,
			EstimatedFlapReduction:  reduction,
			EstimatedDirectionFlips: remainingFlips,
			Rationale: fmt.Sprintf(
				"A %ds stabilization window would suppress %d of %d direction flips (%.0f%% reduction).",
				windowSec, currentFlips-remainingFlips, currentFlips, reduction,
			),
			Patch:      patch,
			Confidence: confidence,
		})
	}

	return recommendations
}

// simulateWindow counts how many direction flips would remain if a
// stabilization window of the given duration were enforced. A flip is
// suppressed when the time between consecutive rescales is less than
// the window AND the direction changed.
func simulateWindow(rescales []event.RescaleData, windowSeconds int32) int {
	window := time.Duration(windowSeconds) * time.Second
	flips := 0
	prevDirection := 0
	prevTime := rescales[0].Timestamp

	for i := 1; i < len(rescales); i++ {
		delta := rescales[i].NewSize - rescales[i-1].NewSize
		gap := rescales[i].Timestamp.Sub(prevTime)

		var direction int
		switch {
		case delta > 0:
			direction = 1
		case delta < 0:
			direction = -1
		default:
			continue
		}

		if prevDirection != 0 && direction != prevDirection {
			if gap >= window {
				flips++
			}
		}

		prevDirection = direction
		prevTime = rescales[i].Timestamp
	}

	return flips
}

// computeFlapReduction returns the percentage reduction in flapping,
// from 0 to 100.
func computeFlapReduction(currentFlips, remainingFlips int) float64 {
	if currentFlips == 0 {
		return 0
	}
	reduction := float64(currentFlips-remainingFlips) / float64(currentFlips) * 100
	if reduction < 0 {
		return 0
	}
	if reduction > 100 {
		return 100
	}
	return reduction
}

// flappingConfidence returns a confidence label based on the estimated
// flap reduction percentage.
func flappingConfidence(reduction float64) string {
	switch {
	case reduction >= 75:
		return "high"
	case reduction >= 40:
		return "medium"
	default:
		return "low"
	}
}

// formatObservationWindow returns a human-readable string for the time
// range spanned by the rescale events.
func formatObservationWindow(rescales []event.RescaleData) string {
	if len(rescales) == 0 {
		return ""
	}
	duration := rescales[len(rescales)-1].Timestamp.Sub(rescales[0].Timestamp)
	return formatFlappingDuration(duration)
}

// formatFlappingDuration converts a duration to a human-readable string.
func formatFlappingDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// buildFlappingSummary creates a one-line summary of the flapping analysis.
func buildFlappingSummary(directionFlips int, currentWindow int32, recommendations []Simulation) string {
	if directionFlips == 0 {
		return fmt.Sprintf("No direction flips detected with current %ds stabilization window.", currentWindow)
	}
	if len(recommendations) == 0 {
		return fmt.Sprintf("%d direction flips detected; current %ds window is already the best option among candidates.", directionFlips, currentWindow)
	}
	best := recommendations[0]
	return fmt.Sprintf("%d direction flips detected with %ds window; increasing to %ds could reduce flips by %.0f%%.",
		directionFlips, currentWindow, best.WindowSeconds, best.EstimatedFlapReduction)
}

// formatDuration formats a duration as a compact human-readable string (e.g.
// "2h30m"). Local copy of pkg/hpa.formatDuration to keep this leaf package
// self-contained; keep in sync with retrospective_render.go.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "0m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 && m > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

// currentStabilizationWindowSeconds returns the HPA scale-down stabilization
// window, defaulting to 300s when unset. Local copy of the pkg/hpa helper; see
// conditions.ScaleDownStabilizationWindow for the nil-returning variant.
func currentStabilizationWindowSeconds(hpa *autoscalingv2.HorizontalPodAutoscaler) int32 {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return 300
	}
	if hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil {
		return 300
	}
	return *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
}

// newSizeRegex extracts the "new size: N" replica count from a
// SuccessfulRescale event message. Local copy of pkg/hpa.newSizeRegex to
// keep this leaf package self-contained; keep in sync with retrospective.go.
var newSizeRegex = regexp.MustCompile(`(?i)new size:\s*(\d+)`)

// parseRescaleSize extracts the new replica count from an HPA event message.
// Returns 0 when the message does not contain a parseable size. Local copy of
// pkg/hpa.parseRescaleSize; keep in sync with churn.go.
func parseRescaleSize(message string) int32 {
	match := newSizeRegex.FindStringSubmatch(message)
	if len(match) < 2 {
		return 0
	}
	var result int32
	if _, err := fmt.Sscanf(match[1], "%d", &result); err != nil {
		return 0
	}
	return result
}
