package hpa

import (
	"fmt"
	"sort"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

const (
	// flappingWindowThreshold is the maximum time window (in seconds) within
	// which direction flips are considered rapid oscillation.
	flappingWindowThreshold = 300 // 5 minutes

	// eventTTLDefault is the default Kubernetes event TTL used in the
	// limitation disclaimer.
	eventTTLDefault = "~1 hour"
)

// DiagnoseFlapping analyzes HPA rescale events for rapid oscillation patterns
// (alternating scale-up/scale-down in short windows) and produces a diagnosis
// with root causes and recommendations. Returns nil if fewer than 3 rescale
// events are available (insufficient data to establish a pattern).
func DiagnoseFlapping(events []Event, hpa *autoscalingv2.HorizontalPodAutoscaler) *FlappingDiagnosis {
	if hpa == nil || len(events) == 0 {
		return nil
	}

	rescales := extractRescaleEvents(events)
	if len(rescales) < 3 {
		return nil
	}

	sort.Slice(rescales, func(i, j int) bool {
		return rescales[i].timestamp.Before(rescales[j].timestamp)
	})

	flips := detectDirectionFlips(rescales)
	if len(flips) == 0 {
		return &FlappingDiagnosis{
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

	diagnosis := &FlappingDiagnosis{
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
func detectDirectionFlips(rescales []rescaleData) []directionFlip {
	var flips []directionFlip
	prevDirection := 0

	for i := 1; i < len(rescales); i++ {
		delta := rescales[i].newSize - rescales[i-1].newSize
		if delta == 0 {
			continue
		}

		dir := 1
		if delta < 0 {
			dir = -1
		}

		if prevDirection != 0 && dir != prevDirection {
			flips = append(flips, directionFlip{
				timestamp: rescales[i].timestamp,
				from:      rescales[i-1].newSize,
				to:        rescales[i].newSize,
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
func diagnoseFlappingCauses(hpa *autoscalingv2.HorizontalPodAutoscaler, rescales []rescaleData) []FlappingCause {
	var causes []FlappingCause

	// Check for tight target: if rescales involve small replica deltas (1-2),
	// the metric is likely hovering near the target threshold.
	if isTightTarget(rescales) {
		causes = append(causes, FlappingCause{
			Type:        "tight-target",
			Description: "Metric values are oscillating around the target threshold, causing the HPA to repeatedly scale up and down by small amounts.",
			Confidence:  "medium",
		})
	}

	// Check for short stabilization window.
	window := currentStabilizationWindowSeconds(hpa)
	if window < 300 {
		causes = append(causes, FlappingCause{
			Type:        "short-stabilization-window",
			Description: fmt.Sprintf("The scaleDown stabilizationWindowSeconds is %ds (< 300s default). A short window allows the HPA to reverse scaling decisions before metrics stabilize.", window),
			Confidence:  "high",
		})
	}

	// Check for missing scaleDown policies.
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil || len(hpa.Spec.Behavior.ScaleDown.Policies) == 0 {
		causes = append(causes, FlappingCause{
			Type:        "missing-scaledown-policy",
			Description: "No explicit scaleDown policies are configured. The controller uses default behavior which may allow rapid downscaling followed by immediate upscaling.",
			Confidence:  "high",
		})
	}

	return causes
}

// isTightTarget infers whether the metric is hovering near the target by
// checking if rescale events involve small replica deltas (1-2 pods).
func isTightTarget(rescales []rescaleData) bool {
	if len(rescales) < 3 {
		return false
	}

	smallDeltas := 0
	for i := 1; i < len(rescales); i++ {
		delta := rescales[i].newSize - rescales[i-1].newSize
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
func generateFlappingFixes(hpa *autoscalingv2.HorizontalPodAutoscaler, causes []FlappingCause) []FlappingFix {
	if len(causes) == 0 {
		return nil
	}

	var fixes []FlappingFix

	for _, cause := range causes {
		switch cause.Type {
		case "tight-target":
			fixes = append(fixes, FlappingFix{
				Action:    "Increase the scaleDown tolerance or widen the target range",
				Rationale: "A wider tolerance band around the target prevents the HPA from reacting to small metric fluctuations near the threshold.",
			})

		case "short-stabilization-window":
			currentWindow := currentStabilizationWindowSeconds(hpa)
			recommendedWindow := currentWindow * 2
			if recommendedWindow < 300 {
				recommendedWindow = 300
			}
			patch := mustJSON(map[string]any{
				"spec": map[string]any{
					"behavior": map[string]any{
						"scaleDown": map[string]any{
							"stabilizationWindowSeconds": recommendedWindow,
						},
					},
				},
			})
			fixes = append(fixes, FlappingFix{
				Action:    fmt.Sprintf("Increase scaleDown stabilizationWindowSeconds from %ds to %ds", currentWindow, recommendedWindow),
				Patch:     patch,
				Rationale: "A longer stabilization window gives the HPA more time to observe sustained metric changes before reversing a scaling decision.",
			})

		case "missing-scaledown-policy":
			window := currentStabilizationWindowSeconds(hpa)
			patch := mustJSON(map[string]any{
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
			fixes = append(fixes, FlappingFix{
				Action:    "Add explicit scaleDown policy (50%/60s)",
				Patch:     patch,
				Rationale: "Explicit scaleDown policies bound the rate of replica removal, preventing rapid downscale followed by immediate upscale cycles.",
			})
		}
	}

	return fixes
}
