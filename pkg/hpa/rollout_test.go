package hpa

import (
	"strings"
	"testing"
)

func TestAnalyzeRolloutNoRollout(t *testing.T) {
	input := RolloutInput{
		Namespace:                    "default",
		HPAName:                      "web",
		Target:                       "Deployment/web",
		RolloutInProgress:            false,
		HasStartupProbe:              true,
		HasReadinessProbe:            true,
		ReadinessInitialDelaySeconds: 10,
	}

	report := AnalyzeRollout(input)
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", report.Namespace)
	}
	if report.Name != "web" {
		t.Errorf("expected name web, got %s", report.Name)
	}
	if report.RolloutInProgress {
		t.Error("expected RolloutInProgress to be false")
	}
	if report.NewPodsReady != "" {
		t.Errorf("expected empty NewPodsReady when no rollout, got %q", report.NewPodsReady)
	}
	if !strings.Contains(report.Summary, "no rollout in progress") {
		t.Errorf("expected summary about no rollout, got %s", report.Summary)
	}
}

func TestAnalyzeRolloutWithRolloutInProgress(t *testing.T) {
	input := RolloutInput{
		Namespace:                    "default",
		HPAName:                      "web",
		Target:                       "Deployment/web",
		RolloutInProgress:            true,
		UpdatedReplicas:              3,
		ReadyReplicas:                3,
		DesiredReplicas:              5,
		HasStartupProbe:              true,
		HasReadinessProbe:            true,
		ReadinessInitialDelaySeconds: 10,
	}

	report := AnalyzeRollout(input)
	if !report.RolloutInProgress {
		t.Error("expected RolloutInProgress to be true")
	}
	if report.NewPodsReady != "3/5" {
		t.Errorf("expected NewPodsReady=3/5, got %q", report.NewPodsReady)
	}
}

func TestAnalyzeRolloutMissingStartupProbe(t *testing.T) {
	input := RolloutInput{
		Namespace:                    "default",
		HPAName:                      "web",
		Target:                       "Deployment/web",
		RolloutInProgress:            false,
		HasStartupProbe:              false,
		HasReadinessProbe:            true,
		ReadinessInitialDelaySeconds: 10,
	}

	report := AnalyzeRollout(input)
	found := false
	for _, c := range report.Checks {
		if c.Category == "probe" && !c.Pass && strings.Contains(c.Message, "startupProbe is missing") {
			found = true
		}
	}
	if !found {
		t.Error("expected failing startupProbe check")
	}
}

func TestAnalyzeRolloutMissingReadinessProbe(t *testing.T) {
	input := RolloutInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		RolloutInProgress: false,
		HasStartupProbe:   true,
		HasReadinessProbe: false,
	}

	report := AnalyzeRollout(input)
	found := false
	for _, c := range report.Checks {
		if c.Category == "readiness" && !c.Pass && strings.Contains(c.Message, "readinessProbe is missing") {
			found = true
		}
	}
	if !found {
		t.Error("expected failing readinessProbe check")
	}
}

func TestAnalyzeRolloutShortReadinessDelay(t *testing.T) {
	input := RolloutInput{
		Namespace:                    "default",
		HPAName:                      "web",
		Target:                       "Deployment/web",
		RolloutInProgress:            false,
		HasStartupProbe:              true,
		HasReadinessProbe:            true,
		ReadinessInitialDelaySeconds: 2,
	}

	report := AnalyzeRollout(input)
	found := false
	for _, c := range report.Checks {
		if c.Category == "readiness" && !c.Pass && strings.Contains(c.Message, "initialDelaySeconds") {
			found = true
		}
	}
	if !found {
		t.Error("expected failing readiness delay check")
	}
}

func TestAnalyzeRolloutContainerNameMismatch(t *testing.T) {
	input := RolloutInput{
		Namespace:                    "default",
		HPAName:                      "web",
		Target:                       "Deployment/web",
		RolloutInProgress:            false,
		HasStartupProbe:              true,
		HasReadinessProbe:            true,
		ReadinessInitialDelaySeconds: 10,
		HPAContainerMetrics:          []string{"app", "sidecar"},
		NewReplicaSetContainerNames:  []string{"app"},
	}

	report := AnalyzeRollout(input)

	// Check for failing metric check.
	found := false
	for _, c := range report.Checks {
		if c.Category == "metric" && !c.Pass {
			found = true
		}
	}
	if !found {
		t.Error("expected failing metric check for container mismatch")
	}

	// Check for HIGH risk.
	foundHigh := false
	for _, r := range report.Risks {
		if r.Severity == "high" && r.Category == "metric" {
			foundHigh = true
		}
	}
	if !foundHigh {
		t.Error("expected HIGH risk for container mismatch")
	}
}

func TestAnalyzeRolloutContainerNameMatch(t *testing.T) {
	input := RolloutInput{
		Namespace:                    "default",
		HPAName:                      "web",
		Target:                       "Deployment/web",
		RolloutInProgress:            false,
		HasStartupProbe:              true,
		HasReadinessProbe:            true,
		ReadinessInitialDelaySeconds: 10,
		HPAContainerMetrics:          []string{"app"},
		NewReplicaSetContainerNames:  []string{"app", "sidecar"},
	}

	report := AnalyzeRollout(input)
	for _, c := range report.Checks {
		if c.Category == "metric" && !c.Pass {
			t.Errorf("expected metric check to pass when containers match, got: %s", c.Message)
		}
	}
}

func TestAnalyzeRolloutPodIssues(t *testing.T) {
	input := RolloutInput{
		Namespace:                    "default",
		HPAName:                      "web",
		Target:                       "Deployment/web",
		RolloutInProgress:            true,
		UpdatedReplicas:              3,
		DesiredReplicas:              5,
		HasStartupProbe:              true,
		HasReadinessProbe:            true,
		ReadinessInitialDelaySeconds: 10,
		PodIssues: []string{
			"pod-abc CrashLoopBackOff",
			"pod-xyz Pending",
		},
	}

	report := AnalyzeRollout(input)

	// CrashLoopBackOff should be HIGH severity.
	foundCrashHigh := false
	for _, r := range report.Risks {
		if r.Severity == "high" && strings.Contains(r.Message, "CrashLoopBackOff") {
			foundCrashHigh = true
		}
	}
	if !foundCrashHigh {
		t.Error("expected HIGH risk for CrashLoopBackOff")
	}

	// Pending should be medium severity.
	foundPendingMed := false
	for _, r := range report.Risks {
		if r.Severity == "medium" && strings.Contains(r.Message, "Pending") {
			foundPendingMed = true
		}
	}
	if !foundPendingMed {
		t.Error("expected medium risk for Pending pod")
	}
}

func TestAnalyzeRolloutAllChecksPass(t *testing.T) {
	input := RolloutInput{
		Namespace:                    "default",
		HPAName:                      "web",
		Target:                       "Deployment/web",
		RolloutInProgress:            false,
		HasStartupProbe:              true,
		HasReadinessProbe:            true,
		ReadinessInitialDelaySeconds: 10,
		HPAContainerMetrics:          []string{"app"},
		NewReplicaSetContainerNames:  []string{"app"},
	}

	report := AnalyzeRollout(input)
	for _, c := range report.Checks {
		if !c.Pass {
			t.Errorf("expected all checks to pass, but [%s] failed: %s", c.Category, c.Message)
		}
	}
	if len(report.Risks) != 0 {
		t.Errorf("expected no risks when all checks pass, got %d", len(report.Risks))
	}
}

func TestFindContainerMismatch(t *testing.T) {
	tests := []struct {
		name       string
		metrics    []string
		containers []string
		want       []string
	}{
		{
			name:       "no metrics",
			metrics:    nil,
			containers: []string{"app"},
			want:       nil,
		},
		{
			name:       "exact match",
			metrics:    []string{"app"},
			containers: []string{"app"},
			want:       nil,
		},
		{
			name:       "partial mismatch",
			metrics:    []string{"app", "missing"},
			containers: []string{"app"},
			want:       []string{"missing"},
		},
		{
			name:       "total mismatch",
			metrics:    []string{"foo"},
			containers: []string{"bar", "baz"},
			want:       []string{"foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findContainerMismatch(tt.metrics, tt.containers)
			if len(got) != len(tt.want) {
				t.Errorf("findContainerMismatch() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("findContainerMismatch()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
