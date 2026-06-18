package hpa

import (
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func TestSimulateExtended(t *testing.T) {
	hpa := buildTestHPAWithResourceMetric(5, 5, 1, 10, 50, 80)

	tests := []struct {
		name      string
		hpa       *autoscalingv2.HorizontalPodAutoscaler
		overrides map[string]string
		extOpts   SimulationExtendedOptions
		wantErr   bool
		check     func(t *testing.T, result *SimulationResult)
	}{
		{
			name:    "nil HPA returns error",
			hpa:     nil,
			wantErr: true,
		},
		{
			name:      "basic simulation without duration",
			hpa:       hpa,
			overrides: map[string]string{"maxReplicas": "20"},
			extOpts:   SimulationExtendedOptions{},
			check: func(t *testing.T, result *SimulationResult) {
				if result.TimeSeriesProjection != nil {
					t.Error("TimeSeriesProjection should be nil without duration")
				}
			},
		},
		{
			name:      "extended simulation with duration produces trajectory",
			hpa:       hpa,
			overrides: map[string]string{"maxReplicas": "20"},
			extOpts:   SimulationExtendedOptions{DurationSeconds: 300, StepSeconds: 60},
			check: func(t *testing.T, result *SimulationResult) {
				if len(result.TimeSeriesProjection) == 0 {
					t.Error("TimeSeriesProjection should not be empty with duration")
				}
				if len(result.TimeSeriesProjection) < 5 {
					t.Errorf("expected at least 5 projection points, got %d", len(result.TimeSeriesProjection))
				}
				if result.TimeSeriesProjection[0].TimeOffset != 0 {
					t.Errorf("first offset = %d, want 0", result.TimeSeriesProjection[0].TimeOffset)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SimulateExtended(tt.hpa, tt.overrides, HealthWeights{}, tt.extOpts)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestAssessExtendedRisk(t *testing.T) {
	hpa := buildTestHPAWithResourceMetric(5, 5, 1, 10, 50, 80)

	tests := []struct {
		name      string
		overrides map[string]string
		before    SimulationState
		after     SimulationState
		wantWarn  []string
	}{
		{
			name:      "large replica swing warning",
			overrides: map[string]string{"maxReplicas": "20"},
			before:    SimulationState{DesiredReplicas: 2, Health: "OK", HealthScore: 100},
			after:     SimulationState{DesiredReplicas: 10, Health: "LIMITED", HealthScore: 70},
			wantWarn:  []string{"Large replica swing"},
		},
		{
			name:      "at maxReplicas warning",
			overrides: map[string]string{"maxReplicas": "20"},
			before:    SimulationState{DesiredReplicas: 10},
			after:     SimulationState{DesiredReplicas: 20, Health: "LIMITED", HealthScore: 70},
			wantWarn:  []string{"at maxReplicas"},
		},
		{
			name:      "health degradation warning",
			overrides: map[string]string{},
			before:    SimulationState{DesiredReplicas: 5, Health: "OK", HealthScore: 100},
			after:     SimulationState{DesiredReplicas: 5, Health: "ERROR", HealthScore: 55},
			wantWarn:  []string{"Health score would drop"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &SimulationResult{Before: tt.before, After: tt.after}
			warnings := assessExtendedRisk(hpa, tt.overrides, result)
			for _, want := range tt.wantWarn {
				found := false
				for _, w := range warnings {
					if strings.Contains(w, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("warning containing %q not found in %v", want, warnings)
				}
			}
		})
	}
}

func TestProjectReplicaTrajectory(t *testing.T) {
	original := buildTestHPAWithResourceMetric(5, 5, 1, 10, 50, 80)
	modified := buildTestHPAWithResourceMetric(5, 5, 1, 20, 50, 80)

	states := ProjectReplicaTrajectory(original, modified, SimulationExtendedOptions{
		DurationSeconds: 300,
		StepSeconds:     60,
	})

	if len(states) < 5 {
		t.Errorf("expected at least 5 states, got %d", len(states))
	}

	// First state should be at offset 0.
	if states[0].TimeOffset != 0 {
		t.Errorf("first TimeOffset = %d, want 0", states[0].TimeOffset)
	}

	// Last state should cover the full duration.
	last := states[len(states)-1]
	if last.TimeOffset < 300 {
		t.Errorf("last TimeOffset = %d, want >= 300", last.TimeOffset)
	}

	// All replicas should be within min/max bounds.
	for _, s := range states {
		if s.ProjectedReplicas < 1 {
			t.Errorf("ProjectedReplicas = %d, want >= 1", s.ProjectedReplicas)
		}
		if s.ProjectedReplicas > 20 {
			t.Errorf("ProjectedReplicas = %d, want <= 20", s.ProjectedReplicas)
		}
	}
}

func TestFormatTrajectoryASCII(t *testing.T) {
	t.Run("empty states returns empty", func(t *testing.T) {
		got := FormatTrajectoryASCII(nil, 40)
		if got != "" {
			t.Errorf("expected empty for nil states, got %q", got)
		}
	})

	t.Run("single state renders", func(t *testing.T) {
		states := []ProjectedState{
			{TimeOffset: 0, ProjectedReplicas: 5, ProjectedMetricRatio: 1.0},
			{TimeOffset: 300, ProjectedReplicas: 10, ProjectedMetricRatio: 0.5},
		}
		got := FormatTrajectoryASCII(states, 40)
		if got == "" {
			t.Error("expected non-empty output")
		}
		if !strings.Contains(got, "│") {
			t.Error("expected graph borders")
		}
	})
}

func TestFormatSimulationExtended(t *testing.T) {
	t.Run("nil result returns empty", func(t *testing.T) {
		got := FormatSimulationExtended(nil)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("result with warnings renders", func(t *testing.T) {
		result := &SimulationResult{
			Before:       SimulationState{DesiredReplicas: 5, Health: "OK", HealthScore: 100},
			After:        SimulationState{DesiredReplicas: 10, Health: "LIMITED", HealthScore: 75},
			RiskWarnings: []string{"test warning"},
		}
		got := FormatSimulationExtended(result)
		if !strings.Contains(got, "Simulation Comparison") {
			t.Error("expected comparison header")
		}
		if !strings.Contains(got, "test warning") {
			t.Error("expected warning in output")
		}
	})

	t.Run("result with trajectory renders graph", func(t *testing.T) {
		result := &SimulationResult{
			Before: SimulationState{DesiredReplicas: 5, Health: "OK", HealthScore: 100},
			After:  SimulationState{DesiredReplicas: 10, Health: "OK", HealthScore: 100},
			TimeSeriesProjection: []ProjectedState{
				{TimeOffset: 0, ProjectedReplicas: 5},
				{TimeOffset: 150, ProjectedReplicas: 8},
				{TimeOffset: 300, ProjectedReplicas: 10},
			},
		}
		got := FormatSimulationExtended(result)
		if !strings.Contains(got, "Projected Trajectory") {
			t.Error("expected trajectory header")
		}
		if !strings.Contains(got, "│") {
			t.Error("expected graph borders")
		}
	})
}

func TestTargetAverageUtilizationOverride(t *testing.T) {
	hpa := buildTestHPAWithResourceMetric(5, 5, 1, 10, 50, 80)

	result, err := SimulateHPA(hpa, map[string]string{"targetAverageUtilization": "60"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// buildTestHPAWithResourceMetric creates a simple HPA with a resource metric for testing.
func buildTestHPAWithResourceMetric(current, desired, minReplicas, maxReplicas, targetUtil, currentUtil int32) *autoscalingv2.HorizontalPodAutoscaler {
	return testutil.BuildHPA("default", "test-hpa",
		testutil.WithMinMax(minReplicas, maxReplicas),
		testutil.WithReplicas(current, desired),
		testutil.WithScaleTargetRef("Deployment", "app"),
		testutil.WithResourceMetric("cpu", targetUtil, currentUtil),
	)
}
