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
	// Deprecated: Use vpa.RecommendationInfo instead. Scheduled for removal in v3.0.0.
	VPARecommendationInfo = vpa.RecommendationInfo
	// VPAInfo aliases vpa.Info.
	//
	// Deprecated: Use vpa.Info instead. Scheduled for removal in v3.0.0.
	VPAInfo = vpa.Info
	// VPAConflictInfo aliases vpa.ConflictInfo.
	//
	// Deprecated: Use vpa.ConflictInfo instead. Scheduled for removal in v3.0.0.
	VPAConflictInfo = vpa.ConflictInfo
	// VPARecommendation aliases vpa.Recommendation.
	//
	// Deprecated: Use vpa.Recommendation instead. Scheduled for removal in v3.0.0.
	VPARecommendation = vpa.Recommendation
	// VPAConflictLevel aliases vpa.ConflictLevel.
	//
	// Deprecated: Use vpa.ConflictLevel instead. Scheduled for removal in v3.0.0.
	VPAConflictLevel = vpa.ConflictLevel
	// VPAAdvisory aliases vpa.Advisory.
	//
	// Deprecated: Use vpa.Advisory instead. Scheduled for removal in v3.0.0.
	VPAAdvisory = vpa.Advisory
)

// VPA conflict level constants (aliases for the canonical vpa.* values).
//
// Deprecated: Use the canonical vpa.ConflictNone/ConflictWarning/ConflictError constants instead. Scheduled for removal in v3.0.0.
const (
	VPAConflictNone    = vpa.ConflictNone
	VPAConflictWarning = vpa.ConflictWarning
	VPAConflictError   = vpa.ConflictError
)

// AnalyzeVPA generates warning lines when VPA conflicts with HPA.
// Delegates to vpa.Analyze.
//
// Deprecated: Use vpa.Analyze instead. Scheduled for removal in v3.0.0.
func AnalyzeVPA(hpa *autoscalingv2.HorizontalPodAutoscaler, info *VPAInfo) []string {
	return vpa.Analyze(hpa, info)
}

// NewVPAConflictInfo converts extracted VPA data into the public analysis model.
// Delegates to vpa.NewConflictInfo.
//
// Deprecated: Use vpa.NewConflictInfo instead. Scheduled for removal in v3.0.0.
func NewVPAConflictInfo(info *VPAInfo) *VPAConflictInfo {
	return vpa.NewConflictInfo(info)
}

// AnalyzeVPAAdvisory produces a structured VPA-HPA coexistence advisory.
// Delegates to vpa.AnalyzeAdvisory.
//
// Deprecated: Use vpa.AnalyzeAdvisory instead. Scheduled for removal in v3.0.0.
func AnalyzeVPAAdvisory(hpa *autoscalingv2.HorizontalPodAutoscaler, info *VPAConflictInfo) *VPAAdvisory {
	return vpa.AnalyzeAdvisory(hpa, info)
}
