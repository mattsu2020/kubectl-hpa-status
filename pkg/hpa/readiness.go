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
	ReadinessImpact = readiness.Impact
	// ReadinessDoctorReport aliases readiness.DoctorReport.
	ReadinessDoctorReport = readiness.DoctorReport
	// ReadinessPodAgeDistribution aliases readiness.PodAgeDistribution.
	ReadinessPodAgeDistribution = readiness.PodAgeDistribution
	// ReadinessProbeAnalysis aliases readiness.ProbeAnalysis.
	ReadinessProbeAnalysis = readiness.ProbeAnalysis
	// ReadinessInitImpact aliases readiness.InitImpact.
	ReadinessInitImpact = readiness.InitImpact
	// ReadinessExclusionEstimate aliases readiness.ExclusionEstimate.
	ReadinessExclusionEstimate = readiness.ExclusionEstimate
	// ReadinessDoctorInput aliases readiness.DoctorInput.
	ReadinessDoctorInput = readiness.DoctorInput
	// ReadinessDoctorPod aliases readiness.DoctorPod.
	ReadinessDoctorPod = readiness.DoctorPod
)

// AnalyzeReadinessDoctor produces a focused readiness diagnostic report.
// Delegates to readiness.AnalyzeReadinessDoctor.
func AnalyzeReadinessDoctor(input ReadinessDoctorInput) *ReadinessDoctorReport {
	return readiness.AnalyzeReadinessDoctor(input)
}
