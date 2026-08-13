// Package churn analyzes HPA scaling churn (frequent rescales that waste
// resources and destabilize the workload). It is a self-contained leaf
// domain depending only on autoscaling/v2 types and the shared event type.
package churn

import (
	"fmt"
	"sort"
	"strings"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/model"
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

	// maxChurnTransitionGap bounds a continuous burst of HPA activity. Replica
	// changes separated by more than an hour are independent observations, not
	// evidence of an active oscillation.
	maxChurnTransitionGap = time.Hour
	// fullChurnTransitionsPerHour is the activity rate at which an oscillation
	// receives its full severity score. Slower patterns are discounted.
	fullChurnTransitionsPerHour = 4.0
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
	// Type categorizes the recommendation (e.g. "stabilization-window" or "behavior-policy").
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
// Returns nil if fewer than 3 unambiguous rescale observations are available
// (insufficient data to establish a thrashing pattern). The function is pure:
// it does not modify the input slices or depend on external state.
func AnalyzeChurnFromEvents(events []model.Event, hpa *autoscalingv2.HorizontalPodAutoscaler) *ChurnAnalysis {
	var rescales []event.RescaleData
	for _, ev := range events {
		if ev.Reason != "SuccessfulRescale" {
			continue
		}
		size, ok := event.ParseNewSize(ev.Message)
		if !ok {
			continue
		}
		rescales = append(rescales, event.RescaleData{
			Timestamp: ev.Timestamp,
			NewSize:   size,
		})
	}

	return buildChurnAnalysis(rescales, hpa)
}

// AnalyzeFromRescales runs the churn analysis on a pre-extracted slice of
// rescale data. This is the canonical entry point for callers that already
// have rescale data (e.g. converted from TimelineSnapshots in the pkg/hpa
// facade). It normalizes a copy into deterministic timestamp order before
// analysis and leaves the caller's slice unchanged.
func AnalyzeFromRescales(rescales []event.RescaleData, hpa *autoscalingv2.HorizontalPodAutoscaler) *ChurnAnalysis {
	return buildChurnAnalysis(rescales, hpa)
}

// buildChurnAnalysis computes the churn analysis from rescale data points.
// It first creates a deterministic chronological copy and discards ambiguous
// same-timestamp observations, because Kubernetes events with identical
// timestamps do not reveal an ordering from which direction changes can be
// reconstructed safely.
func buildChurnAnalysis(rescales []event.RescaleData, hpa *autoscalingv2.HorizontalPodAutoscaler) *ChurnAnalysis {
	rescales = event.NormalizeRescales(rescales)
	if len(rescales) < 3 {
		return nil
	}
	rescales = latestContinuousRescaleSegment(rescales)
	if len(rescales) < 3 {
		return nil
	}

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
	baseScore := float64(directionFlips*15) + oscillationRate*40
	activityFactor := churnActivityFactor(totalEvents, timeWindow)
	score := int(baseScore*activityFactor + 0.5)
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

// normalizeRescaleObservations returns a sorted copy with at most one usable
// observation per timestamp. Exact duplicates collapse to one observation. A
// timestamp containing different replica sizes is omitted because there is no
// reliable ordering or final value, and choosing one would invent transitions.
func normalizeRescaleObservations(rescales []event.RescaleData) []event.RescaleData {
	return event.NormalizeRescales(rescales)
}

// latestContinuousRescaleSegment returns the most recent uninterrupted burst
// of rescale observations. Using the latest segment prevents old, sparse
// changes from being reported as current churn.
func latestContinuousRescaleSegment(rescales []event.RescaleData) []event.RescaleData {
	if len(rescales) == 0 {
		return nil
	}
	start := len(rescales) - 1
	for start > 0 {
		gap := rescales[start].Timestamp.Sub(rescales[start-1].Timestamp)
		if gap > maxChurnTransitionGap {
			break
		}
		start--
	}
	return rescales[start:]
}

func churnActivityFactor(transitions int, window time.Duration) float64 {
	if transitions <= 0 {
		return 0
	}
	if window <= 0 {
		return 1
	}
	ratePerHour := float64(transitions) / window.Hours()
	if ratePerHour >= fullChurnTransitionsPerHour {
		return 1
	}
	return ratePerHour / fullChurnTransitionsPerHour
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

	// For MEDIUM and above: recommend increasing the stabilization window when
	// it is still possible to do so within the Kubernetes API limit.
	if recommendation, ok := stabilizationWindowRecommendation(hpa); ok {
		recommendations = append(recommendations, recommendation)
	}

	// Configurable directional tolerance is feature-gated and the default 0.1
	// would be a no-op, so churn analysis does not emit an automatic tolerance
	// patch. For HIGH and above, consider a scale-down policy only when doing so
	// cannot replace an existing explicit policy.
	if level == ChurnHigh || level == ChurnCritical {
		recommendations = append(recommendations, behaviorPolicyRecommendation(hpa))
	}

	return recommendations
}

// stabilizationWindowRecommendation recommends increasing the current
// scale-down stabilization window without exceeding the Kubernetes API limit.
// An explicitly disabled window starts at 300 seconds. No recommendation is
// returned when the window is already at or above the maximum.
func stabilizationWindowRecommendation(hpa *autoscalingv2.HorizontalPodAutoscaler) (ChurnRecommendation, bool) {
	currentWindow := currentStabilizationWindowSeconds(hpa)
	recommendedWindow, ok := nextStabilizationWindowSeconds(currentWindow)
	if !ok {
		return ChurnRecommendation{}, false
	}

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
	}, true
}

func nextStabilizationWindowSeconds(current int32) (int32, bool) {
	const (
		initialWindow = int64(300)
		maximumWindow = int64(3600)
	)
	if int64(current) >= maximumWindow {
		return 0, false
	}
	if current <= 0 {
		return int32(initialWindow), true
	}
	next := int64(current) * 2
	if next > maximumWindow {
		next = maximumWindow
	}
	if next <= int64(current) {
		return 0, false
	}
	return int32(next), true
}

// behaviorPolicyRecommendation recommends an explicit scale-down policy only
// when no policy is already configured. Replacing an existing policy requires
// workload-specific comparison of Pods/Percent periods and selectPolicy, so the
// safe fallback is patch-free review guidance.
func behaviorPolicyRecommendation(hpa *autoscalingv2.HorizontalPodAutoscaler) ChurnRecommendation {
	rules := scaleDownRules(hpa)
	if rules != nil && rules.SelectPolicy != nil && *rules.SelectPolicy == autoscalingv2.DisabledPolicySelect {
		return ChurnRecommendation{
			Type:             "behavior-policy",
			CurrentValue:     "scale-down disabled",
			RecommendedValue: "keep the existing disabled policy and investigate metric noise",
			Rationale:        "Scale-down is already disabled, which is stricter than a 50% rate limit. No automatic patch is emitted because enabling scale-down could increase churn or change availability behavior.",
			Confidence:       "high",
		}
	}
	if rules != nil && len(rules.Policies) > 0 {
		current := formatScaleDownPolicies(rules)
		return ChurnRecommendation{
			Type:             "behavior-policy",
			CurrentValue:     current,
			RecommendedValue: "keep the existing policy; review it with scaling history before changing it",
			Rationale:        "An explicit scale-down policy is already configured. Replacing it with a generic 50%/60s policy could loosen an existing stricter limit, so no automatic patch is emitted.",
			Confidence:       "medium",
		}
	}

	patch := util.MustMarshalJSON(map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"policies": []map[string]any{
						{"type": "Percent", "value": 50, "periodSeconds": 60},
					},
				},
			},
		},
	})

	return ChurnRecommendation{
		Type:             "behavior-policy",
		CurrentValue:     "default scale-down policy (up to 100%/15s)",
		RecommendedValue: "50%/60s scale-down limit",
		Rationale:        "No explicit scale-down policy is present. A 50%/60s limit is more conservative than the Kubernetes default and bounds rapid replica removal without changing other behavior fields.",
		Patch:            patch,
		Confidence:       "medium",
	}
}

func scaleDownRules(hpa *autoscalingv2.HorizontalPodAutoscaler) *autoscalingv2.HPAScalingRules {
	if hpa == nil || hpa.Spec.Behavior == nil {
		return nil
	}
	return hpa.Spec.Behavior.ScaleDown
}

func formatScaleDownPolicies(rules *autoscalingv2.HPAScalingRules) string {
	if rules == nil || len(rules.Policies) == 0 {
		return "default scale-down policy (up to 100%/15s)"
	}
	selectPolicy := autoscalingv2.MaxChangePolicySelect
	if rules.SelectPolicy != nil {
		selectPolicy = *rules.SelectPolicy
	}
	policies := make([]string, 0, len(rules.Policies))
	for _, policy := range rules.Policies {
		var value string
		switch policy.Type {
		case autoscalingv2.PercentScalingPolicy:
			value = fmt.Sprintf("%d%%/%ds", policy.Value, policy.PeriodSeconds)
		case autoscalingv2.PodsScalingPolicy:
			value = fmt.Sprintf("%d pods/%ds", policy.Value, policy.PeriodSeconds)
		default:
			value = fmt.Sprintf("%s %d/%ds", policy.Type, policy.Value, policy.PeriodSeconds)
		}
		policies = append(policies, value)
	}
	sort.Strings(policies)
	return fmt.Sprintf("%s selectPolicy: %s", selectPolicy, strings.Join(policies, ", "))
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
