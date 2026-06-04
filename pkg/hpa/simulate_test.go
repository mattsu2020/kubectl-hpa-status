package hpa

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func buildSimHPA(current, desired, maxReplicas int32) *autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := int32(1)
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test-deploy",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: current,
			DesiredReplicas: desired,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{
					Type:   autoscalingv2.ScalingActive,
					Status: corev1.ConditionTrue,
					Reason: "ValidMetricFound",
				},
			},
		},
	}
}
