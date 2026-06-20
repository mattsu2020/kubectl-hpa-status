// Package policy implements organizational policy guardrails for HPA
// configuration. It loads YAML policy files, evaluates built-in and custom
// rules against an HPA, and reports violations with suggested fixes. It is
// a self-contained domain depending only on standard library and
// autoscaling/v2 types. The cmd/ layer reaches it through the pkg/hpa
// re-export facade.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"sigs.k8s.io/yaml"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/suggestion"
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

// Violation represents a single policy violation found for an HPA.
type Violation struct {
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

// Report holds the result of evaluating policies against an HPA.
type Report struct {
	// Namespace is the HPA namespace.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA name.
	Name string `json:"name" yaml:"name"`
	// Score is the compliance score from 0 (worst) to 100 (fully compliant).
	Score int `json:"score" yaml:"score"`
	// Violations lists all policy violations found.
	Violations []Violation `json:"violations" yaml:"violations"`
	// Summary is a human-readable one-line summary.
	Summary string `json:"summary" yaml:"summary"`
}

// buildPolicySummary generates a one-line summary of the policy report.
func buildPolicySummary(report *Report) string {
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

// EvaluatePolicies evaluates all applicable policy rules against an HPA
// and returns a Report with violations and a compliance score.
func EvaluatePolicies(hpa *autoscalingv2.HorizontalPodAutoscaler, policyFile File) *Report {
	report := &Report{
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
func EvaluateRule(hpa *autoscalingv2.HorizontalPodAutoscaler, rule Rule) []Violation {
	rule = normalizePolicyRule(rule)
	ruleFunc, ok := builtinRules[rule.ID]
	if !ok {
		return []Violation{{
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

func policySetMatches(hpa *autoscalingv2.HorizontalPodAutoscaler, set Set) bool {
	for key, want := range set.Selector {
		if hpa.Labels[key] != want {
			return false
		}
	}
	return true
}

// LoadPolicyFile reads and validates a policy YAML file from the given path.
func LoadPolicyFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("reading policy file %s: %w", path, err)
	}

	var pf File
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return File{}, fmt.Errorf("parsing policy file %s: %w", path, err)
	}

	if err := pf.Validate(); err != nil {
		return File{}, fmt.Errorf("validating policy file %s: %w", path, err)
	}

	return pf, nil
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

// RuleFunc evaluates a single policy rule against an HPA.
// Returns zero or more violations if the HPA does not comply.
type RuleFunc func(hpa *autoscalingv2.HorizontalPodAutoscaler, params Params) []Violation

// builtinRules maps rule IDs to their evaluation functions.
var builtinRules = map[string]RuleFunc{
	"stabilization-window":      stabilizationWindowPolicy,
	"max-replicas-multiplier":   maxReplicasMultiplierPolicy,
	"max-replicas-from-current": maxReplicasFromCurrentPolicy,
	"behavior-policy-required":  behaviorPolicyRequiredPolicy,
	"metric-coverage":           metricCoveragePolicy,
	"target-utilization-range":  targetUtilizationRangePolicy,
	"replica-range":             replicaRangePolicy,
}

// StabilizationWindowPolicy checks that the scaleDown stabilization window
// is within the configured range.
//
// Parameters:
//   - min (int, default 60): minimum allowed stabilization window in seconds
//   - max (int, default 3600): maximum allowed stabilization window in seconds
func stabilizationWindowPolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params Params) []Violation {
	minSec := params.Int("min", 60)
	maxSec := params.Int("max", 3600)

	var window int32
	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
		window = *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	} else {
		// Kubernetes default is 300s.
		window = 300
	}

	if int(window) >= minSec && int(window) <= maxSec {
		return nil
	}

	current := fmt.Sprintf("%ds", window)
	required := fmt.Sprintf("%d-%ds", minSec, maxSec)

	return []Violation{
		{
			RuleID:      "stabilization-window",
			RuleName:    "Stabilization Window Range",
			Severity:    "warning",
			Description: fmt.Sprintf("scaleDown.stabilizationWindowSeconds is %ds, outside allowed range [%ds, %ds]", window, minSec, maxSec),
			Current:     current,
			Required:    required,
		},
	}
}

// MaxReplicasMultiplierPolicy checks that maxReplicas is at least N * minReplicas.
//
// Parameters:
//   - multiplier (int, default 3): minimum maxReplicas/minReplicas ratio
func maxReplicasMultiplierPolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params Params) []Violation {
	multiplier := params.Int("multiplier", 3)

	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	required := minReplicas * int32(multiplier)
	if hpa.Spec.MaxReplicas >= required {
		return nil
	}

	return []Violation{
		{
			RuleID:      "max-replicas-multiplier",
			RuleName:    "Max Replicas Multiplier",
			Severity:    "warning",
			Description: fmt.Sprintf("maxReplicas=%d is less than %d×minReplicas (%d×%d=%d)", hpa.Spec.MaxReplicas, multiplier, multiplier, minReplicas, required),
			Current:     fmt.Sprintf("%d", hpa.Spec.MaxReplicas),
			Required:    fmt.Sprintf(">= %d", required),
		},
	}
}

func maxReplicasFromCurrentPolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params Params) []Violation {
	multiplier := params.Int("maxMultiplierFromCurrent", 5)
	current := hpa.Status.CurrentReplicas
	if current < 1 {
		current = 1
	}
	allowed := current * int32(multiplier)
	if hpa.Spec.MaxReplicas <= allowed {
		return nil
	}
	return []Violation{
		{
			RuleID:      "max-replicas-from-current",
			RuleName:    "Max Replicas From Current",
			Severity:    "warning",
			Description: fmt.Sprintf("maxReplicas=%d exceeds %d×currentReplicas (%d×%d=%d)", hpa.Spec.MaxReplicas, multiplier, multiplier, current, allowed),
			Current:     fmt.Sprintf("%d", hpa.Spec.MaxReplicas),
			Required:    fmt.Sprintf("<= %d", allowed),
		},
	}
}

// BehaviorPolicyRequiredPolicy checks that explicit scaleUp and/or scaleDown
// policies are configured.
//
// Parameters:
//   - requireScaleUp (bool, default true): require explicit scaleUp policies
//   - requireScaleDown (bool, default true): require explicit scaleDown policies
func behaviorPolicyRequiredPolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params Params) []Violation {
	requireUp := params.Bool("requireScaleUp", true)
	requireDown := params.Bool("requireScaleDown", true)

	var violations []Violation

	if requireUp {
		hasScaleUp := hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleUp != nil && len(hpa.Spec.Behavior.ScaleUp.Policies) > 0
		if !hasScaleUp {
			violations = append(violations, Violation{
				RuleID:      "behavior-policy-required",
				RuleName:    "Behavior Policy Required",
				Severity:    "info",
				Description: "No explicit scaleUp policies configured; relying on Kubernetes defaults",
				Current:     "none",
				Required:    "explicit scaleUp policies recommended",
			})
		}
	}

	if requireDown {
		hasScaleDown := hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && len(hpa.Spec.Behavior.ScaleDown.Policies) > 0
		if !hasScaleDown {
			violations = append(violations, Violation{
				RuleID:      "behavior-policy-required",
				RuleName:    "Behavior Policy Required",
				Severity:    "info",
				Description: "No explicit scaleDown policies configured; relying on Kubernetes defaults",
				Current:     "none",
				Required:    "explicit scaleDown policies recommended",
			})
		}
	}

	return violations
}

// MetricCoveragePolicy checks that the HPA has the required metric types.
//
// Parameters:
//   - requireResource (bool, default true): require at least one Resource metric
//   - minMetrics (int, default 1): minimum number of metrics
func metricCoveragePolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params Params) []Violation {
	requireResource := params.Bool("requireResource", true)
	minMetrics := params.Int("minMetrics", 1)

	var violations []Violation

	if len(hpa.Spec.Metrics) < minMetrics {
		violations = append(violations, Violation{
			RuleID:      "metric-coverage",
			RuleName:    "Metric Coverage",
			Severity:    "warning",
			Description: fmt.Sprintf("HPA has %d metric(s), but at least %d are required", len(hpa.Spec.Metrics), minMetrics),
			Current:     fmt.Sprintf("%d metrics", len(hpa.Spec.Metrics)),
			Required:    fmt.Sprintf(">= %d metrics", minMetrics),
		})
		return violations
	}

	if requireResource {
		hasResource := false
		for _, m := range hpa.Spec.Metrics {
			if m.Type == autoscalingv2.ResourceMetricSourceType {
				hasResource = true
				break
			}
		}
		if !hasResource {
			violations = append(violations, Violation{
				RuleID:      "metric-coverage",
				RuleName:    "Metric Coverage",
				Severity:    "info",
				Description: "No Resource metric (cpu/memory) configured; consider adding one for reliable scaling",
				Current:     "no resource metrics",
				Required:    "at least one Resource metric recommended",
			})
		}
	}

	return violations
}

// TargetUtilizationRangePolicy checks that resource metric target utilization
// is within the allowed range.
//
// Parameters:
//   - min (int, default 30): minimum allowed target utilization percent
//   - max (int, default 90): maximum allowed target utilization percent
func targetUtilizationRangePolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params Params) []Violation {
	minPct := params.Int("min", 30)
	maxPct := params.Int("max", 90)

	var violations []Violation

	for _, m := range hpa.Spec.Metrics {
		if m.Type != autoscalingv2.ResourceMetricSourceType || m.Resource == nil {
			continue
		}
		if m.Resource.Target.Type != autoscalingv2.UtilizationMetricType || m.Resource.Target.AverageUtilization == nil {
			continue
		}
		util := int(*m.Resource.Target.AverageUtilization)
		name := strings.ToLower(string(m.Resource.Name))

		if util < minPct || util > maxPct {
			violations = append(violations, Violation{
				RuleID:      "target-utilization-range",
				RuleName:    "Target Utilization Range",
				Severity:    "warning",
				Description: fmt.Sprintf("%s target utilization is %d%%, outside allowed range [%d%%, %d%%]", name, util, minPct, maxPct),
				Current:     fmt.Sprintf("%d%%", util),
				Required:    fmt.Sprintf("%d%%-%d%%", minPct, maxPct),
			})
		}
	}

	return violations
}

// ReplicaRangePolicy checks that the maxReplicas/minReplicas ratio is within bounds.
//
// Parameters:
//   - maxRatio (int, default 10): maximum allowed maxReplicas/minReplicas ratio
func replicaRangePolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params Params) []Violation {
	maxRatio := params.Int("maxRatio", 10)

	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	if minReplicas == 0 {
		return nil
	}

	ratio := int(hpa.Spec.MaxReplicas) / int(minReplicas)
	if ratio <= maxRatio {
		return nil
	}

	return []Violation{
		{
			RuleID:      "replica-range",
			RuleName:    "Replica Range",
			Severity:    "warning",
			Description: fmt.Sprintf("maxReplicas/minReplicas ratio is %d (max=%d, min=%d), exceeds maximum allowed ratio of %d", ratio, hpa.Spec.MaxReplicas, minReplicas, maxRatio),
			Current:     fmt.Sprintf("ratio=%d", ratio),
			Required:    fmt.Sprintf("<= %d", maxRatio),
		},
	}
}
