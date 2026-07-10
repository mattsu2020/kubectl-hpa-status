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
	//
	// Deprecated: Use vpa.RecommendationInfo instead.
	VPARecommendationInfo = vpa.RecommendationInfo
	// VPAInfo aliases vpa.Info.
	//
	// Deprecated: Use vpa.Info instead.
	VPAInfo = vpa.Info
	// VPAConflictInfo aliases vpa.ConflictInfo.
	//
	// Deprecated: Use vpa.ConflictInfo instead.
	VPAConflictInfo = vpa.ConflictInfo
	// VPARecommendation aliases vpa.Recommendation.
	//
	// Deprecated: Use vpa.Recommendation instead.
	VPARecommendation = vpa.Recommendation
	// VPAConflictLevel aliases vpa.ConflictLevel.
	//
	// Deprecated: Use vpa.ConflictLevel instead.
	VPAConflictLevel = vpa.ConflictLevel
	// VPAAdvisory aliases vpa.Advisory.
	//
	// Deprecated: Use vpa.Advisory instead.
	VPAAdvisory = vpa.Advisory
)

// VPA conflict level constants (aliases for the canonical vpa.* values).
//
// Deprecated: Use the canonical vpa.ConflictNone/ConflictWarning/ConflictError constants instead.
const (
	VPAConflictNone    = vpa.ConflictNone
	VPAConflictWarning = vpa.ConflictWarning
	VPAConflictError   = vpa.ConflictError
)

// AnalyzeVPA generates warning lines when VPA conflicts with HPA.
// Delegates to vpa.Analyze.
//
// Deprecated: Use vpa.Analyze instead.
func AnalyzeVPA(hpa *autoscalingv2.HorizontalPodAutoscaler, info *VPAInfo) []string {
	return vpa.Analyze(hpa, info)
}

// NewVPAConflictInfo converts extracted VPA data into the public analysis model.
// Delegates to vpa.NewConflictInfo.
//
// Deprecated: Use vpa.NewConflictInfo instead.
func NewVPAConflictInfo(info *VPAInfo) *VPAConflictInfo {
	return vpa.NewConflictInfo(info)
}

// AnalyzeVPAAdvisory produces a structured VPA-HPA coexistence advisory.
// Delegates to vpa.AnalyzeAdvisory.
//
// Deprecated: Use vpa.AnalyzeAdvisory instead.
func AnalyzeVPAAdvisory(hpa *autoscalingv2.HorizontalPodAutoscaler, info *VPAConflictInfo) *VPAAdvisory {
	return vpa.AnalyzeAdvisory(hpa, info)
}
