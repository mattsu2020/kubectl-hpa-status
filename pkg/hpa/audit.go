package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
)

// This file is a thin re-export facade for the audit domain, which now lives
// in pkg/hpa/audit. The types and functions below preserve the existing
// hpaanalysis.* API surface so cmd/ and internal/ callers keep compiling
// without changing their imports. The canonical implementations are in
// pkg/hpa/audit/audit.go (types renamed to drop the Audit prefix to avoid
// stuttering: audit.Severity, audit.Finding, etc.). The audit_text.go
// renderer stays in pkg/hpa because it shares the labels machinery.

// Audit domain type aliases.
type (
	// AuditRule aliases audit.Rule.
	AuditRule = audit.Rule
	// AuditFinding aliases audit.Finding.
	AuditFinding = audit.Finding
	// AuditSeverity aliases audit.Severity.
	AuditSeverity = audit.Severity
	// AuditProfile aliases audit.Profile.
	AuditProfile = audit.Profile
	// AuditReport aliases audit.Report.
	AuditReport = audit.Report
)

// Audit severity constants. Both the prefixed form (AuditSeverityCritical)
// and the short form (AuditCritical) are aliased for backwards compatibility.
const (
	AuditSeverityCritical = audit.AuditCritical
	AuditSeverityWarning  = audit.AuditWarning
	AuditSeverityInfo     = audit.AuditInfo
	AuditCritical         = audit.AuditCritical
	AuditWarning          = audit.AuditWarning
	AuditInfo             = audit.AuditInfo
)

// Audit profile constants. Both forms are aliased for backwards compatibility.
const (
	AuditProfileLatency  = audit.ProfileLatency
	AuditProfileCost     = audit.ProfileCost
	AuditProfileBatch    = audit.ProfileBatch
	AuditProfileKEDA     = audit.ProfileKEDA
	AuditProfileCritical = audit.ProfileCritical
	ProfileLatency       = audit.ProfileLatency
	ProfileCost          = audit.ProfileCost
	ProfileBatch         = audit.ProfileBatch
	ProfileKEDA          = audit.ProfileKEDA
	ProfileCritical      = audit.ProfileCritical
)

// AuditHPA runs the core audit rules and returns a Report.
// Delegates to audit.Run.
func AuditHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) *AuditReport {
	return audit.Run(hpa, minReplicas)
}

// AuditHPAWithProfile runs the core audit rules plus profile-specific rules.
// Delegates to audit.RunWithProfile.
func AuditHPAWithProfile(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, profile AuditProfile) *AuditReport {
	return audit.RunWithProfile(hpa, minReplicas, profile)
}
