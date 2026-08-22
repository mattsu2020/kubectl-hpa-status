// Package enrichment provides KEDA and VPA enrichment logic for HPA analysis.
// It encapsulates CRD detection, dynamic client creation, and batched
// enrichment operations, decoupled from CLI flag handling.
package enrichment

import (
	"context"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// EnrichReport applies KEDA and VPA enrichment to a StatusReport and
// adjusts the health score with enrichment penalties.
func EnrichReport(ctx context.Context, ec *Context, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, weights hpaanalysis.HealthWeights) {
	if ec == nil {
		return
	}
	status := ec.status.Clone()

	if ec.kedaEnabled {
		var outcome Entry
		kedaInfo, outcome := enrichKEDA(ctx, ec, hpa)
		report.Analysis.Controllers.KEDAInfo = kedaInfo
		status.KEDA = &outcome
		if outcome.State == StateError {
			report.Analysis.Actions.Warnings = append(report.Analysis.Actions.Warnings, "KEDA enrichment failed: "+outcome.Reason)
		}
	}

	if ec.vpaEnabled {
		outcome := EnrichVPA(ctx, ec, hpa, report)
		status.VPA = &outcome
		if outcome.State == StateError {
			report.Analysis.Actions.Warnings = append(report.Analysis.Actions.Warnings, "VPA enrichment failed: "+outcome.Reason)
		}
	}

	if report.Analysis.Controllers.KEDAInfo != nil || report.Analysis.Advisory.VPAConflict != nil {
		hpaanalysis.ApplyEnrichmentPenalties(&report.Analysis, weights)
	}

	// Attach enrichment status to analysis for diagnostic output.
	report.Analysis.Lifecycle.EnrichmentStatus = &status
}
