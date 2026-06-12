package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// SuggestionContext carries all the data that suggestion rules need to evaluate
// conditions and produce recommendations. Optional fields (CapacityPlan,
// Headroom) are nil when the corresponding analysis flags are not enabled.
type SuggestionContext struct {
	HPA         *autoscalingv2.HorizontalPodAutoscaler
	MinReplicas int32
	Capacity    *SuggestionCapacity
}

// SuggestionCapacity holds capacity-related data for suggestion rules to
// evaluate whether scaling is feasible given cluster constraints.
type SuggestionCapacity struct {
	// RequiredCPU is the estimated CPU required per pod (empty string if unknown).
	RequiredCPU string
	// AvailableCPU is the estimated CPU available in the cluster (empty string if unknown).
	AvailableCPU string
	// RequiredMemory is the estimated memory required per pod (empty string if unknown).
	RequiredMemory string
	// AvailableMemory is the estimated memory available in the cluster (empty string if unknown).
	AvailableMemory string
	// Insufficient is true when cluster capacity is likely insufficient for scale-out.
	Insufficient bool
	// Reason describes why capacity is insufficient (empty if sufficient).
	Reason string
}

// BuildSuggestions generates patch suggestions for the given HPA using
// a rule-based approach. Each rule inspects specific HPA conditions and
// returns zero or more suggestions. Rules are evaluated sequentially.
func BuildSuggestions(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Suggestion {
	return BuildSuggestionsWithContext(SuggestionContext{
		HPA:         hpa,
		MinReplicas: minReplicas,
	})
}

// BuildSuggestionsWithContext generates patch suggestions using the full
// suggestion context including capacity information.
func BuildSuggestionsWithContext(ctx SuggestionContext) []Suggestion {
	var suggestions []Suggestion
	if items := scalingActiveRule(ctx); len(items) > 0 {
		return items
	}
	for _, rule := range coreSuggestionRules()[1:] {
		items := rule(ctx)
		if len(items) == 0 {
			continue
		}
		suggestions = append(suggestions, items...)
	}
	if len(suggestions) == 0 {
		suggestions = append(suggestions, noSafeFixSuggestion())
	}
	return suggestions
}
