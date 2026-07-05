// Package lint runs static-analysis checks against an HPA manifest,
// reporting findings (severity, rule, message, recommendation) as a
// Result. It is a self-contained leaf domain depending only on
// autoscaling/v2 types. The cmd/ layer reaches it through the pkg/hpa
// re-export facade (hpaanalysis.Run, hpaanalysis.Result, etc.).
package lint

import (
	"encoding/json"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

// Severity represents the severity of a lint finding.
type Severity string

const (
	// Error indicates a finding that makes the HPA configuration invalid or
	// non-functional (e.g. minReplicas > maxReplicas).
	Error Severity = "error"
	// Warning indicates a finding that the HPA will function but the
	// configuration is risky or suboptimal (e.g. no scaleDown behavior).
	Warning Severity = "warning"
	// Info indicates an informational finding that may be of interest but
	// does not require action (e.g. only one metric configured).
	Info Severity = "info"
)

// Finding represents a single lint finding.
type Finding struct {
	Severity       Severity `json:"severity" yaml:"severity"`
	Rule           string   `json:"rule" yaml:"rule"`
	Message        string   `json:"message" yaml:"message"`
	Recommendation string   `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	AutoFix        *AutoFix `json:"autoFix,omitempty" yaml:"autoFix,omitempty"`
}

// Result holds the output of a lint run.
type Result struct {
	Findings []Finding `json:"findings" yaml:"findings"`
	Errors   int       `json:"errors" yaml:"errors"`
	Warnings int       `json:"warnings" yaml:"warnings"`
	Infos    int       `json:"infos" yaml:"infos"`
	Pass     bool      `json:"pass" yaml:"pass"`
}

// lintRule is the signature of every offline check. Rules are gathered into
// the slice in Run and executed in order; each returns its findings (which may
// be nil/empty). The rule functions themselves live in lint_rules.go.
type lintRule func(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding

// Run runs all offline lint rules against an HPA manifest and returns findings.
// This function works without cluster connection — it only inspects the manifest.
func Run(hpa *autoscalingv2.HorizontalPodAutoscaler) *Result {
	if hpa == nil {
		return &Result{
			Findings: []Finding{{
				Severity: Error,
				Rule:     "nil-hpa",
				Message:  "HPA manifest is nil or empty",
			}},
			Errors: 1,
			Pass:   false,
		}
	}

	rules := []lintRule{
		lintReplicaRange,
		lintMinGreaterThanMax,
		lintMinEqualsMax,
		lintMissingCPURequest,
		lintMultiContainerResource,
		lintNoScaleDownBehavior,
		lintHighUtilizationTarget,
		lintSingleMetric,
		lintNoResourceMetrics,
		lintStabilizationWindow,
		lintTolerance,
		lintScaleToZero,
		lintKEDAStyle,
	}

	var allFindings []Finding
	for _, rule := range rules {
		findings := rule(hpa)
		allFindings = append(allFindings, findings...)
	}

	result := &Result{Findings: allFindings}
	for _, f := range allFindings {
		switch f.Severity {
		case Error:
			result.Errors++
		case Warning:
			result.Warnings++
		case Info:
			result.Infos++
		}
	}
	result.Pass = result.Errors == 0
	return result
}

// AutoFix describes a proposed auto-fix for a lint finding.
type AutoFix struct {
	// Patch is a JSON merge patch string that would fix the issue.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Command is a kubectl patch command to apply the fix.
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// Before describes the current state in human-readable form.
	Before string `json:"before,omitempty" yaml:"before,omitempty"`
	// After describes the desired state in human-readable form.
	After string `json:"after,omitempty" yaml:"after,omitempty"`
	// Risk indicates the risk level of applying this fix.
	Risk string `json:"risk,omitempty" yaml:"risk,omitempty"`
}

// generateAutoFix produces an auto-fix proposal for a given lint rule and HPA.
// Returns nil if the rule is not auto-fixable.
func generateAutoFix(rule string, hpa *autoscalingv2.HorizontalPodAutoscaler) *AutoFix {
	switch rule {
	case "behavior-scaledown":
		return fixMissingScaleDownBehavior(hpa)
	case "target-utilization":
		return fixHighUtilizationTarget(hpa)
	case "tolerance":
		return fixTightTolerance(hpa)
	case "stabilization-window":
		return fixLongStabilizationWindow(hpa)
	default:
		return nil
	}
}

// buildAutoFix marshals the patch and assembles an AutoFix whose Command is a
// kubectl patch suggestion (no dry-run flag, matching the historical lint
// output). Returns nil when the patch cannot be marshaled, which lets each
// caller's early-return path treat a bad patch like "no autofix available".
func buildAutoFix(hpa *autoscalingv2.HorizontalPodAutoscaler, patch map[string]any, before, after, risk string) *AutoFix {
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil
	}
	return &AutoFix{
		Patch:   string(patchJSON),
		Command: util.KubectlPatchCommandWithDryRun(hpa, string(patchJSON), util.DryRunNone),
		Before:  before,
		After:   after,
		Risk:    risk,
	}
}
