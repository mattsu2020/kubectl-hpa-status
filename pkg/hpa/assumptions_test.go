package hpa

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func buildAssumptionsHPA() *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "production"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			MinReplicas: int32Ptr(1),
			MaxReplicas: 10,
		},
	}
}

func TestDetectControllerAssumptions(t *testing.T) {
	tests := []struct {
		name  string
		hpa   *autoscalingv2.HorizontalPodAutoscaler
		check func(t *testing.T, result *ControllerAssumptions)
	}{
		{
			name:  "nil HPA returns nil",
			hpa:   nil,
			check: func(t *testing.T, result *ControllerAssumptions) {
				if result != nil {
					t.Fatalf("expected nil, got %+v", result)
				}
			},
		},
		{
			name: "no behavior - all defaults",
			hpa:  buildAssumptionsHPA(),
			check: func(t *testing.T, result *ControllerAssumptions) {
				defaults := []Assumption{
					result.SyncPeriod,
					result.GlobalTolerance,
					result.CPUInitializationPeriod,
					result.InitialReadinessDelay,
					result.DownscaleStabilization,
					result.UpscaleStabilization,
				}
				for _, a := range defaults {
					if a.Source != "kubernetes-default" {
						t.Errorf("expected source kubernetes-default for %s, got %s", a.Name, a.Source)
					}
				}
				if result.SyncPeriod.Confidence != "medium" {
					t.Errorf("expected SyncPeriod confidence medium, got %s", result.SyncPeriod.Confidence)
				}
				if result.CPUInitializationPeriod.Confidence != "low" {
					t.Errorf("expected CPUInitializationPeriod confidence low, got %s", result.CPUInitializationPeriod.Confidence)
				}
				if result.InitialReadinessDelay.Confidence != "low" {
					t.Errorf("expected InitialReadinessDelay confidence low, got %s", result.InitialReadinessDelay.Confidence)
				}
				if result.DownscaleStabilization.Value != "300s" {
					t.Errorf("expected DownscaleStabilization value 300s, got %s", result.DownscaleStabilization.Value)
				}
				if result.UpscaleStabilization.Value != "0s" {
					t.Errorf("expected UpscaleStabilization value 0s, got %s", result.UpscaleStabilization.Value)
				}
				warningTexts := strings.Join(result.Warnings, " ")
				for _, want := range []string{"CPU", "10%", "ready"} {
					if !strings.Contains(warningTexts, want) {
						t.Errorf("expected warnings to contain %q, got: %v", want, result.Warnings)
					}
				}
			},
		},
		{
			name: "explicit stabilization window",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				h := buildAssumptionsHPA()
				h.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: int32Ptr(600),
					},
				}
				return h
			}(),
			check: func(t *testing.T, result *ControllerAssumptions) {
				a := result.DownscaleStabilization
				if a.Value != "600s" {
					t.Errorf("expected value 600s, got %s", a.Value)
				}
				if a.Source != "hpa.spec" {
					t.Errorf("expected source hpa.spec, got %s", a.Source)
				}
				if a.Confidence != "high" {
					t.Errorf("expected confidence high, got %s", a.Confidence)
				}
			},
		},
		{
			name: "explicit tolerance",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				h := buildAssumptionsHPA()
				tol := resource.MustParse("0.15")
				h.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{
						Tolerance: &tol,
					},
				}
				return h
			}(),
			check: func(t *testing.T, result *ControllerAssumptions) {
				a := result.GlobalTolerance
				if a.Source != "hpa.spec" {
					t.Errorf("expected source hpa.spec, got %s", a.Source)
				}
				if a.Confidence != "high" {
					t.Errorf("expected confidence high, got %s", a.Confidence)
				}
			},
		},
		{
			name: "explicit upscale stabilization",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				h := buildAssumptionsHPA()
				h.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleUp: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: int32Ptr(120),
					},
				}
				return h
			}(),
			check: func(t *testing.T, result *ControllerAssumptions) {
				a := result.UpscaleStabilization
				if a.Value != "120s" {
					t.Errorf("expected value 120s, got %s", a.Value)
				}
				if a.Source != "hpa.spec" {
					t.Errorf("expected source hpa.spec, got %s", a.Source)
				}
				if a.Confidence != "high" {
					t.Errorf("expected confidence high, got %s", a.Confidence)
				}
			},
		},
		{
			name: "all explicit values",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				h := buildAssumptionsHPA()
				tol := resource.MustParse("0.2")
				h.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: int32Ptr(600),
						Tolerance:                  &tol,
					},
					ScaleUp: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: int32Ptr(120),
					},
				}
				return h
			}(),
			check: func(t *testing.T, result *ControllerAssumptions) {
				explicit := []Assumption{
					result.GlobalTolerance,
					result.DownscaleStabilization,
					result.UpscaleStabilization,
				}
				for _, a := range explicit {
					if a.Source != "hpa.spec" {
						t.Errorf("expected source hpa.spec for %s, got %s", a.Name, a.Source)
					}
					if a.Confidence != "high" {
						t.Errorf("expected confidence high for %s, got %s", a.Name, a.Confidence)
					}
				}
				if !strings.Contains(result.Summary, "explicitly configured") {
					t.Errorf("expected summary to mention explicit values, got: %s", result.Summary)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectControllerAssumptions(tt.hpa)
			tt.check(t, result)
		})
	}
}
