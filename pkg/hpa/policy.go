package hpa

import (
	"fmt"
	"strings"
)

// PolicyRule defines a single organizational policy constraint for HPA configuration.
type PolicyRule struct {
	// ID is a unique identifier for this policy rule.
	ID string `json:"id" yaml:"id"`
	// Type is a short rule type alias accepted by policy-set YAML files.
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// Name is a human-readable name for this rule.
	Name string `json:"name" yaml:"name"`
	// Description explains what this rule enforces.
	Description string `json:"description" yaml:"description"`
	// Severity is the violation severity: "critical", "warning", or "info".
	Severity string `json:"severity" yaml:"severity"`
	// Enabled controls whether this rule is evaluated. nil defaults to true.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// Parameters holds rule-specific configuration.
	Parameters PolicyParams `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	// Common short-form parameters accepted directly on a rule.
	Min                      *int `json:"min,omitempty" yaml:"min,omitempty"`
	Max                      *int `json:"max,omitempty" yaml:"max,omitempty"`
	Multiplier               *int `json:"multiplier,omitempty" yaml:"multiplier,omitempty"`
	MaxRatio                 *int `json:"maxRatio,omitempty" yaml:"maxRatio,omitempty"`
	MaxMultiplierFromCurrent *int `json:"max_multiplier_from_current,omitempty" yaml:"max_multiplier_from_current,omitempty"`
}

// IsEnabled returns true if the rule is explicitly enabled or defaults to enabled.
func (r PolicyRule) IsEnabled() bool {
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// PolicyParams holds rule-specific parameters as a typed map.
type PolicyParams map[string]interface{}

// Int returns the parameter value as an int, or the default if missing or wrong type.
func (p PolicyParams) Int(key string, defaultVal int) int {
	v, ok := p[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return defaultVal
}

// String returns the parameter value as a string, or the default if missing.
func (p PolicyParams) String(key string, defaultVal string) string {
	v, ok := p[key]
	if !ok {
		return defaultVal
	}
	s, ok := v.(string)
	if !ok {
		return defaultVal
	}
	return s
}

// Bool returns the parameter value as a bool, or the default if missing.
func (p PolicyParams) Bool(key string, defaultVal bool) bool {
	v, ok := p[key]
	if !ok {
		return defaultVal
	}
	b, ok := v.(bool)
	if !ok {
		return defaultVal
	}
	return b
}

// PolicyFile represents the structure of a policy configuration file
// (e.g. ~/.kube/hpa-policies.yaml).
type PolicyFile struct {
	// APIVersion is the policy file format version (e.g. "hpa-status/v1").
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	// Rules lists the policy rules to evaluate.
	Rules []PolicyRule `json:"rules" yaml:"rules"`
	// Policies groups rules behind label selectors.
	Policies []PolicySet `json:"policies,omitempty" yaml:"policies,omitempty"`
}

// PolicySet groups rules under an optional exact-match label selector.
type PolicySet struct {
	Name     string            `json:"name" yaml:"name"`
	Selector map[string]string `json:"selector,omitempty" yaml:"selector,omitempty"`
	Rules    []PolicyRule      `json:"rules" yaml:"rules"`
}

// Validate checks the policy file for structural errors.
func (f PolicyFile) Validate() error {
	if f.APIVersion != "" && f.APIVersion != "hpa-status/v1" {
		return fmt.Errorf("unsupported apiVersion %q; supported: hpa-status/v1", f.APIVersion)
	}
	for _, rule := range f.allRules() {
		rule = normalizePolicyRule(rule)
		if rule.ID == "" {
			return fmt.Errorf("policy rule missing id")
		}
		if rule.Name == "" {
			return fmt.Errorf("policy rule %q missing name", rule.ID)
		}
		switch rule.Severity {
		case "critical", "warning", "info", "":
		default:
			return fmt.Errorf("policy rule %q has invalid severity %q; use critical, warning, or info", rule.ID, rule.Severity)
		}
	}
	return nil
}

func (f PolicyFile) allRules() []PolicyRule {
	rules := append([]PolicyRule{}, f.Rules...)
	for _, set := range f.Policies {
		rules = append(rules, set.Rules...)
	}
	return rules
}

func normalizePolicyRule(rule PolicyRule) PolicyRule {
	if rule.ID == "" {
		rule.ID = rule.Type
	}
	normalizePolicyRuleIDAndName(&rule)
	normalizePolicyRuleParameters(&rule)
	return rule
}

// normalizePolicyRuleIDAndName maps the rule's ID to a canonical form and fills in a default Name when unset.
func normalizePolicyRuleIDAndName(rule *PolicyRule) {
	switch normalizePolicyID(rule.ID) {
	case "stabilizationwindowseconds", "stabilizationwindow", "stabilization-window":
		rule.ID = "stabilization-window"
		if rule.Name == "" {
			rule.Name = "Stabilization Window Range"
		}
	case "maxreplicas", "maxreplicasmultiplier", "max-replicas-multiplier":
		normalizeMaxReplicasRule(rule)
	case "behaviorpolicyrequired", "behavior-policy-required":
		rule.ID = "behavior-policy-required"
		if rule.Name == "" {
			rule.Name = "Behavior Policy Required"
		}
	case "metriccoverage", "metric-coverage":
		rule.ID = "metric-coverage"
		if rule.Name == "" {
			rule.Name = "Metric Coverage"
		}
	case "targetutilizationrange", "target-utilization-range":
		rule.ID = "target-utilization-range"
		if rule.Name == "" {
			rule.Name = "Target Utilization Range"
		}
	case "replicarange", "replica-range":
		rule.ID = "replica-range"
		if rule.Name == "" {
			rule.Name = "Replica Range"
		}
	}
}

// normalizeMaxReplicasRule disambiguates maxReplicas rules based on whether a from-current multiplier is set.
func normalizeMaxReplicasRule(rule *PolicyRule) {
	if rule.MaxMultiplierFromCurrent != nil {
		rule.ID = "max-replicas-from-current"
		if rule.Name == "" {
			rule.Name = "Max Replicas From Current"
		}
		return
	}
	rule.ID = "max-replicas-multiplier"
	if rule.Name == "" {
		rule.Name = "Max Replicas Multiplier"
	}
}

// normalizePolicyRuleParameters ensures Parameters is non-nil and copies scalar fields into it.
func normalizePolicyRuleParameters(rule *PolicyRule) {
	if rule.Parameters == nil {
		rule.Parameters = PolicyParams{}
	}
	if rule.Min != nil {
		rule.Parameters["min"] = *rule.Min
	}
	if rule.Max != nil {
		rule.Parameters["max"] = *rule.Max
	}
	if rule.Multiplier != nil {
		rule.Parameters["multiplier"] = *rule.Multiplier
	}
	if rule.MaxRatio != nil {
		rule.Parameters["maxRatio"] = *rule.MaxRatio
	}
	if rule.MaxMultiplierFromCurrent != nil {
		rule.Parameters["maxMultiplierFromCurrent"] = *rule.MaxMultiplierFromCurrent
	}
}

func normalizePolicyID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	return value
}

// PolicyViolation represents a single policy violation found for an HPA.
type PolicyViolation struct {
	// RuleID is the ID of the violated rule.
	RuleID string `json:"ruleId" yaml:"ruleId"`
	// RuleName is the name of the violated rule.
	RuleName string `json:"ruleName" yaml:"ruleName"`
	// Severity is the violation severity: "critical", "warning", or "info".
	Severity string `json:"severity" yaml:"severity"`
	// Description explains the violation.
	Description string `json:"description" yaml:"description"`
	// Current shows the current HPA configuration value.
	Current string `json:"current" yaml:"current"`
	// Required shows the required policy value.
	Required string `json:"required" yaml:"required"`
	// FixPatch is a JSON merge patch to fix the violation, if applicable.
	FixPatch string `json:"fixPatch,omitempty" yaml:"fixPatch,omitempty"`
	// FixCommand is the kubectl command to apply the fix.
	FixCommand string `json:"fixCommand,omitempty" yaml:"fixCommand,omitempty"`
}

// PolicyReport holds the result of evaluating policies against an HPA.
type PolicyReport struct {
	// Namespace is the HPA namespace.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA name.
	Name string `json:"name" yaml:"name"`
	// Score is the compliance score from 0 (worst) to 100 (fully compliant).
	Score int `json:"score" yaml:"score"`
	// Violations lists all policy violations found.
	Violations []PolicyViolation `json:"violations" yaml:"violations"`
	// Summary is a human-readable one-line summary.
	Summary string `json:"summary" yaml:"summary"`
}

// buildPolicySummary generates a one-line summary of the policy report.
func buildPolicySummary(report *PolicyReport) string {
	var counts [3]int // critical, warning, info
	for _, v := range report.Violations {
		switch v.Severity {
		case "critical":
			counts[0]++
		case "warning":
			counts[1]++
		case "info":
			counts[2]++
		}
	}
	parts := make([]string, 0, 3)
	if counts[0] > 0 {
		parts = append(parts, fmt.Sprintf("%d critical", counts[0]))
	}
	if counts[1] > 0 {
		parts = append(parts, fmt.Sprintf("%d warnings", counts[1]))
	}
	if counts[2] > 0 {
		parts = append(parts, fmt.Sprintf("%d informational", counts[2]))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("All policies passed (score: %d/100)", report.Score)
	}
	return fmt.Sprintf("Found %s (score: %d/100)", strings.Join(parts, ", "), report.Score)
}
