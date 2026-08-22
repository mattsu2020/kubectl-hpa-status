package hpa

import (
	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
)

// AnalysisBuilder owns the mutable enrichment phase and guarantees that every
// result is finalized and its canonical projection is frozen exactly once.
type AnalysisBuilder struct {
	analysis Analysis
	built    bool
}

// NewAnalysisBuilder starts the mutable enrichment phase.
func NewAnalysisBuilder(analysis Analysis) *AnalysisBuilder {
	return &AnalysisBuilder{analysis: analysis}
}

// AddEnrichment attaches optional observations before finalization.
func (b *AnalysisBuilder) AddEnrichment(keda *hpakeda.Analysis, vpa *hpavpa.ConflictInfo, warnings []string, weights HealthWeights) *AnalysisBuilder {
	if b == nil || b.built {
		return b
	}
	b.analysis.Actions.Warnings = append(b.analysis.Actions.Warnings, warnings...)
	b.analysis.Controllers.KEDAInfo = keda
	b.analysis.Advisory.VPAConflict = vpa
	if keda != nil || vpa != nil {
		ApplyEnrichmentPenalties(&b.analysis, weights)
	}
	return b
}

// Build finalizes the analysis and returns both compatibility and canonical
// views. Calling Build again is safe and returns the same finalized state.
func (b *AnalysisBuilder) Build() (Analysis, StatusReport) {
	if b == nil {
		return Analysis{}, StatusReport{APIVersion: SchemaVersion}
	}
	if !b.built {
		b.analysis = FinalizeAnalysis(b.analysis)
		b.built = true
	}
	report := StatusReport{APIVersion: SchemaVersion, Analysis: b.analysis}
	report.FreezeCanonical()
	return b.analysis, report
}
