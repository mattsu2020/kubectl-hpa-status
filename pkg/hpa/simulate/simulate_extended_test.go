package simulate

import (
	"encoding/json"
	"math"
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
			result, err := Extended(tt.hpa, tt.overrides, HealthWeights{}, tt.extOpts)
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

func TestSimulateExtendedRiskUsesOverriddenReplicaBounds(t *testing.T) {
	t.Parallel()

	hpa := buildMetricSimHPA(10, 10, 10, 100)
	current := int32(150)
	hpa.Status.CurrentMetrics[0].Resource.Current.AverageUtilization = &current

	result, err := HPA(hpa, map[string]string{"maxReplicas": "20"}, HealthWeights{})
	if err != nil {
		t.Fatalf("HPA: %v", err)
	}
	if result.After.DesiredReplicas != 15 {
		t.Fatalf("After.DesiredReplicas = %d, want 15", result.After.DesiredReplicas)
	}
	for _, warning := range result.RiskWarnings {
		if strings.Contains(warning, "at maxReplicas=10") {
			t.Fatalf("risk warning used the pre-override bound: %q", warning)
		}
	}
}

func TestProjectReplicaTrajectory(t *testing.T) {
	original := buildTestHPAWithResourceMetric(5, 5, 1, 10, 50, 80)
	modified := buildTestHPAWithResourceMetric(5, 5, 1, 20, 50, 80)

	states, err := ProjectReplicaTrajectory(original, modified, SimulationExtendedOptions{
		DurationSeconds: 300,
		StepSeconds:     60,
	})
	if err != nil {
		t.Fatal(err)
	}

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

func TestComputeMetricRatioAtZeroIsJSONSafe(t *testing.T) {
	ratio := computeMetricRatio(5, 0, 0, 10)
	if math.IsInf(ratio, 0) || math.IsNaN(ratio) {
		t.Fatalf("ratio must be finite, got %v", ratio)
	}
	if _, err := json.Marshal(ProjectedState{ProjectedMetricRatio: ratio}); err != nil {
		t.Fatalf("ProjectedState must be JSON serializable: %v", err)
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

	result, err := HPA(hpa, map[string]string{"targetAverageUtilization": "60"}, HealthWeights{})
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

// TestProjectReplicaTrajectoryScaleUpIgnoresScaleDownWindow pins the
// per-direction stabilization fix: a 300s scale-down stabilization window
// must not stall a projected increase. The controller applies scaleUp rules
// to increases, and with no scaleUp stabilization configured the trajectory
// reaches the projected replicas at the very first step. The modified HPA is
// built through BuildSimulatedHPA so its status carries the recomputed
// desired count, matching how ProjectReplicaTrajectory is invoked in
// production.
func TestProjectReplicaTrajectoryScaleUpIgnoresScaleDownWindow(t *testing.T) {
	original := buildTestHPAWithResourceMetric(5, 5, 1, 10, 50, 80)
	testutil.WithScaleDownStabilizationWindow(300)(original)

	modified, err := BuildSimulatedHPA(original, map[string]string{"maxReplicas": "20"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if modified.Status.DesiredReplicas <= original.Status.DesiredReplicas {
		t.Fatalf("fixture must project an increase; desired %d → %d", original.Status.DesiredReplicas, modified.Status.DesiredReplicas)
	}

	states, err := ProjectReplicaTrajectory(original, modified, SimulationExtendedOptions{
		DurationSeconds: 300,
		StepSeconds:     30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) == 0 {
		t.Fatal("expected projection states")
	}
	if states[0].ProjectedReplicas != modified.Status.DesiredReplicas {
		t.Fatalf("scale-up must not wait for the scale-down stabilization window; first state = %d replicas, want %d", states[0].ProjectedReplicas, modified.Status.DesiredReplicas)
	}
}

// TestComputeStabilizationDelayPerDirection checks the rule selection table.
func TestComputeStabilizationDelayPerDirection(t *testing.T) {
	scaleDownWindow := int32(300)
	scaleUpWindow := int32(60)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &scaleUpWindow,
				},
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &scaleDownWindow,
				},
			},
		},
	}

	if got := computeStabilizationDelay(hpa, 5, 10); got != 60 {
		t.Errorf("scale-up delay = %d, want the scaleUp window 60", got)
	}
	if got := computeStabilizationDelay(hpa, 10, 5); got != 300 {
		t.Errorf("scale-down delay = %d, want the scaleDown window 300", got)
	}

	// No behavior configured at all: no delay either direction.
	plain := buildTestHPAWithResourceMetric(5, 5, 1, 10, 50, 80)
	if got := computeStabilizationDelay(plain, 5, 10); got != 0 {
		t.Errorf("delay without behavior = %d, want 0", got)
	}

	// Direction rules left nil fall back to zero for that direction.
	onlyDown := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &scaleDownWindow,
				},
			},
		},
	}
	if got := computeStabilizationDelay(onlyDown, 5, 10); got != 0 {
		t.Errorf("scale-up delay with no scaleUp rules = %d, want 0", got)
	}
}

// TestProjectReplicaTrajectoryScaleDownHonorsWindow checks the decrease
// direction still holds the current replicas for the configured scale-down
// stabilization window before the projection descends.
func TestProjectReplicaTrajectoryScaleDownHonorsWindow(t *testing.T) {
	original := buildTestHPAWithResourceMetric(10, 10, 1, 20, 50, 20)
	testutil.WithScaleDownStabilizationWindow(300)(original)

	// The maxReplicas value is unchanged, but a non-empty override set is what
	// triggers BuildSimulatedHPA's desired-replica recompute, mirroring a real
	// what-if run.
	modified, err := BuildSimulatedHPA(original, map[string]string{"maxReplicas": "20"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if modified.Status.DesiredReplicas >= original.Status.DesiredReplicas {
		t.Fatalf("fixture must project a decrease; desired %d → %d", original.Status.DesiredReplicas, modified.Status.DesiredReplicas)
	}

	states, err := ProjectReplicaTrajectory(original, modified, SimulationExtendedOptions{
		DurationSeconds: 300,
		StepSeconds:     30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) == 0 {
		t.Fatal("expected projection states")
	}
	for _, s := range states {
		if s.TimeOffset < 300 && s.ProjectedReplicas != original.Status.DesiredReplicas {
			t.Fatalf("scale-down must hold the current replicas during the stabilization window; state at %ds = %d replicas", s.TimeOffset, s.ProjectedReplicas)
		}
	}
}
