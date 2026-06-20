// Package readiness analyzes pod readiness impact on HPA decisions:
// not-yet-ready pods, missing PodMetrics, and initialization delays that
// make the HPA controller's view differ from the visible status. It is a
// self-contained leaf domain depending only on standard library types.
package readiness

import (
	"fmt"
	"strings"
)

// Impact summarizes visible pod readiness and metrics gaps that may
// make HPA controller decisions differ from status.currentMetrics.
type Impact struct {
	LikelyAffected          bool     `json:"likelyAffected" yaml:"likelyAffected"`
	TotalPods               int32    `json:"totalPods" yaml:"totalPods"`
	NotYetReadyPods         int32    `json:"notYetReadyPods" yaml:"notYetReadyPods"`
	MissingMetricPods       int32    `json:"missingMetricPods,omitempty" yaml:"missingMetricPods,omitempty"`
	InitialReadinessDelay   string   `json:"initialReadinessDelay" yaml:"initialReadinessDelay"`
	CPUInitializationPeriod string   `json:"cpuInitializationPeriod" yaml:"cpuInitializationPeriod"`
	PossibleEffects         []string `json:"possibleEffects,omitempty" yaml:"possibleEffects,omitempty"`
	Evidence                []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	NextChecks              []string `json:"nextChecks,omitempty" yaml:"nextChecks,omitempty"`
}

// ---------------------------------------------------------------------------
// Readiness Doctor types (readiness doctor command)
// ---------------------------------------------------------------------------

// DoctorReport holds the focused readiness diagnostic for an HPA
// scale target, covering pod age distribution, probe configuration, CPU
// initialization window impact, and metric exclusion estimates.
type DoctorReport struct {
	Namespace            string             `json:"namespace" yaml:"namespace"`
	Name                 string             `json:"name" yaml:"name"`
	Target               string             `json:"target" yaml:"target"`
	Summary              string             `json:"summary" yaml:"summary"`
	PodAgeDistribution   PodAgeDistribution `json:"podAgeDistribution" yaml:"podAgeDistribution"`
	ProbeAnalysis        ProbeAnalysis      `json:"probeAnalysis" yaml:"probeAnalysis"`
	InitializationImpact InitImpact         `json:"initializationImpact" yaml:"initializationImpact"`
	ExclusionEstimate    ExclusionEstimate  `json:"exclusionEstimate" yaml:"exclusionEstimate"`
	Recommendations      []string           `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	NextChecks           []string           `json:"nextChecks,omitempty" yaml:"nextChecks,omitempty"`
}

// PodAgeDistribution summarizes pod age across the scale target.
type PodAgeDistribution struct {
	TotalPods         int32 `json:"totalPods" yaml:"totalPods"`
	YoungPods         int32 `json:"youngPods" yaml:"youngPods"`
	MaturePods        int32 `json:"maturePods" yaml:"maturePods"`
	ReadyYoungPods    int32 `json:"readyYoungPods" yaml:"readyYoungPods"`
	NotReadyYoungPods int32 `json:"notReadyYoungPods" yaml:"notReadyYoungPods"`
}

// ProbeAnalysis evaluates probe configuration on the pod template.
type ProbeAnalysis struct {
	HasStartupProbe          bool     `json:"hasStartupProbe" yaml:"hasStartupProbe"`
	HasReadinessProbe        bool     `json:"hasReadinessProbe" yaml:"hasReadinessProbe"`
	ReadinessInitialDelaySec int32    `json:"readinessInitialDelaySec" yaml:"readinessInitialDelaySec"`
	StartupMaxDelaySec       int32    `json:"startupMaxDelaySec,omitempty" yaml:"startupMaxDelaySec,omitempty"`
	Assessment               string   `json:"assessment" yaml:"assessment"`
	Warnings                 []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// InitImpact estimates how the CPU initialization window affects HPA.
type InitImpact struct {
	CPUInitPeriodSeconds  int32  `json:"cpuInitPeriodSeconds" yaml:"cpuInitPeriodSeconds"`
	InitialReadinessDelay int32  `json:"initialReadinessDelaySeconds" yaml:"initialReadinessDelaySeconds"`
	EstimatedExcludedPods int32  `json:"estimatedExcludedPods" yaml:"estimatedExcludedPods"`
	ImpactDescription     string `json:"impactDescription" yaml:"impactDescription"`
}

// ExclusionEstimate estimates pods excluded from HPA metric calculation.
type ExclusionEstimate struct {
	NotReadyPods           int32  `json:"notReadyPods" yaml:"notReadyPods"`
	MissingMetricPods      int32  `json:"missingMetricPods" yaml:"missingMetricPods"`
	EstimatedExcludedCount int32  `json:"estimatedExcludedCount" yaml:"estimatedExcludedCount"`
	Explanation            string `json:"explanation" yaml:"explanation"`
}

// DoctorInput is assembled by the cmd layer from Kubernetes API data.
type DoctorInput struct {
	Namespace             string
	HPAName               string
	Target                string
	PodDetails            []DoctorPod
	HasStartupProbe       bool
	HasReadinessProbe     bool
	ReadinessInitialDelay int32
	StartupMaxDelay       int32
	CPUInitPeriodSeconds  int32
	InitialReadinessDelay int32
	MissingMetricPods     int32
}

// DoctorPod describes a single pod for readiness analysis.
type DoctorPod struct {
	Name       string
	Ready      bool
	AgeSeconds int64
}

// AnalyzeReadinessDoctor produces a focused readiness diagnostic report
// from the provided input. It analyzes pod age distribution, probe
// configuration, CPU initialization window impact, and metric exclusion
// estimates to surface issues that may cause HPA to behave unexpectedly.
func AnalyzeReadinessDoctor(input DoctorInput) *DoctorReport {
	if input.CPUInitPeriodSeconds == 0 {
		input.CPUInitPeriodSeconds = 300 // default 5m
	}
	if input.InitialReadinessDelay == 0 {
		input.InitialReadinessDelay = 30 // default 30s
	}

	report := &DoctorReport{
		Namespace:            input.Namespace,
		Name:                 input.HPAName,
		Target:               input.Target,
		PodAgeDistribution:   analyzePodAgeDistribution(input),
		ProbeAnalysis:        analyzeReadinessProbeConfig(input),
		InitializationImpact: analyzeInitializationImpact(input),
		ExclusionEstimate:    analyzeExclusionEstimate(input),
	}

	report.Recommendations = buildReadinessDoctorRecommendations(report, input)
	report.NextChecks = buildReadinessDoctorNextChecks(input)
	report.Summary = buildReadinessDoctorSummary(report)

	return report
}

// analyzePodAgeDistribution categorizes pods as young (age < cpuInitPeriod)
// or mature, with readiness breakdown for young pods.
func analyzePodAgeDistribution(input DoctorInput) PodAgeDistribution {
	dist := PodAgeDistribution{
		TotalPods: int32(len(input.PodDetails)),
	}

	for _, pod := range input.PodDetails {
		if pod.AgeSeconds < int64(input.CPUInitPeriodSeconds) {
			dist.YoungPods++
			if pod.Ready {
				dist.ReadyYoungPods++
			} else {
				dist.NotReadyYoungPods++
			}
		} else {
			dist.MaturePods++
		}
	}

	return dist
}

// analyzeReadinessProbeConfig evaluates probe settings and produces warnings.
func analyzeReadinessProbeConfig(input DoctorInput) ProbeAnalysis {
	analysis := ProbeAnalysis{
		HasStartupProbe:          input.HasStartupProbe,
		HasReadinessProbe:        input.HasReadinessProbe,
		ReadinessInitialDelaySec: input.ReadinessInitialDelay,
		StartupMaxDelaySec:       input.StartupMaxDelay,
	}

	var warnings []string
	var issues []string

	if !input.HasReadinessProbe {
		issues = append(issues, "no readinessProbe configured")
		warnings = append(warnings, "Without readinessProbe, kubelet uses PodReady=true immediately on container start. HPA may count pods as ready before they can serve traffic.")
	}

	if !input.HasStartupProbe {
		issues = append(issues, "no startupProbe configured")
		warnings = append(warnings, "Without startupProbe, slow-starting applications (JVM, large frameworks) may fail readiness checks during warm-up, causing HPA to miscount available pods.")
	}

	if input.HasReadinessProbe && input.ReadinessInitialDelay > 60 {
		issues = append(issues, fmt.Sprintf("readinessProbe initialDelaySeconds=%d is high", input.ReadinessInitialDelay))
		warnings = append(warnings, fmt.Sprintf("A high initialDelaySeconds (%ds) delays pod readiness, potentially causing HPA to undercount available capacity.", input.ReadinessInitialDelay))
	}

	analysis.Warnings = warnings

	switch {
	case len(issues) == 0:
		analysis.Assessment = "Probe configuration looks healthy."
	case len(issues) == 1:
		analysis.Assessment = fmt.Sprintf("Minor issue: %s.", issues[0])
	default:
		analysis.Assessment = fmt.Sprintf("Multiple issues: %s.", strings.Join(issues, "; "))
	}

	return analysis
}

// analyzeInitializationImpact estimates how many pods fall within the CPU
// initialization window and are likely excluded from metric calculations.
func analyzeInitializationImpact(input DoctorInput) InitImpact {
	impact := InitImpact{
		CPUInitPeriodSeconds:  input.CPUInitPeriodSeconds,
		InitialReadinessDelay: input.InitialReadinessDelay,
	}

	youngCount := int32(0)
	for _, pod := range input.PodDetails {
		if pod.AgeSeconds < int64(input.CPUInitPeriodSeconds) {
			youngCount++
		}
	}
	impact.EstimatedExcludedPods = youngCount

	switch {
	case youngCount == 0:
		impact.ImpactDescription = "No pods are in the CPU initialization window."
	case youngCount <= 2:
		impact.ImpactDescription = fmt.Sprintf("%d pod(s) are in the CPU initialization window (%ds). CPU metrics from these pods may be ignored.", youngCount, input.CPUInitPeriodSeconds)
	default:
		impact.ImpactDescription = fmt.Sprintf("%d pods are in the CPU initialization window (%ds). CPU metrics from these pods are likely ignored, which can suppress scale-up decisions.", youngCount, input.CPUInitPeriodSeconds)
	}

	return impact
}

// analyzeExclusionEstimate computes the total estimated excluded pods
// from not-ready and missing-metrics pods.
func analyzeExclusionEstimate(input DoctorInput) ExclusionEstimate {
	estimate := ExclusionEstimate{
		MissingMetricPods: input.MissingMetricPods,
	}

	notReady := int32(0)
	for _, pod := range input.PodDetails {
		if !pod.Ready {
			notReady++
		}
	}
	estimate.NotReadyPods = notReady
	estimate.EstimatedExcludedCount = notReady + input.MissingMetricPods

	var parts []string
	if notReady > 0 {
		parts = append(parts, fmt.Sprintf("%d not-ready pods may be excluded from metric calculation", notReady))
	}
	if input.MissingMetricPods > 0 {
		parts = append(parts, fmt.Sprintf("%d pods have no visible metrics in PodMetrics API", input.MissingMetricPods))
	}

	if len(parts) == 0 {
		estimate.Explanation = "No pods appear to be excluded from HPA metric calculation."
	} else {
		estimate.Explanation = strings.Join(parts, "; ") + "."
	}

	return estimate
}

// buildReadinessDoctorRecommendations generates actionable recommendations.
func buildReadinessDoctorRecommendations(report *DoctorReport, input DoctorInput) []string {
	var recommendations []string

	if !input.HasStartupProbe && report.PodAgeDistribution.YoungPods > 0 {
		recommendations = append(recommendations,
			"Add startupProbe for JVM warm-up period or slow-starting containers to prevent readiness flapping during initialization.")
	}

	if !input.HasReadinessProbe {
		recommendations = append(recommendations,
			"Add readinessProbe to ensure pods are only counted as ready when they can serve traffic.")
	}

	if input.HasReadinessProbe && input.ReadinessInitialDelay > 60 {
		recommendations = append(recommendations,
			fmt.Sprintf("Consider reducing readinessProbe initialDelaySeconds from %ds — high delays postpone metric availability.", input.ReadinessInitialDelay))
	}

	if report.InitializationImpact.EstimatedExcludedPods > int32(len(input.PodDetails))/2 {
		recommendations = append(recommendations,
			"More than half of pods are in the CPU initialization window. Consider delaying the next deployment or reducing rollout velocity.")
	}

	if report.ExclusionEstimate.NotReadyPods > 0 && report.PodAgeDistribution.YoungPods > 0 {
		recommendations = append(recommendations,
			"Delay readiness until warm-up CPU spike is over to avoid HPA miscounting available capacity.")
	}

	return recommendations
}

// buildReadinessDoctorNextChecks generates kubectl commands for follow-up.
func buildReadinessDoctorNextChecks(input DoctorInput) []string {
	return []string{
		fmt.Sprintf("kubectl get pod -n %s -l <scale-target-selector> -o wide", input.Namespace),
		fmt.Sprintf("kubectl top pod -n %s -l <scale-target-selector>", input.Namespace),
	}
}

// buildReadinessDoctorSummary generates a one-line overall assessment.
func buildReadinessDoctorSummary(report *DoctorReport) string {
	parts := []string{}

	if report.PodAgeDistribution.YoungPods > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d pods younger than CPU init window",
			report.PodAgeDistribution.YoungPods, report.PodAgeDistribution.TotalPods))
	}

	if report.ExclusionEstimate.NotReadyPods > 0 {
		parts = append(parts, fmt.Sprintf("%d pods NotReady", report.ExclusionEstimate.NotReadyPods))
	}

	if !report.ProbeAnalysis.HasStartupProbe {
		parts = append(parts, "startupProbe not configured")
	}

	if !report.ProbeAnalysis.HasReadinessProbe {
		parts = append(parts, "readinessProbe not configured")
	}

	if len(parts) == 0 {
		return "Readiness impact: unlikely — all pods are mature and probes are configured."
	}

	return "Readiness impact: likely — " + strings.Join(parts, "; ") + "."
}
