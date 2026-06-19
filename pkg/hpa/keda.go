package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
)

// This file is a thin re-export facade for the KEDA domain, which now lives in
// pkg/hpa/keda. The types and AnalyzeKEDA function below preserve the existing
// hpaanalysis.* API surface so cmd/ and internal/ callers keep compiling
// without changing their imports. The canonical implementations are in
// pkg/hpa/keda/keda.go.
//
// TargetReplicaInfo stays here (not in pkg/hpa/keda) because it describes the
// HPA scale target resource, not KEDA-specific state.

// KEDAAnalysis is a type alias for keda.Analysis, the canonical KEDA summary
// type. It preserves the historical hpaanalysis.KEDAAnalysis name.
type KEDAAnalysis = keda.Analysis

// KEDATriggerSummary is a type alias for keda.TriggerSummary.
type KEDATriggerSummary = keda.TriggerSummary

// KEDAFallbackInfo is a type alias for keda.FallbackInfo.
type KEDAFallbackInfo = keda.FallbackInfo

// AnalyzeKEDA produces interpretation lines that cross-reference an HPA with
// its KEDA ScaledObject. Delegates to keda.Analyze.
func AnalyzeKEDA(hpa *autoscalingv2.HorizontalPodAutoscaler, k *KEDAAnalysis) []string {
	return keda.Analyze(hpa, k)
}

// TargetReplicaInfo holds replica status from the scale target resource.
// When not-ready pods exist, HPA scaling calculations may be affected.
// Kept in pkg/hpa (not keda) because it describes the scale target, not KEDA.
type TargetReplicaInfo struct {
	TotalReplicas int32 `json:"totalReplicas" yaml:"totalReplicas"`
	ReadyReplicas int32 `json:"readyReplicas" yaml:"readyReplicas"`
	NotReady      int32 `json:"notReady" yaml:"notReady"`
	Pending       int32 `json:"pending,omitempty" yaml:"pending,omitempty"`
	Unschedulable int32 `json:"unschedulable,omitempty" yaml:"unschedulable,omitempty"`
}
