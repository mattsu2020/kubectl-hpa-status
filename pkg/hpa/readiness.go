package hpa

import "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/readiness"

// This file is a thin re-export facade for the readiness domain, which now
// lives in pkg/hpa/readiness. The types and functions below preserve the
// existing hpaanalysis.* API surface. The canonical implementations are in
// pkg/hpa/readiness/readiness.go.

// Readiness domain type aliases. Each alias preserves the historical
// hpaanalysis.Readiness* name while delegating to the canonical readiness.*
// type (prefix dropped to avoid stuttering).
type (
	// ReadinessImpact aliases readiness.Impact.
	//
	// Deprecated: Use readiness.Impact instead.
	ReadinessImpact = readiness.Impact
	// ReadinessDoctorReport aliases readiness.DoctorReport.
	//
	// Deprecated: Use readiness.DoctorReport instead.
	ReadinessDoctorReport = readiness.DoctorReport
	// ReadinessPodAgeDistribution aliases readiness.PodAgeDistribution.
	//
	// Deprecated: Use readiness.PodAgeDistribution instead.
	ReadinessPodAgeDistribution = readiness.PodAgeDistribution
	// ReadinessProbeAnalysis aliases readiness.ProbeAnalysis.
	//
	// Deprecated: Use readiness.ProbeAnalysis instead.
	ReadinessProbeAnalysis = readiness.ProbeAnalysis
	// ReadinessInitImpact aliases readiness.InitImpact.
	//
	// Deprecated: Use readiness.InitImpact instead.
	ReadinessInitImpact = readiness.InitImpact
	// ReadinessExclusionEstimate aliases readiness.ExclusionEstimate.
	//
	// Deprecated: Use readiness.ExclusionEstimate instead.
	ReadinessExclusionEstimate = readiness.ExclusionEstimate
	// ReadinessDoctorInput aliases readiness.DoctorInput.
	//
	// Deprecated: Use readiness.DoctorInput instead.
	ReadinessDoctorInput = readiness.DoctorInput
	// ReadinessDoctorPod aliases readiness.DoctorPod.
	//
	// Deprecated: Use readiness.DoctorPod instead.
	ReadinessDoctorPod = readiness.DoctorPod
)

// AnalyzeReadinessDoctor produces a focused readiness diagnostic report.
// Delegates to readiness.AnalyzeReadinessDoctor.
//
// Deprecated: Use readiness.AnalyzeReadinessDoctor instead.
func AnalyzeReadinessDoctor(input ReadinessDoctorInput) *ReadinessDoctorReport {
	return readiness.AnalyzeReadinessDoctor(input)
}
