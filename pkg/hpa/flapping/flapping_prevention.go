package flapping

import (
	"fmt"
	"sort"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

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
		patch := util.MustMarshalJSON(map[string]any{
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

// parseRescaleSize extracts the new replica count from an HPA event message.
// Returns 0 when the message does not contain a parseable size.
func parseRescaleSize(message string) int32 {
	result, _ := event.ParseNewSize(message)
	return result
}
