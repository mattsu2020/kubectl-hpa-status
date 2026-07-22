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
	// Deprecated: Use readiness.Impact instead. Scheduled for removal in v3.0.0.
	ReadinessImpact = readiness.Impact
	// ReadinessDoctorReport aliases readiness.DoctorReport.
	//
	// Deprecated: Use readiness.DoctorReport instead. Scheduled for removal in v3.0.0.
	ReadinessDoctorReport = readiness.DoctorReport
	// ReadinessPodAgeDistribution aliases readiness.PodAgeDistribution.
	//
	// Deprecated: Use readiness.PodAgeDistribution instead. Scheduled for removal in v3.0.0.
	ReadinessPodAgeDistribution = readiness.PodAgeDistribution
	// ReadinessProbeAnalysis aliases readiness.ProbeAnalysis.
	//
	// Deprecated: Use readiness.ProbeAnalysis instead. Scheduled for removal in v3.0.0.
	ReadinessProbeAnalysis = readiness.ProbeAnalysis
	// ReadinessInitImpact aliases readiness.InitImpact.
	//
	// Deprecated: Use readiness.InitImpact instead. Scheduled for removal in v3.0.0.
	ReadinessInitImpact = readiness.InitImpact
	// ReadinessExclusionEstimate aliases readiness.ExclusionEstimate.
	//
	// Deprecated: Use readiness.ExclusionEstimate instead. Scheduled for removal in v3.0.0.
	ReadinessExclusionEstimate = readiness.ExclusionEstimate
	// ReadinessDoctorInput aliases readiness.DoctorInput.
	//
	// Deprecated: Use readiness.DoctorInput instead. Scheduled for removal in v3.0.0.
	ReadinessDoctorInput = readiness.DoctorInput
	// ReadinessDoctorPod aliases readiness.DoctorPod.
	//
	// Deprecated: Use readiness.DoctorPod instead. Scheduled for removal in v3.0.0.
	ReadinessDoctorPod = readiness.DoctorPod
)

// AnalyzeReadinessDoctor produces a focused readiness diagnostic report.
// Delegates to readiness.AnalyzeReadinessDoctor.
//
// Deprecated: Use readiness.AnalyzeReadinessDoctor instead. Scheduled for removal in v3.0.0.
func AnalyzeReadinessDoctor(input ReadinessDoctorInput) *ReadinessDoctorReport {
	return readiness.AnalyzeReadinessDoctor(input)
}
