package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
)

// This file is a thin re-export facade for the VPA conflict-analysis domain,
// which now lives in pkg/hpa/vpa. The types and functions below preserve the
// existing hpaanalysis.* API surface so cmd/ and internal/ callers keep
// compiling without changing their imports. The canonical implementations are
// in pkg/hpa/vpa/vpa.go.

// VPA domain type aliases. The canonical types live in pkg/hpa/vpa without the
// VPA prefix (e.g. vpa.ConflictInfo); these aliases preserve the historical
// hpaanalysis.VPA* names so existing callers keep compiling.
type (
	// VPARecommendationInfo aliases vpa.RecommendationInfo.
	VPARecommendationInfo = vpa.RecommendationInfo
	// VPAInfo aliases vpa.Info.
	VPAInfo = vpa.Info
	// VPAConflictInfo aliases vpa.ConflictInfo.
	VPAConflictInfo = vpa.ConflictInfo
	// VPARecommendation aliases vpa.Recommendation.
	VPARecommendation = vpa.Recommendation
	// VPAConflictLevel aliases vpa.ConflictLevel.
	VPAConflictLevel = vpa.ConflictLevel
	// VPAAdvisory aliases vpa.Advisory.
	VPAAdvisory = vpa.Advisory
)

// VPA conflict level constants (aliases for the canonical vpa.* values).
const (
	VPAConflictNone    = vpa.ConflictNone
	VPAConflictWarning = vpa.ConflictWarning
	VPAConflictError   = vpa.ConflictError
)

// AnalyzeVPA generates warning lines when VPA conflicts with HPA.
// Delegates to vpa.Analyze.
func AnalyzeVPA(hpa *autoscalingv2.HorizontalPodAutoscaler, info *VPAInfo) []string {
	return vpa.Analyze(hpa, info)
}

// NewVPAConflictInfo converts extracted VPA data into the public analysis model.
// Delegates to vpa.NewConflictInfo.
func NewVPAConflictInfo(info *VPAInfo) *VPAConflictInfo {
	return vpa.NewConflictInfo(info)
}

// AnalyzeVPAAdvisory produces a structured VPA-HPA coexistence advisory.
// Delegates to vpa.AnalyzeAdvisory.
func AnalyzeVPAAdvisory(hpa *autoscalingv2.HorizontalPodAutoscaler, info *VPAConflictInfo) *VPAAdvisory {
	return vpa.AnalyzeAdvisory(hpa, info)
}
