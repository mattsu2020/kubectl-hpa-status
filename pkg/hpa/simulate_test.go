package hpa

import (
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func TestSimulateHPA_NilHPA(t *testing.T) {
	_, err := SimulateHPA(nil, map[string]string{"maxReplicas": "20"}, HealthWeights{})
	if err == nil {
		t.Error("expected error for nil HPA")
	}
}

func TestSimulateHPA_RaiseMaxReplicas(t *testing.T) {
	hpa := buildSimHPA(5, 5, 10) // current=5, desired=5, max=10 -> at max -> LIMITED

	result, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "20"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Parameter != "maxReplicas" {
		t.Errorf("expected parameter=maxReplicas, got %q", result.Parameter)
	}
	if result.OriginalValue != "10" {
		t.Errorf("expected originalValue=10, got %q", result.OriginalValue)
	}
	if result.SimulatedValue != "20" {
		t.Errorf("expected simulatedValue=20, got %q", result.SimulatedValue)
	}

	// The deep copy should not mutate the original
	if hpa.Spec.MaxReplicas != 10 {
		t.Errorf("original HPA was mutated: maxReplicas=%d", hpa.Spec.MaxReplicas)
	}
}

func TestSimulateHPA_LowerMinReplicas(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)
	hpa.Spec.MinReplicas = ptrInt32(3)

	result, err := SimulateHPA(hpa, map[string]string{"minReplicas": "1"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.OriginalValue != "3" {
		t.Errorf("expected originalValue=3, got %q", result.OriginalValue)
	}
	if result.SimulatedValue != "1" {
		t.Errorf("expected simulatedValue=1, got %q", result.SimulatedValue)
	}
}

func TestSimulateHPA_StabilizationWindow(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)
	window := int32(300)
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &window,
		},
	}

	result, err := SimulateHPA(hpa, map[string]string{"scaleDown.stabilizationWindowSeconds": "30"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.OriginalValue != "300" {
		t.Errorf("expected originalValue=300, got %q", result.OriginalValue)
	}
	if result.SimulatedValue != "30" {
		t.Errorf("expected simulatedValue=30, got %q", result.SimulatedValue)
	}
	if result.RiskAssessment == "" {
		t.Error("expected risk assessment for reducing stabilization window")
	}
}

func TestSimulateHPA_InvalidPath(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	_, err := SimulateHPA(hpa, map[string]string{"invalidField": "10"}, HealthWeights{})
	if err == nil {
		t.Error("expected error for invalid path")
	}
	if !strings.Contains(err.Error(), "unsupported path") {
		t.Errorf("expected unsupported path error, got: %v", err)
	}
}

func TestSimulateHPA_InvalidValue(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	_, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "abc"}, HealthWeights{})
	if err == nil {
		t.Error("expected error for non-numeric value")
	}
}

func TestSimulateHPA_MaxReplicasZero(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	_, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "0"}, HealthWeights{})
	if err == nil {
		t.Error("expected error for maxReplicas=0")
	}
}

func TestSimulateHPA_DeepCopyIsolation(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	_, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "20"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hpa.Spec.MaxReplicas != 10 {
		t.Errorf("original HPA maxReplicas mutated: got %d, want 10", hpa.Spec.MaxReplicas)
	}
}

func TestSimulateHPA_InterpretationGenerated(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	result, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "20"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Interpretation) == 0 {
		t.Error("expected interpretation lines to be generated")
	}

	found := false
	for _, line := range result.Interpretation {
		if strings.Contains(line, "desiredReplicas") {
			found = true
		}
	}
	if !found {
		t.Error("expected interpretation to mention desiredReplicas")
	}
}

func TestSimulateHPA_SelectPolicy(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	result, err := SimulateHPA(hpa, map[string]string{"scaleDown.selectPolicy": "Disabled"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestSimulateScenarioTargetToleranceAndDuration(t *testing.T) {
	hpa := buildMetricSimHPA(10, 10, 30, 100)
	// Current 108% is inside the default 10% band, but outside the simulated
	// 5% scale-up tolerance. Lowering the target to 90 further raises the ratio.
	current := int32(108)
	hpa.Status.CurrentMetrics[0].Resource.Current.AverageUtilization = &current

	result, err := SimulateScenario(hpa,
		map[string]string{"metric.cpu.target": "90", "tolerance": "0.05"},
		nil, HealthWeights{}, SimulationExtendedOptions{DurationSeconds: 120, StepSeconds: 30})
	if err != nil {
		t.Fatalf("SimulateScenario: %v", err)
	}
	if result.After.DesiredReplicas != 12 {
		t.Fatalf("after desired replicas = %d, want ceil(10 * 108/90) = 12", result.After.DesiredReplicas)
	}
	if len(result.TimeSeriesProjection) == 0 || result.TimeSeriesProjection[len(result.TimeSeriesProjection)-1].TimeOffset != 120 {
		t.Fatalf("duration projection not applied: %+v", result.TimeSeriesProjection)
	}
	modified, err := BuildSimulatedHPA(hpa,
		map[string]string{"metric.cpu.target": "90", "tolerance": "0.05"}, nil)
	if err != nil {
		t.Fatalf("BuildSimulatedHPA: %v", err)
	}
	if got := *modified.Spec.Metrics[0].Resource.Target.AverageUtilization; got != 90 {
		t.Fatalf("simulated target = %d, want 90", got)
	}
	if modified.Spec.Behavior == nil || modified.Spec.Behavior.ScaleUp.Tolerance == nil || modified.Spec.Behavior.ScaleDown.Tolerance == nil {
		t.Fatalf("simulated directional tolerances missing: %+v", modified.Spec.Behavior)
	}
}

func TestSimulateScenarioToleranceOnlyChangesProjection(t *testing.T) {
	hpa := buildMetricSimHPA(10, 10, 30, 100)
	current := int32(108)
	hpa.Status.CurrentMetrics[0].Resource.Current.AverageUtilization = &current

	defaultResult, err := SimulateScenario(hpa, nil, map[string]string{"cpu": "108%"}, HealthWeights{}, SimulationExtendedOptions{})
	if err != nil {
		t.Fatalf("default simulation: %v", err)
	}
	if defaultResult.After.DesiredReplicas != 10 {
		t.Fatalf("default tolerance should hold at 10, got %d", defaultResult.After.DesiredReplicas)
	}
	tightResult, err := SimulateScenario(hpa, map[string]string{"tolerance": "0.05"}, map[string]string{"cpu": "108%"}, HealthWeights{}, SimulationExtendedOptions{})
	if err != nil {
		t.Fatalf("tight tolerance simulation: %v", err)
	}
	if tightResult.After.DesiredReplicas != 11 {
		t.Fatalf("0.05 tolerance should project 11, got %d", tightResult.After.DesiredReplicas)
	}
}

func buildSimHPA(current, desired, maxReplicas int32) *autoscalingv2.HorizontalPodAutoscaler {
	return testutil.BuildHPA("default", "test-hpa",
		testutil.WithMinMax(1, maxReplicas),
		testutil.WithReplicas(current, desired),
		testutil.WithScaleTargetRef("Deployment", "test-deploy"),
		testutil.WithConditions(
			autoscalingv2.HorizontalPodAutoscalerCondition{
				Type: autoscalingv2.ScalingActive, Status: corev1.ConditionTrue, Reason: "ValidMetricFound",
			},
		),
	)
}
