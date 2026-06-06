package hpa

import (
	"fmt"
	"os"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"sigs.k8s.io/yaml"
)

// EvaluatePolicies evaluates all applicable policy rules against an HPA
// and returns a PolicyReport with violations and a compliance score.
func EvaluatePolicies(hpa *autoscalingv2.HorizontalPodAutoscaler, policyFile PolicyFile) *PolicyReport {
	report := &PolicyReport{
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
		Score:     100,
	}

	for _, rule := range policyFile.Rules {
		rule = normalizePolicyRule(rule)
		if !rule.IsEnabled() {
			continue
		}
		violations := EvaluateRule(hpa, rule)
		report.Violations = append(report.Violations, violations...)
	}
	for _, set := range policyFile.Policies {
		if !policySetMatches(hpa, set) {
			continue
		}
		for _, rule := range set.Rules {
			rule = normalizePolicyRule(rule)
			if !rule.IsEnabled() {
				continue
			}
			violations := EvaluateRule(hpa, rule)
			report.Violations = append(report.Violations, violations...)
		}
	}

	// Score deductions: critical=-20, warning=-10, info=0.
	for _, v := range report.Violations {
		switch v.Severity {
		case "critical":
			report.Score -= 20
		case "warning":
			report.Score -= 10
		}
	}
	if report.Score < 0 {
		report.Score = 0
	}

	report.Summary = buildPolicySummary(report)
	return report
}

// EvaluateRule evaluates a single policy rule against an HPA.
func EvaluateRule(hpa *autoscalingv2.HorizontalPodAutoscaler, rule PolicyRule) []PolicyViolation {
	rule = normalizePolicyRule(rule)
	ruleFunc, ok := builtinRules[rule.ID]
	if !ok {
		return []PolicyViolation{{
			RuleID:      rule.ID,
			RuleName:    rule.Name,
			Severity:    "info",
			Description: fmt.Sprintf("unknown policy rule %q; skipped", rule.ID),
		}}
	}

	violations := ruleFunc(hpa, rule.Parameters)

	// Override severity from the rule definition if specified.
	for i := range violations {
		if rule.Severity != "" {
			violations[i].Severity = rule.Severity
		}
	}

	return violations
}

func policySetMatches(hpa *autoscalingv2.HorizontalPodAutoscaler, set PolicySet) bool {
	for key, want := range set.Selector {
		if hpa.Labels[key] != want {
			return false
		}
	}
	return true
}

// LoadPolicyFile reads and validates a policy YAML file from the given path.
func LoadPolicyFile(path string) (PolicyFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PolicyFile{}, fmt.Errorf("reading policy file %s: %w", path, err)
	}

	var pf PolicyFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return PolicyFile{}, fmt.Errorf("parsing policy file %s: %w", path, err)
	}

	if err := pf.Validate(); err != nil {
		return PolicyFile{}, fmt.Errorf("validating policy file %s: %w", path, err)
	}

	return pf, nil
}
