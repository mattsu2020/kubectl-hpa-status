// Package churn analyzes HPA scaling churn (frequent rescales that waste
// resources and destabilize the workload). It is a self-contained leaf
// domain depending only on autoscaling/v2 types and the shared event type.
package churn

import (
	"fmt"

	"sort"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
)

// ChurnLevel classifies the severity of HPA replica thrashing.
//
//nolint:revive // stutter is intentional: "Level" alone collides with pkg/hpa.Analysis
type ChurnLevel string

const (
	// ChurnLow indicates minimal oscillation (score 0-25).
	ChurnLow ChurnLevel = "LOW"
	// ChurnMedium indicates moderate oscillation that warrants attention (score 26-50).
	ChurnMedium ChurnLevel = "MEDIUM"
	// ChurnHigh indicates significant thrashing that likely impacts workload stability (score 51-75).
	ChurnHigh ChurnLevel = "HIGH"
	// ChurnCritical indicates severe oscillation requiring immediate remediation (score 76-100).
	ChurnCritical ChurnLevel = "CRITICAL"
)

// ChurnAnalysis holds the result of thrashing/churn detection for HPA scaling events.
//
//nolint:revive // stutter is intentional: "Analysis" alone collides with pkg/hpa.Analysis
type ChurnAnalysis struct {
	// Score is the churn severity score from 0 (no churn) to 100 (extreme churn).
	Score int `json:"score" yaml:"score"`
	// ChurnLevel is the qualitative churn classification based on Score.
	Level ChurnLevel `json:"level" yaml:"level"`
	// ScaleUpCount is the number of scale-up events observed.
	ScaleUpCount int `json:"scaleUpCount" yaml:"scaleUpCount"`
	// ScaleDownCount is the number of scale-down events observed.
	ScaleDownCount int `json:"scaleDownCount" yaml:"scaleDownCount"`
	// DirectionFlips counts how many times the scaling direction changed between
	// consecutive events (e.g. scale-up followed by scale-down counts as one flip).
	DirectionFlips int `json:"directionFlips" yaml:"directionFlips"`
	// AvgReplicaDelta is the average absolute replica change across all rescale events.
	AvgReplicaDelta float64 `json:"avgReplicaDelta" yaml:"avgReplicaDelta"`
	// MaxReplicaDelta is the largest absolute replica change observed.
	MaxReplicaDelta int32 `json:"maxReplicaDelta" yaml:"maxReplicaDelta"`
	// TimeWindow is the duration from the first to the last rescale event.
	TimeWindow time.Duration `json:"timeWindow" yaml:"timeWindow"`
	// Recommendations lists actionable suggestions to reduce churn, generated
	// based on the churn level.
	Recommendations []ChurnRecommendation `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
}

// ChurnRecommendation describes a single actionable suggestion to reduce HPA churn.
//
//nolint:revive // stutter is intentional: matches ChurnAnalysis/ChurnLevel naming
type ChurnRecommendation struct {
	// Type categorizes the recommendation (e.g. "stabilization-window", "tolerance", "behavior-policy").
	Type string `json:"type" yaml:"type"`
	// CurrentValue describes the current HPA configuration value.
	CurrentValue string `json:"currentValue" yaml:"currentValue"`
	// RecommendedValue describes the suggested configuration value.
	RecommendedValue string `json:"recommendedValue" yaml:"recommendedValue"`
	// Rationale explains why this change would reduce churn.
	Rationale string `json:"rationale" yaml:"rationale"`
	// Patch is a JSON merge patch that applies the recommendation.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Confidence indicates how confident the analysis is about this recommendation.
	Confidence string `json:"confidence" yaml:"confidence"`
}

// AnalyzeChurnFromEvents detects thrashing/churn patterns in HPA scaling by
// examining SuccessfulRescale events. It extracts the new replica count from
// each event message, tracks direction changes, and produces a churn score.
//
// Returns nil if fewer than 3 rescale events are available (insufficient data
// to establish a thrashing pattern). The function is pure: it does not modify
// the input slices or depend on external state.
func AnalyzeChurnFromEvents(events []event.Event, hpa *autoscalingv2.HorizontalPodAutoscaler) *ChurnAnalysis {
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

	if len(rescales) < 3 {
		return nil
	}

	// Sort by timestamp ascending to establish chronological order.
	sort.Slice(rescales, func(i, j int) bool {
		return rescales[i].Timestamp.Before(rescales[j].Timestamp)
	})

	return buildChurnAnalysis(rescales, hpa)
}

// AnalyzeFromRescales runs thrashing/churn patterns in HPA scaling by
// examining changes in TimelineSnapshot.Desired values. It tracks direction
// changes and produces a churn score using the same algorithm as
// AnalyzeChurnFromEvents.
//
// Returns nil if fewer than 3 snapshots with distinct Desired values are
// available. The function is pure: it does not modify the input slices or
// depend on external state.
// AnalyzeFromRescales runs the churn analysis on a pre-extracted slice of
// rescale data. This is the canonical entry point for callers that already
// have rescale data (e.g. converted from TimelineSnapshots in the pkg/hpa
// facade). It sorts the input by timestamp ascending before analysis.
func AnalyzeFromRescales(rescales []event.RescaleData, hpa *autoscalingv2.HorizontalPodAutoscaler) *ChurnAnalysis {
	if len(rescales) < 3 {
		return nil
	}

	// Sort by timestamp ascending to establish chronological order.
	sort.Slice(rescales, func(i, j int) bool {
		return rescales[i].Timestamp.Before(rescales[j].Timestamp)
	})

	return buildChurnAnalysis(rescales, hpa)
}

// buildChurnAnalysis computes the churn analysis from an ordered sequence of
// rescale data points. Both AnalyzeChurnFromEvents and AnalyzeChurnFromSnapshots
// delegate to this function after extracting and sorting their rescale data.
func buildChurnAnalysis(rescales []event.RescaleData, hpa *autoscalingv2.HorizontalPodAutoscaler) *ChurnAnalysis {
	scaleUpCount := 0
	scaleDownCount := 0
	directionFlips := 0
	var totalDelta float64
	var maxDelta int32

	// Track the previous direction: 1 = scale-up, -1 = scale-down, 0 = initial.
	prevDirection := 0

	for i := 1; i < len(rescales); i++ {
		delta := rescales[i].NewSize - rescales[i-1].NewSize
		absDelta := delta
		if absDelta < 0 {
			absDelta = -delta
		}

		totalDelta += float64(absDelta)
		if absDelta > maxDelta {
			maxDelta = absDelta
		}

		var direction int
		switch {
		case delta > 0:
			direction = 1
			scaleUpCount++
		case delta < 0:
			direction = -1
			scaleDownCount++
		default:
			// No change in replica count; skip direction tracking.
			continue
		}

		if prevDirection != 0 && direction != prevDirection {
			directionFlips++
		}
		prevDirection = direction
	}

	totalEvents := scaleUpCount + scaleDownCount
	if totalEvents == 0 {
		return nil
	}

	avgReplicaDelta := totalDelta / float64(totalEvents)
	timeWindow := rescales[len(rescales)-1].Timestamp.Sub(rescales[0].Timestamp)

	oscillationRate := float64(directionFlips) / float64(totalEvents)
	score := directionFlips*15 + int(oscillationRate*40)
	if score > 100 {
		score = 100
	}

	level := churnLevelFromScore(score)
	recommendations := generateChurnRecommendations(level, hpa)

	return &ChurnAnalysis{
		Score:           score,
		Level:           level,
		ScaleUpCount:    scaleUpCount,
		ScaleDownCount:  scaleDownCount,
		DirectionFlips:  directionFlips,
		AvgReplicaDelta: avgReplicaDelta,
		MaxReplicaDelta: maxDelta,
		TimeWindow:      timeWindow,
		Recommendations: recommendations,
	}
}

// churnLevelFromScore maps a numeric churn score to a ChurnLevel.
func churnLevelFromScore(score int) ChurnLevel {
	switch {
	case score <= 25:
		return ChurnLow
	case score <= 50:
		return ChurnMedium
	case score <= 75:
		return ChurnHigh
	default:
		return ChurnCritical
	}
}

// generateChurnRecommendations produces actionable recommendations based on
// the detected churn level and current HPA configuration.
func generateChurnRecommendations(level ChurnLevel, hpa *autoscalingv2.HorizontalPodAutoscaler) []ChurnRecommendation {
	if level == ChurnLow {
		return nil
	}

	var recommendations []ChurnRecommendation

	// For MEDIUM and above: recommend increasing the stabilization window.
	recommendations = append(recommendations, stabilizationWindowRecommendation(hpa))

	// For HIGH and above: recommend adding tolerance.
	if level == ChurnHigh || level == ChurnCritical {
		recommendations = append(recommendations, toleranceRecommendation())
		recommendations = append(recommendations, behaviorPolicyRecommendation(hpa))
	}

	return recommendations
}

// stabilizationWindowRecommendation recommends doubling the current scale-down
// stabilization window to reduce oscillation.
func stabilizationWindowRecommendation(hpa *autoscalingv2.HorizontalPodAutoscaler) ChurnRecommendation {
	currentWindow := currentStabilizationWindowSeconds(hpa)
	recommendedWindow := currentWindow * 2

	patch := util.MustMarshalJSON(map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"stabilizationWindowSeconds": recommendedWindow,
				},
			},
		},
	})

	return ChurnRecommendation{
		Type:             "stabilization-window",
		CurrentValue:     fmt.Sprintf("%ds", currentWindow),
		RecommendedValue: fmt.Sprintf("%ds", recommendedWindow),
		Rationale:        "Increasing the scale-down stabilization window gives the HPA more time to observe sustained metric changes before reversing a scaling decision, reducing oscillation.",
		Patch:            patch,
		Confidence:       "medium",
	}
}

// toleranceRecommendation recommends adding explicit tolerance to dampen
// small metric fluctuations that trigger unnecessary rescales.
func toleranceRecommendation() ChurnRecommendation {
	patch := util.MustMarshalJSON(map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"tolerance": "0.1",
				},
			},
		},
	})

	return ChurnRecommendation{
		Type:             "tolerance",
		CurrentValue:     "default (0.1)",
		RecommendedValue: "0.1 (explicit)",
		Rationale:        "Adding an explicit tolerance dampens small metric fluctuations, preventing the HPA from reacting to noise near the target threshold.",
		Patch:            patch,
		Confidence:       "medium",
	}
}

// behaviorPolicyRecommendation recommends adding explicit scale-down behavior
// policies to bound the rate of replica removal.
func behaviorPolicyRecommendation(hpa *autoscalingv2.HorizontalPodAutoscaler) ChurnRecommendation {
	stabilizationSeconds := currentStabilizationWindowSeconds(hpa)

	patch := util.MustMarshalJSON(map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"stabilizationWindowSeconds": stabilizationSeconds,
					"selectPolicy":               "Max",
					"policies": []map[string]any{
						{"type": "Percent", "value": 50, "periodSeconds": 60},
					},
				},
			},
		},
	})

	return ChurnRecommendation{
		Type:             "behavior-policy",
		CurrentValue:     "implicit (no explicit scaleDown policies)",
		RecommendedValue: "Max selectPolicy with 50%/60s policy",
		Rationale:        "Explicit scale-down policies bound the rate of replica removal, preventing rapid downscale followed by immediate upscale cycles.",
		Patch:            patch,
		Confidence:       "medium",
	}
}

// currentStabilizationWindowSeconds returns the configured scale-down
// stabilization window, defaulting to 300 seconds when not explicitly set.
// A nil HPA is valid: snapshot-based callers (AnalyzeFromSnapshots) analyze
// recorded traces without access to the live HPA object.
func currentStabilizationWindowSeconds(hpa *autoscalingv2.HorizontalPodAutoscaler) int32 {
	if hpa == nil || hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return 300
	}
	if hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil {
		return 300
	}
	return *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
}

// parseRescaleSize extracts the new replica count from an HPA event message
// containing the "New size: N" pattern. Returns 0 if the pattern cannot be
// parsed.
func parseRescaleSize(message string) int32 {
	result, _ := event.ParseNewSize(message)
	return result
}
