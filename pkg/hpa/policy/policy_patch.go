package policy

import (
	"encoding/json"
	"fmt"
	"os"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"sigs.k8s.io/yaml"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/suggestion"
)

// LoadPolicyFile reads and validates a policy YAML file from the given path.
func LoadPolicyFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("reading policy file %s: %w", path, err)
	}

	var pf File
	if err := yaml.UnmarshalStrict(data, &pf); err != nil {
		return File{}, fmt.Errorf("parsing policy file %s: %w", path, err)
	}

	if err := pf.Validate(); err != nil {
		return File{}, fmt.Errorf("validating policy file %s: %w", path, err)
	}

	return pf, nil
}

// EvaluateMergePatch applies an RFC 7396 JSON merge patch to a copy of hpa and
// evaluates the resulting, complete HPA against policyFile. Callers use this
// after combining multiple independently suggested patches so policy rules are
// checked against the exact final state that would be persisted.
func EvaluateMergePatch(hpa *autoscalingv2.HorizontalPodAutoscaler, mergePatch string, policyFile File) (*Report, error) {
	if hpa == nil {
		return nil, fmt.Errorf("cannot evaluate policy guard without an HPA")
	}
	patched, err := applySuggestionPatch(hpa, mergePatch)
	if err != nil {
		return nil, fmt.Errorf("applying merged patch for policy evaluation: %w", err)
	}
	return EvaluatePolicies(patched, policyFile), nil
}

// GuardFix evaluates suggested merge patches against a policy file before
// applying them. Each suggestion is applied to a copy of the HPA and then
// checked with the existing policy engine.
func GuardFix(suggestions []suggestion.Suggestion, policyFile File, hpa *autoscalingv2.HorizontalPodAutoscaler) *suggestion.GuardResult {
	result := &suggestion.GuardResult{}
	if hpa == nil {
		for _, sug := range suggestions {
			result.Blocked = append(result.Blocked, suggestion.GuardBlocked{
				Suggestion: sug,
				Reason:     "cannot evaluate policy guard without an HPA",
				PolicyRule: "policy-guard",
			})
		}
		return result
	}

	for _, sug := range suggestions {
		if !sug.Apply || sug.Patch == "" {
			result.Allowed = append(result.Allowed, sug)
			continue
		}

		patched, err := applySuggestionPatch(hpa, sug.Patch)
		if err != nil {
			result.Blocked = append(result.Blocked, suggestion.GuardBlocked{
				Suggestion: sug,
				Reason:     fmt.Sprintf("invalid patch: %v", err),
				PolicyRule: "policy-guard",
			})
			continue
		}

		report := EvaluatePolicies(patched, policyFile)
		critical := firstViolationWithSeverity(report.Violations, "critical")
		if critical != nil {
			result.Blocked = append(result.Blocked, suggestion.GuardBlocked{
				Suggestion: sug,
				Reason:     critical.Description,
				PolicyRule: critical.RuleID,
			})
			continue
		}
		warning := firstViolationWithSeverity(report.Violations, "warning")
		if warning != nil {
			result.Warnings = append(result.Warnings, suggestion.GuardWarning{
				Suggestion: sug,
				Reason:     warning.Description,
				PolicyRule: warning.RuleID,
			})
		}
		result.Allowed = append(result.Allowed, sug)
	}

	return result
}

func applySuggestionPatch(hpa *autoscalingv2.HorizontalPodAutoscaler, patch string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	base, err := json.Marshal(hpa)
	if err != nil {
		return nil, err
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		return nil, err
	}
	var patchMap map[string]any
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

func mergeJSONMap(dst, src map[string]any) {
	for key, srcValue := range src {
		if srcValue == nil {
			delete(dst, key)
			continue
		}
		srcMap, srcIsMap := srcValue.(map[string]any)
		dstMap, dstIsMap := dst[key].(map[string]any)
		if srcIsMap && dstIsMap {
			mergeJSONMap(dstMap, srcMap)
			continue
		}
		dst[key] = srcValue
	}
}

func firstViolationWithSeverity(violations []Violation, severity string) *Violation {
	for i := range violations {
		if violations[i].Severity == severity {
			return &violations[i]
		}
	}
	return nil
}
