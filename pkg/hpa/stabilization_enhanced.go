package hpa

import (
	"fmt"
	"math"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

const (
	// stabilizationConfidenceLabel is the confidence disclaimer for stabilization
	// estimates. The Kubernetes HPA controller uses the max recommendation within
	// the window rather than a simple LastScaleTime timer, so the estimate is
	// approximate.
	stabilizationConfidenceLabel = "estimated (API limitation)"

	// StabilizationSourceScaleDown indicates stabilization from scaleDown behavior.
	StabilizationSourceScaleDown = "scaleDown"

	// StabilizationSourceScaleUp indicates stabilization from scaleUp behavior.
	StabilizationSourceScaleUp = "scaleUp"
)

// detectStabilizationSource determines which behavior direction (scaleDown or
// scaleUp) caused the stabilization window to be active. It inspects the HPA
// conditions and behavior configuration.
func detectStabilizationSource(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if hpa == nil {
		return StabilizationSourceScaleDown
	}

	condition := FindCondition(hpa, ConditionAbleToScale)

	// ScaleDownStabilized reason explicitly indicates scaleDown.
	if condition != nil && condition.Reason == "ScaleDownStabilized" {
		return StabilizationSourceScaleDown
	}

	// Check if scaleUp has a non-zero stabilization window configured.
	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleUp != nil {
		if hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds != nil &&
			*hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds > 0 {
			return StabilizationSourceScaleUp
		}
	}

	return StabilizationSourceScaleDown
}

// FormatASCIIProgressBar renders a text progress bar using block characters.
// Ratio is 0.0–1.0 representing elapsed progress. Width is the total bar
// width in characters.
//
// Example: ratio=0.62, width=24 → "[███████████████░░░░░░░░░]"
func FormatASCIIProgressBar(ratio float64, width int) string {
	if width <= 0 {
		width = 24
	}
	// Clamp ratio to [0, 1].
	ratio = math.Max(0, math.Min(1.0, ratio))

	inner := width - 2 // subtract brackets
	if inner < 1 {
		inner = 1
	}
	innerFilled := int(math.Round(ratio * float64(inner)))
	if innerFilled > inner {
		innerFilled = inner
	}

	bar := fmt.Sprintf("[%s%s]",
		repeatChar("█", innerFilled),
		repeatChar("░", inner-innerFilled),
	)

	// Percentage label.
	pct := int(math.Round(ratio * 100))
	return fmt.Sprintf("%s %d%% elapsed", bar, pct)
}

// FormatStabilizationExplain produces the full stabilization explanation
// block for --explain mode output. It includes the status, progress bar,
// remaining time, source behavior, and confidence label.
func FormatStabilizationExplain(a Analysis) string {
	if a.StabilizationRemaining == nil || *a.StabilizationRemaining <= 0 {
		return ""
	}

	remaining := *a.StabilizationRemaining
	window := int64(0)
	if a.StabilizationWindowSeconds != nil {
		window = int64(*a.StabilizationWindowSeconds)
	}

	// Basic progress line.
	progress := FormatStabilizationProgress(a.StabilizationRemaining, a.StabilizationWindowSeconds)

	var lines []string
	lines = append(lines, fmt.Sprintf("ScalingDownStabilized: True (%s)", progress))

	// Progress bar.
	if window > 0 {
		ratio := StabilizationProgressRatio(a.StabilizationRemaining, a.StabilizationWindowSeconds)
		bar := FormatASCIIProgressBar(ratio, 24)
		lines = append(lines, bar)
	}

	// Remaining estimate line.
	remainingStr := FormatDuration(remaining)
	if window > 0 {
		windowStr := FormatDuration(window)
		lines = append(lines, fmt.Sprintf("→ Scale down will be enabled in approximately %s (window: %s)", remainingStr, windowStr))
	} else {
		lines = append(lines, fmt.Sprintf("→ Scale down will be enabled in approximately %s", remainingStr))
	}

	// Source and confidence.
	source := a.StabilizationSource
	if source == "" {
		source = StabilizationSourceScaleDown
	}
	lines = append(lines, fmt.Sprintf("  source: %s behavior  [%s]", source, a.StabilizationConfidence))

	// Join with newlines and indent continuation lines.
	result := lines[0]
	for _, line := range lines[1:] {
		result += "\n" + line
	}

	return result
}

// FormatStabilizationWithSource returns a formatted stabilization string
// including the source behavior direction.
func FormatStabilizationWithSource(remaining *int64, windowSeconds *int32, source string) string {
	if remaining == nil || *remaining <= 0 {
		return ""
	}

	progress := FormatStabilizationProgress(remaining, windowSeconds)
	if source == "" {
		return progress
	}
	return fmt.Sprintf("%s — %s stabilization", progress, source)
}

// repeatChar repeats a string n times.
func repeatChar(s string, n int) string {
	if n <= 0 {
		return ""
	}
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
