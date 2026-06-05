package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// BuildSuggestions generates patch suggestions for the given HPA using
// a rule-based approach. Each rule inspects specific HPA conditions and
// returns zero or more suggestions. Rules are evaluated sequentially.
func BuildSuggestions(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Suggestion {
	var suggestions []Suggestion
	if items := scalingActiveRule(hpa, minReplicas); len(items) > 0 {
		return items
	}
	for _, rule := range coreSuggestionRules()[1:] {
		items := rule(hpa, minReplicas)
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
