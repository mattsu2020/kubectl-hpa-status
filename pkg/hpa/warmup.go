package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/warmup"
)

// This file is a thin re-export facade for the warmup domain, which now lives
// in pkg/hpa/warmup. The types and functions below preserve the existing
// hpaanalysis.* API surface. The canonical implementations are in
// pkg/hpa/warmup/warmup.go (types renamed to drop the Warmup prefix to avoid
// stuttering: warmup.Analysis, warmup.Bottleneck, etc.). The warmup_text.go
// renderer stays in pkg/hpa because it shares the labels machinery.

// Warmup domain type aliases.
type (
	// WarmupAnalysis aliases warmup.Analysis.
	WarmupAnalysis = warmup.Analysis
	// WarmupBottleneck aliases warmup.Bottleneck.
	WarmupBottleneck = warmup.Bottleneck
	// WarmupPodDetail aliases warmup.PodDetail.
	WarmupPodDetail = warmup.PodDetail
	// WarmupInput aliases warmup.Input.
	WarmupInput = warmup.Input
	// WarmupEventInfo aliases warmup.EventInfo.
	WarmupEventInfo = warmup.EventInfo
)

// AnalyzeWarmup analyzes pod warmup bottlenecks. Delegates to warmup.AnalyzeWarmup.
func AnalyzeWarmup(input WarmupInput) *WarmupAnalysis {
	return warmup.AnalyzeWarmup(input)
}
