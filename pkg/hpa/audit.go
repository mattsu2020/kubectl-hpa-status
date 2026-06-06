package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// AuditRule examines an HPA for best-practice compliance and returns findings.
type AuditRule func(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []AuditFinding

// AuditHPA runs all audit rules against the HPA and returns a compliance report.
func AuditHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) *AuditReport {
	if hpa == nil {
		return &AuditReport{Score: 0, Summary: "HPA is nil"}
	}

	report := &AuditReport{
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
		Target:    fmt.Sprintf("%s/%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name),
		Score:     healthScoreMax, // start at 100
	}

	for _, rule := range coreAuditRules() {
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

// coreAuditRules returns the ordered list of best-practice audit rules.
func coreAuditRules() []AuditRule {
	return []AuditRule{
		stabilizationWindowAuditRule,
		replicaRangeAuditRule,
		behaviorPolicyAuditRule,
		metricCoverageAuditRule,
		toleranceAuditRule,
		scaleToZeroAuditRule,
		resourceRequestAuditRule,
		kedaAuditRule,
		targetUtilizationAuditRule,
	}
}

func buildAuditSummary(report *AuditReport) string {
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
