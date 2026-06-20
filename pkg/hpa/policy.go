package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/suggestion"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/policy"
)

// This file is a thin re-export facade for the policy domain, which now lives
// in pkg/hpa/policy. The types and functions below preserve the existing
// hpaanalysis.* API surface. The canonical implementations are in
// pkg/hpa/policy/policy.go (types renamed to drop the Policy prefix to avoid
// stuttering). The policy_guard_text.go renderer stays in pkg/hpa because it
// shares the labels machinery.

// Policy domain type aliases.
type (
	// PolicyRule aliases policy.Rule.
	PolicyRule = policy.Rule
	// PolicyParams aliases policy.Params.
	PolicyParams = policy.Params
	// PolicyFile aliases policy.File.
	PolicyFile = policy.File
	// PolicySet aliases policy.Set.
	PolicySet = policy.Set
	// PolicyViolation aliases policy.Violation.
	PolicyViolation = policy.Violation
	// PolicyReport aliases policy.Report.
	PolicyReport = policy.Report
)

// EvaluatePolicies evaluates all policy rules against the HPA. Delegates to
// policy.EvaluatePolicies.
func EvaluatePolicies(hpa *autoscalingv2.HorizontalPodAutoscaler, policyFile PolicyFile) *PolicyReport {
	return policy.EvaluatePolicies(hpa, policyFile)
}

// EvaluateRule evaluates a single policy rule against the HPA. Delegates to
// policy.EvaluateRule.
func EvaluateRule(hpa *autoscalingv2.HorizontalPodAutoscaler, rule PolicyRule) []PolicyViolation {
	return policy.EvaluateRule(hpa, rule)
}

// LoadPolicyFile loads a policy YAML file. Delegates to policy.LoadPolicyFile.
func LoadPolicyFile(path string) (PolicyFile, error) {
	return policy.LoadPolicyFile(path)
}

// GuardFix checks suggestions against the policy guard. Delegates to
// policy.GuardFix.
func GuardFix(suggestions []Suggestion, policyFile PolicyFile, hpa *autoscalingv2.HorizontalPodAutoscaler) *GuardResult {
	return policy.GuardFix(suggestions, policyFile, hpa)
}

// Ensure the suggestion import is used even if the alias is the only consumer.
var _ = suggestion.Suggestion{}
