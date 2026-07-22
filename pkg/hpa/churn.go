package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
)

// This file is a thin re-export facade for the churn domain, which now lives
// in pkg/hpa/churn. The types and functions below preserve the existing
// hpaanalysis.* API surface. The canonical implementations are in
// pkg/hpa/churn/churn.go. The churn_text.go renderer stays in pkg/hpa because
// it shares the labels machinery.

// Churn domain type aliases.
type (
	// ChurnLevel aliases churn.ChurnLevel.
	//
	// Deprecated: Use churn.ChurnLevel instead. Scheduled for removal in v3.0.0.
	ChurnLevel = churn.ChurnLevel
	// ChurnAnalysis aliases churn.ChurnAnalysis.
	//
	// Deprecated: Use churn.ChurnAnalysis instead. Scheduled for removal in v3.0.0.
	ChurnAnalysis = churn.ChurnAnalysis
	// ChurnRecommendation aliases churn.ChurnRecommendation.
	//
	// Deprecated: Use churn.ChurnRecommendation instead. Scheduled for removal in v3.0.0.
	ChurnRecommendation = churn.ChurnRecommendation
)

// Churn level constants.
//
// Deprecated: Use the canonical churn.ChurnLow/ChurnMedium/ChurnHigh/ChurnCritical constants instead. Scheduled for removal in v3.0.0.
const (
	ChurnLow      = churn.ChurnLow
	ChurnMedium   = churn.ChurnMedium
	ChurnHigh     = churn.ChurnHigh
	ChurnCritical = churn.ChurnCritical
)

// AnalyzeChurnFromEvents detects thrashing/churn from HPA events. Delegates
// to churn.AnalyzeChurnFromEvents.
//
// Deprecated: Use churn.AnalyzeChurnFromEvents instead. Scheduled for removal in v3.0.0.
func AnalyzeChurnFromEvents(events []Event, hpa *autoscalingv2.HorizontalPodAutoscaler) *ChurnAnalysis {
	return churn.AnalyzeChurnFromEvents(events, hpa)
}

// AnalyzeChurnFromSnapshots detects churn from timeline snapshots. Converts
// snapshots to rescale data, then delegates to churn.AnalyzeFromRescales.
func AnalyzeChurnFromSnapshots(snapshots []TimelineSnapshot, hpa *autoscalingv2.HorizontalPodAutoscaler) *ChurnAnalysis {
	rescales := make([]event.RescaleData, 0, len(snapshots))
	for _, snap := range snapshots {
		rescales = append(rescales, event.RescaleData{
			Timestamp: snap.Timestamp,
			NewSize:   snap.Desired,
		})
	}
	return churn.AnalyzeFromRescales(rescales, hpa)
}
