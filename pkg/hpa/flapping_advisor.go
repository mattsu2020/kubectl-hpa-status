package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/flapping"
)

// This file re-exports the subset of the flapping domain (which now lives in
// pkg/hpa/flapping) that this package still uses as its own vocabulary: the
// Analysis struct's FlappingPrevention/FlappingDiagnosis fields and the
// anomaly detection types/constants in health_trend_anomaly.go. The
// flapping_advisor_text.go and flapping_text.go renderers stay in pkg/hpa
// because they share the labels machinery.
type (
	// FlappingPreventionReport aliases flapping.PreventionReport.
	FlappingPreventionReport = flapping.PreventionReport
	// FlappingDiagnosis aliases flapping.Diagnosis.
	FlappingDiagnosis = flapping.Diagnosis
	// AnomalyDetection aliases flapping.AnomalyDetection.
	AnomalyDetection = flapping.AnomalyDetection
)

// Flapping anomaly type constants, aliased from pkg/hpa/flapping.
const (
	AnomalySuddenDegradation     = flapping.AnomalySuddenDegradation
	AnomalyStuckState            = flapping.AnomalyStuckState
	AnomalyOscillationEscalation = flapping.AnomalyOscillationEscalation
)
