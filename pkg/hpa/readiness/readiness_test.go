package readiness

import (
	"strings"
	"testing"
)

func TestAnalyzeReadinessDoctor(t *testing.T) {
	tests := []struct {
		name  string
		input DoctorInput
		check func(t *testing.T, report *DoctorReport)
	}{
		{
			name: "all pods healthy with probes",
			input: DoctorInput{
				Namespace: "prod",
				HPAName:   "web",
				Target:    "Deployment/web",
				PodDetails: []DoctorPod{
					{Name: "web-1", Ready: true, AgeSeconds: 600},
					{Name: "web-2", Ready: true, AgeSeconds: 600},
				},
				HasStartupProbe:       true,
				HasReadinessProbe:     true,
				ReadinessInitialDelay: 10,
				CPUInitPeriodSeconds:  300,
				InitialReadinessDelay: 30,
			},
			check: func(t *testing.T, report *DoctorReport) {
				if report.PodAgeDistribution.YoungPods != 0 {
					t.Errorf("expected 0 young pods, got %d", report.PodAgeDistribution.YoungPods)
				}
				if report.ProbeAnalysis.HasStartupProbe != true {
					t.Error("expected startupProbe to be detected")
				}
				if report.InitializationImpact.EstimatedExcludedPods != 0 {
					t.Errorf("expected 0 excluded pods, got %d", report.InitializationImpact.EstimatedExcludedPods)
				}
				if len(report.Recommendations) != 0 {
					t.Errorf("expected no recommendations, got %d", len(report.Recommendations))
				}
				if !strings.Contains(report.Summary, "unlikely") {
					t.Errorf("expected summary to say 'unlikely', got: %s", report.Summary)
				}
			},
		},
		{
			name: "young pods present without startupProbe",
			input: DoctorInput{
				Namespace: "prod",
				HPAName:   "web",
				Target:    "Deployment/web",
				PodDetails: []DoctorPod{
					{Name: "web-1", Ready: true, AgeSeconds: 60},
					{Name: "web-2", Ready: false, AgeSeconds: 30},
					{Name: "web-3", Ready: true, AgeSeconds: 600},
					{Name: "web-4", Ready: false, AgeSeconds: 90},
					{Name: "web-5", Ready: true, AgeSeconds: 120},
				},
				HasStartupProbe:       false,
				HasReadinessProbe:     true,
				ReadinessInitialDelay: 5,
				CPUInitPeriodSeconds:  300,
				InitialReadinessDelay: 30,
			},
			check: func(t *testing.T, report *DoctorReport) {
				if report.PodAgeDistribution.YoungPods != 4 {
					t.Errorf("expected 4 young pods, got %d", report.PodAgeDistribution.YoungPods)
				}
				if report.PodAgeDistribution.NotReadyYoungPods != 2 {
					t.Errorf("expected 2 not-ready young pods, got %d", report.PodAgeDistribution.NotReadyYoungPods)
				}
				if report.ProbeAnalysis.HasStartupProbe {
					t.Error("expected startupProbe to be absent")
				}
				if report.InitializationImpact.EstimatedExcludedPods != 4 {
					t.Errorf("expected 4 excluded pods, got %d", report.InitializationImpact.EstimatedExcludedPods)
				}
				foundStartupRec := false
				for _, rec := range report.Recommendations {
					if strings.Contains(rec, "startupProbe") {
						foundStartupRec = true
					}
				}
				if !foundStartupRec {
					t.Error("expected recommendation to mention startupProbe")
				}
				if !strings.Contains(report.Summary, "likely") {
					t.Errorf("expected summary to say 'likely', got: %s", report.Summary)
				}
			},
		},
		{
			name: "no probes configured with young pods",
			input: DoctorInput{
				Namespace: "prod",
				HPAName:   "web",
				Target:    "Deployment/web",
				PodDetails: []DoctorPod{
					{Name: "web-1", Ready: false, AgeSeconds: 45},
					{Name: "web-2", Ready: false, AgeSeconds: 90},
					{Name: "web-3", Ready: true, AgeSeconds: 500},
				},
				HasStartupProbe:       false,
				HasReadinessProbe:     false,
				CPUInitPeriodSeconds:  300,
				InitialReadinessDelay: 30,
			},
			check: func(t *testing.T, report *DoctorReport) {
				if report.ProbeAnalysis.HasReadinessProbe {
					t.Error("expected readinessProbe to be absent")
				}
				recText := strings.Join(report.Recommendations, " ")
				if !strings.Contains(recText, "readinessProbe") {
					t.Error("expected recommendation to mention readinessProbe")
				}
				if !strings.Contains(recText, "startupProbe") {
					t.Error("expected recommendation to mention startupProbe")
				}
			},
		},
		{
			name: "high readiness initial delay",
			input: DoctorInput{
				Namespace: "prod",
				HPAName:   "web",
				Target:    "Deployment/web",
				PodDetails: []DoctorPod{
					{Name: "web-1", Ready: true, AgeSeconds: 600},
				},
				HasStartupProbe:       true,
				HasReadinessProbe:     true,
				ReadinessInitialDelay: 120,
				CPUInitPeriodSeconds:  300,
				InitialReadinessDelay: 30,
			},
			check: func(t *testing.T, report *DoctorReport) {
				warningText := strings.Join(report.ProbeAnalysis.Warnings, " ")
				if !strings.Contains(warningText, "initialDelaySeconds") {
					t.Errorf("expected warning about initialDelaySeconds, got: %v", report.ProbeAnalysis.Warnings)
				}
				recText := strings.Join(report.Recommendations, " ")
				if !strings.Contains(recText, "reducing") {
					t.Errorf("expected recommendation about reducing delay, got: %v", report.Recommendations)
				}
			},
		},
		{
			name: "empty pod list",
			input: DoctorInput{
				Namespace:             "prod",
				HPAName:               "web",
				Target:                "Deployment/web",
				PodDetails:            nil,
				HasStartupProbe:       true,
				HasReadinessProbe:     true,
				CPUInitPeriodSeconds:  300,
				InitialReadinessDelay: 30,
			},
			check: func(t *testing.T, report *DoctorReport) {
				if report.PodAgeDistribution.TotalPods != 0 {
					t.Errorf("expected 0 total pods, got %d", report.PodAgeDistribution.TotalPods)
				}
				if report.ExclusionEstimate.NotReadyPods != 0 {
					t.Errorf("expected 0 not-ready pods, got %d", report.ExclusionEstimate.NotReadyPods)
				}
			},
		},
		{
			name: "missing metric pods counted",
			input: DoctorInput{
				Namespace: "prod",
				HPAName:   "web",
				Target:    "Deployment/web",
				PodDetails: []DoctorPod{
					{Name: "web-1", Ready: true, AgeSeconds: 600},
					{Name: "web-2", Ready: true, AgeSeconds: 600},
					{Name: "web-3", Ready: true, AgeSeconds: 600},
				},
				HasStartupProbe:       true,
				HasReadinessProbe:     true,
				CPUInitPeriodSeconds:  300,
				InitialReadinessDelay: 30,
				MissingMetricPods:     2,
			},
			check: func(t *testing.T, report *DoctorReport) {
				if report.ExclusionEstimate.MissingMetricPods != 2 {
					t.Errorf("expected 2 missing metric pods, got %d", report.ExclusionEstimate.MissingMetricPods)
				}
				if report.ExclusionEstimate.EstimatedExcludedCount != 2 {
					t.Errorf("expected 2 estimated excluded, got %d", report.ExclusionEstimate.EstimatedExcludedCount)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := AnalyzeReadinessDoctor(tt.input)
			if report == nil {
				t.Fatal("expected non-nil report")
			}
			tt.check(t, report)
		})
	}
}
