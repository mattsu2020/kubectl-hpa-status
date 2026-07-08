// Package audit runs best-practice configuration audits against an HPA,
// producing a scored Report with actionable findings. It is a self-contained
// domain depending only on autoscaling/v2 types plus the shared
// pkg/hpa/internal/{util,conditions} helpers. The cmd/ layer reaches it
// through the pkg/hpa re-export facade (hpaanalysis.AuditHPA, etc.). The
// *_text.go renderer stays in pkg/hpa because it shares the labels machinery.
package audit

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// healthScoreMax is the starting audit score; findings deduct from it.
// Mirrors pkg/hpa.healthScoreMax (unexported there). Keep in sync.
const healthScoreMax = 100

// Severity represents the severity of an audit finding.
type Severity string

const (
	// AuditCritical indicates a critical finding requiring immediate attention.
	AuditCritical Severity = "critical"
	// AuditWarning indicates a finding that warrants operator attention.
	AuditWarning Severity = "warning"
	// AuditInfo indicates an informational finding or best-practice suggestion.
	AuditInfo Severity = "info"
)

// Finding represents a single best-practice audit finding.
type Finding struct {
	// ID is a unique identifier for the audit rule that produced this finding.
	ID string `json:"id" yaml:"id"`
	// Title is a short description of the finding.
	Title string `json:"title" yaml:"title"`
	// Description provides detailed context about the finding.
	Description string `json:"description" yaml:"description"`
	// Severity is the severity level: critical, warning, or info.
	Severity Severity `json:"severity" yaml:"severity"`
	// Category groups related findings (e.g. "stabilization", "replica-range").
	Category string `json:"category" yaml:"category"`
	// Current shows the current configuration value.
	Current string `json:"current,omitempty" yaml:"current,omitempty"`
	// Recommended shows the recommended configuration value.
	Recommended string `json:"recommended,omitempty" yaml:"recommended,omitempty"`
	// Patch is a JSON merge patch to fix the finding, if applicable.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Command is the kubectl command to apply the patch.
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// Risk indicates the risk level of applying the patch.
	Risk string `json:"risk,omitempty" yaml:"risk,omitempty"`
	// References lists URLs or docs for further reading.
	References []string `json:"references,omitempty" yaml:"references,omitempty"`
}

// Profile represents a workload profile that adjusts audit rule thresholds.
type Profile string

const (
	// ProfileLatency optimizes for low-latency workloads: fast scale-up, slow scale-down.
	ProfileLatency Profile = "latency"
	// ProfileCost optimizes for cost efficiency: low minReplicas, aggressive scale-down.
	ProfileCost Profile = "cost"
	// ProfileBatch is for batch workloads: high CPU tolerance, no urgent scale-up.
	ProfileBatch Profile = "batch"
	// ProfileKEDA is for KEDA-managed workloads: scale-to-zero, trigger/cooldown focus.
	ProfileKEDA Profile = "keda"
	// ProfileCritical is for critical workloads: maxReplicas headroom, capacity checks.
	ProfileCritical Profile = "critical"
)

// Report holds the complete audit result for an HPA.
type Report struct {
	// Namespace is the HPA namespace.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// Score is the compliance score from 0 (worst) to 100 (fully compliant).
	Score int `json:"score" yaml:"score"`
	// Findings lists all audit findings.
	Findings []Finding `json:"findings" yaml:"findings"`
	// Summary is a human-readable one-line summary of the audit.
	Summary string `json:"summary" yaml:"summary"`
	// Profile indicates the workload profile used for threshold adjustments, if any.
	Profile Profile `json:"profile,omitempty" yaml:"profile,omitempty"`
}

// Rule examines an HPA for best-practice compliance and returns findings.
type Rule func(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding

// Run runs all audit rules against the HPA and returns a compliance report.
func Run(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) *Report {
	return RunWithProfile(hpa, minReplicas, "")
}

// RunWithProfile runs all audit rules against the HPA using the given
// workload profile to adjust thresholds. An empty profile uses defaults.
func RunWithProfile(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, profile Profile) *Report {
	if hpa == nil {
		return &Report{Score: 0, Summary: "HPA is nil"}
	}

	report := &Report{
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
		Target:    fmt.Sprintf("%s/%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name),
		Score:     healthScoreMax, // start at 100
		Profile:   profile,
	}

	for _, rule := range coreRulesWithProfile(profile) {
		findings := rule(hpa, minReplicas)
		report.Findings = append(report.Findings, findings...)
	}

	// Calculate score based on findings
	for _, f := range report.Findings {
		switch f.Severity {
		case AuditCritical:
			report.Score -= 20
		case AuditWarning:
			report.Score -= 10
			// AuditInfo: no deduction
		}
	}
	if report.Score < 0 {
		report.Score = 0
	}

	report.Summary = buildAuditSummary(report)
	return report
}

// coreRules returns the ordered list of best-practice audit rules.
func coreRules() []Rule {
	return coreRulesWithProfile("")
}

// coreRulesWithProfile returns the base audit rules plus any
// profile-specific rules that apply for the given workload profile.
func coreRulesWithProfile(profile Profile) []Rule {
	base := []Rule{
		stabilizationWindowRule,
		replicaRangeRule,
		behaviorPolicyRule,
		metricCoverageRule,
		toleranceRule,
		scaleToZeroRule,
		resourceRequestRule,
		kedaRule,
		targetUtilizationRule,
	}

	profileRules := profileSpecificRules(profile)
	return append(base, profileRules...)
}

func buildAuditSummary(report *Report) string {
	critical := 0
	warning := 0
	info := 0
	for _, f := range report.Findings {
		switch f.Severity {
		case AuditCritical:
			critical++
		case AuditWarning:
			warning++
		case AuditInfo:
			info++
		}
	}
	if len(report.Findings) == 0 {
		return "No best-practice issues found."
	}
	return fmt.Sprintf("Found %d critical, %d warnings, %d informational findings (score: %d/100)", critical, warning, info, report.Score)
}

// ---------------------------------------------------------------------------
// Profile-specific audit rules
// ---------------------------------------------------------------------------

// profileSpecificRules returns audit rules that apply only when a specific
// workload profile is selected. Each rule applies profile-adjusted thresholds.
func profileSpecificRules(profile Profile) []Rule {
	switch profile {
	case ProfileLatency:
		return []Rule{
			latencyStabilizationRule,
			latencyScaleUpPolicyRule,
		}
	case ProfileCost:
		return []Rule{
			costMinReplicasRule,
			costScaleDownRule,
		}
	case ProfileBatch:
		return []Rule{
			batchToleranceRule,
		}
	case ProfileKEDA:
		return []Rule{
			kedaScaleToZeroRule,
			kedaCooldownRule,
		}
	case ProfileCritical:
		return []Rule{
			criticalMaxHeadroomRule,
			criticalMinReplicasRule,
		}
	default:
		return nil
	}
}
