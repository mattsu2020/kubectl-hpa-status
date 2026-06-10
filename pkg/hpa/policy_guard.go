package hpa

import (
	"encoding/json"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// GuardFix evaluates suggested merge patches against a policy file before
// applying them. Each suggestion is applied to a copy of the HPA and then
// checked with the existing policy engine.
func GuardFix(suggestions []Suggestion, policyFile PolicyFile, hpa *autoscalingv2.HorizontalPodAutoscaler) *GuardResult {
	result := &GuardResult{}
	if hpa == nil {
		for _, suggestion := range suggestions {
			result.Blocked = append(result.Blocked, GuardBlocked{
				Suggestion: suggestion,
				Reason:     "cannot evaluate policy guard without an HPA",
				PolicyRule: "policy-guard",
			})
		}
		return result
	}

	for _, suggestion := range suggestions {
		if !suggestion.Apply || suggestion.Patch == "" {
			result.Allowed = append(result.Allowed, suggestion)
			continue
		}

		patched, err := applySuggestionPatch(hpa, suggestion.Patch)
		if err != nil {
			result.Blocked = append(result.Blocked, GuardBlocked{
				Suggestion: suggestion,
				Reason:     fmt.Sprintf("invalid patch: %v", err),
				PolicyRule: "policy-guard",
			})
			continue
		}

		report := EvaluatePolicies(patched, policyFile)
		critical := firstViolationWithSeverity(report.Violations, "critical")
		if critical != nil {
			result.Blocked = append(result.Blocked, GuardBlocked{
				Suggestion: suggestion,
				Reason:     critical.Description,
				PolicyRule: critical.RuleID,
			})
			continue
		}
		warning := firstViolationWithSeverity(report.Violations, "warning")
		if warning != nil {
			result.Warnings = append(result.Warnings, GuardWarning{
				Suggestion: suggestion,
				Reason:     warning.Description,
				PolicyRule: warning.RuleID,
			})
		}
		result.Allowed = append(result.Allowed, suggestion)
	}

	return result
}

func applySuggestionPatch(hpa *autoscalingv2.HorizontalPodAutoscaler, patch string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	base, err := json.Marshal(hpa)
	if err != nil {
		return nil, err
	}
	var baseMap map[string]interface{}
	if err := json.Unmarshal(base, &baseMap); err != nil {
		return nil, err
	}
	var patchMap map[string]interface{}
	if err := json.Unmarshal([]byte(patch), &patchMap); err != nil {
		return nil, err
	}

	mergeJSONMap(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		return nil, err
	}
	var patched autoscalingv2.HorizontalPodAutoscaler
	if err := json.Unmarshal(merged, &patched); err != nil {
		return nil, err
	}
	return &patched, nil
}

func mergeJSONMap(dst, src map[string]interface{}) {
	for key, srcValue := range src {
		if srcValue == nil {
			delete(dst, key)
			continue
		}
		srcMap, srcIsMap := srcValue.(map[string]interface{})
		dstMap, dstIsMap := dst[key].(map[string]interface{})
		if srcIsMap && dstIsMap {
			mergeJSONMap(dstMap, srcMap)
			continue
		}
		dst[key] = srcValue
	}
}

func firstViolationWithSeverity(violations []PolicyViolation, severity string) *PolicyViolation {
	for i := range violations {
		if violations[i].Severity == severity {
			return &violations[i]
		}
	}
	return nil
}
