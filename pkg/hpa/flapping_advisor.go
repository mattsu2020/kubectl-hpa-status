package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/flapping"
)

// This file is a thin re-export facade for the flapping domain, which now
// lives in pkg/hpa/flapping. The types and functions below preserve the
// existing hpaanalysis.* API surface. The canonical implementations are in
// pkg/hpa/flapping/flapping.go (types renamed to drop the Flapping prefix to
// avoid stuttering). The flapping_advisor_text.go and flapping_text.go
// renderers stay in pkg/hpa because they share the labels machinery.

// Flapping domain type aliases. The canonical types in pkg/hpa/flapping drop
// the Flapping prefix (e.g. flapping.Diagnosis); these aliases preserve the
// historical hpaanalysis.Flapping* names.
type (
	// FlappingPreventionReport aliases flapping.PreventionReport.
	FlappingPreventionReport = flapping.PreventionReport
	// FlappingSimulation aliases flapping.Simulation.
	FlappingSimulation = flapping.Simulation
	// FlappingDiagnosis aliases flapping.Diagnosis.
	FlappingDiagnosis = flapping.Diagnosis
	// FlappingCause aliases flapping.Cause.
	FlappingCause = flapping.Cause
	// FlappingFix aliases flapping.Fix.
	FlappingFix = flapping.Fix
	// AnomalyType aliases flapping.AnomalyType.
	AnomalyType = flapping.AnomalyType
	// AnomalyDetection aliases flapping.AnomalyDetection.
	AnomalyDetection = flapping.AnomalyDetection
)

// Flapping anomaly type constants.
const (
	AnomalySuddenDegradation     = flapping.AnomalySuddenDegradation
	AnomalyStuckState            = flapping.AnomalyStuckState
	AnomalyOscillationEscalation = flapping.AnomalyOscillationEscalation
)

// DiagnoseFlapping detects scaling flapping. Delegates to
// flapping.DiagnoseFlapping.
func DiagnoseFlapping(events []Event, hpa *autoscalingv2.HorizontalPodAutoscaler) *FlappingDiagnosis {
	return flapping.DiagnoseFlapping(events, hpa)
}

// AnalyzeFlappingPrevention analyzes whether the HPA's behavior policies
// would prevent detected flapping. Delegates to flapping.AnalyzeFlappingPrevention.
func AnalyzeFlappingPrevention(events []Event, hpa *autoscalingv2.HorizontalPodAutoscaler) *FlappingPreventionReport {
	return flapping.AnalyzeFlappingPrevention(events, hpa)
}
