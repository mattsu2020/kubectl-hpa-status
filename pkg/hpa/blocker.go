package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
)

// This file is a thin re-export facade for the blocker domain, which now lives
// in pkg/hpa/blocker. The types and functions below preserve the existing
// hpaanalysis.* API surface so cmd/ and internal/ callers keep compiling
// without changing their imports. The canonical implementations are in
// pkg/hpa/blocker/blocker.go (types renamed to drop the Blocker prefix to
// avoid stuttering: blocker.Severity, blocker.Finding, etc.). The
// blocker_text.go renderer stays in pkg/hpa because it shares the labels
// machinery.

// Blocker domain type aliases. Each alias preserves the historical
// hpaanalysis.* name while delegating to the canonical blocker.* type.
type (
	// BlockerSeverity aliases blocker.Severity.
	BlockerSeverity = blocker.Severity
	// BlockerFinding aliases blocker.Finding.
	BlockerFinding = blocker.Finding
	// BlockerReport aliases blocker.Report.
	BlockerReport = blocker.Report
	// BlockerInput aliases blocker.Input.
	BlockerInput = blocker.Input
	// BlockerPodInfo aliases blocker.PodInfo.
	BlockerPodInfo = blocker.PodInfo
	// BlockerQuotaInfo aliases blocker.QuotaInfo.
	BlockerQuotaInfo = blocker.QuotaInfo
)

// Non-prefixed type aliases for types that never had the Blocker prefix.
type (
	// ContainerStatusSummary aliases blocker.ContainerStatusSummary.
	ContainerStatusSummary = blocker.ContainerStatusSummary
	// NodeCapacitySummary aliases blocker.NodeCapacitySummary.
	NodeCapacitySummary = blocker.NodeCapacitySummary
)

// Blocker severity constants.
const (
	BlockerHigh   = blocker.BlockerHigh
	BlockerMedium = blocker.BlockerMedium
	BlockerInfo   = blocker.BlockerInfo
)

// AnalyzeBlockers detects scale-out blockers. Delegates to blocker.AnalyzeBlockers.
func AnalyzeBlockers(input BlockerInput) *BlockerReport {
	return blocker.AnalyzeBlockers(input)
}
