// Package policy implements organizational policy guardrails for HPA
// configuration. It loads YAML policy files, evaluates built-in and custom
// rules against an HPA, and reports violations with suggested fixes. It is
// a self-contained domain depending only on standard library and
// autoscaling/v2 types. The cmd/ layer reaches it through the pkg/hpa
// re-export facade.
package policy

import (
	"fmt"
	"strings"
)

// Rule defines a single organizational policy constraint for HPA configuration.
type Rule struct {
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
	Parameters Params `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	// Common short-form parameters accepted directly on a rule.
	Min                      *int `json:"min,omitempty" yaml:"min,omitempty"`
	Max                      *int `json:"max,omitempty" yaml:"max,omitempty"`
	Multiplier               *int `json:"multiplier,omitempty" yaml:"multiplier,omitempty"`
	MaxRatio                 *int `json:"maxRatio,omitempty" yaml:"maxRatio,omitempty"`
	MaxMultiplierFromCurrent *int `json:"max_multiplier_from_current,omitempty" yaml:"max_multiplier_from_current,omitempty"`
}

// IsEnabled returns true if the rule is explicitly enabled or defaults to enabled.
func (r Rule) IsEnabled() bool {
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// Params holds rule-specific parameters as a typed map.
type Params map[string]any

// Int returns the parameter value as an int, or the default if missing or wrong type.
func (p Params) Int(key string, defaultVal int) int {
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
func (p Params) String(key string, defaultVal string) string {
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
func (p Params) Bool(key string, defaultVal bool) bool {
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

// File represents the structure of a policy configuration file
// (e.g. ~/.kube/hpa-policies.yaml).
type File struct {
	// APIVersion is the policy file format version (e.g. "hpa-status/v1").
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	// Rules lists the policy rules to evaluate.
	Rules []Rule `json:"rules" yaml:"rules"`
	// Policies groups rules behind label selectors.
	Policies []Set `json:"policies,omitempty" yaml:"policies,omitempty"`
}

// Set groups rules under an optional exact-match label selector.
type Set struct {
	Name     string            `json:"name" yaml:"name"`
	Selector map[string]string `json:"selector,omitempty" yaml:"selector,omitempty"`
	Rules    []Rule            `json:"rules" yaml:"rules"`
}

// Validate checks the policy file for structural errors.
func (f File) Validate() error {
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

func (f File) allRules() []Rule {
	rules := append([]Rule{}, f.Rules...)
	for _, set := range f.Policies {
		rules = append(rules, set.Rules...)
	}
	return rules
}

func normalizePolicyRule(rule Rule) Rule {
	if rule.ID == "" {
		rule.ID = rule.Type
	}
	normalizePolicyRuleIDAndName(&rule)
	normalizePolicyRuleParameters(&rule)
	return rule
}

// normalizePolicyRuleIDAndName maps the rule's ID to a canonical form and fills in a default Name when unset.
func normalizePolicyRuleIDAndName(rule *Rule) {
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
func normalizeMaxReplicasRule(rule *Rule) {
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
func normalizePolicyRuleParameters(rule *Rule) {
	if rule.Parameters == nil {
		rule.Parameters = Params{}
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
