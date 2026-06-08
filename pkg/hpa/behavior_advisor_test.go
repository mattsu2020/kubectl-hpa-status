package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAnalyzeBehaviorAdvisor_NilHPA(t *testing.T) {
	result := AnalyzeBehaviorAdvisor(nil)
	if result != nil {
		t.Error("expected nil for nil HPA")
	}
}

func TestAnalyzeBehaviorAdvisor_NoBehavior(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: int32Ptr(60),
						},
					},
				},
			},
		},
	}

	result := AnalyzeBehaviorAdvisor(hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should have findings about default stabilization window and tolerance.
	hasStabilization := false
	hasTolerance := false
	for _, f := range result.Findings {
		if f.Category == "stabilization" {
			hasStabilization = true
		}
		if f.Category == "tolerance" {
			hasTolerance = true
		}
	}
	if !hasStabilization {
		t.Error("expected stabilization finding for default behavior")
	}
	if !hasTolerance {
		t.Error("expected tolerance finding for default behavior")
	}
}

func TestAnalyzeBehaviorAdvisor_LongScaleDownWindow(t *testing.T) {
	window := int32(900)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &window,
				},
			},
		},
	}

	result := AnalyzeBehaviorAdvisor(hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	found := false
	for _, f := range result.Findings {
		if f.ID == "behavior-scaledown-window-long" {
			found = true
			if f.Severity != SeverityWarning {
				t.Errorf("expected warning severity, got %s", f.Severity)
			}
			break
		}
	}
	if !found {
		t.Error("expected finding for long scale-down window")
	}
}

func TestAnalyzeBehaviorAdvisor_LongScaleUpWindow(t *testing.T) {
	window := int32(300)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &window,
				},
			},
		},
	}

	result := AnalyzeBehaviorAdvisor(hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	found := false
	for _, f := range result.Findings {
		if f.ID == "behavior-scaleup-window-long" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected finding for long scale-up window")
	}
}

func TestAnalyzeBehaviorAdvisor_LooseTolerance(t *testing.T) {
	tol := resource.MustParse("0.25")
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					Tolerance: &tol,
				},
			},
		},
	}

	result := AnalyzeBehaviorAdvisor(hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	found := false
	for _, f := range result.Findings {
		if f.ID == "behavior-tolerance-scaledown-loose" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected finding for loose scale-down tolerance")
	}
}

func TestAnalyzeBehaviorAdvisor_NoPoliciesWithExplicitBehavior(t *testing.T) {
	window := int32(300)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &window,
				},
			},
		},
	}

	result := AnalyzeBehaviorAdvisor(hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	found := false
	for _, f := range result.Findings {
		if f.ID == "behavior-scaledown-no-policies" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected finding for missing scaleDown policies")
	}
}

func TestAnalyzeBehaviorAdvisor_ReasonableConfig(t *testing.T) {
	window := int32(300)
	tol := resource.MustParse("0.1")
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &window,
					Tolerance:                  &tol,
					Policies: []autoscalingv2.HPAScalingPolicy{
						{
							Type:          autoscalingv2.PercentScalingPolicy,
							Value:         10,
							PeriodSeconds: 60,
						},
					},
				},
				ScaleUp: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &window,
					Tolerance:                  &tol,
					Policies: []autoscalingv2.HPAScalingPolicy{
						{
							Type:          autoscalingv2.PercentScalingPolicy,
							Value:         100,
							PeriodSeconds: 60,
						},
					},
				},
			},
		},
	}

	result := AnalyzeBehaviorAdvisor(hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// A reasonable config should have few or no findings.
	if len(result.Findings) > 2 {
		t.Errorf("expected few findings for reasonable config, got %d", len(result.Findings))
	}
}
