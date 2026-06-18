package hpa

import (
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

func buildAssumptionsHPA() *autoscalingv2.HorizontalPodAutoscaler {
	return testutil.BuildHPA("production", "web",
		testutil.WithMinMax(1, 10),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)
}

func TestDetectControllerAssumptions(t *testing.T) {
	tests := []struct {
		name  string
		hpa   *autoscalingv2.HorizontalPodAutoscaler
		check func(t *testing.T, result *ControllerAssumptions)
	}{
		{
			name: "nil HPA returns nil",
			hpa:  nil,
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

func TestDetectControllerAssumptionsWithOverrides(t *testing.T) {
	tolerance := "0.20"
	syncPeriod := "30s"
	cpuInit := "3m"
	readinessDelay := "60s"

	tests := []struct {
		name      string
		hpa       *autoscalingv2.HorizontalPodAutoscaler
		overrides AssumptionOverrides
		observed  *ControllerProfile
		check     func(t *testing.T, result *ControllerAssumptions)
	}{
		{
			name:      "nil HPA returns nil",
			hpa:       nil,
			overrides: AssumptionOverrides{},
			check: func(t *testing.T, result *ControllerAssumptions) {
				if result != nil {
					t.Fatalf("expected nil, got %+v", result)
				}
			},
		},
		{
			name:      "empty overrides behaves like original",
			hpa:       buildAssumptionsHPA(),
			overrides: AssumptionOverrides{},
			check: func(t *testing.T, result *ControllerAssumptions) {
				if result.SyncPeriod.Source != "kubernetes-default" {
					t.Errorf("expected default source, got %s", result.SyncPeriod.Source)
				}
				if result.GlobalTolerance.Source != "kubernetes-default" {
					t.Errorf("expected default source, got %s", result.GlobalTolerance.Source)
				}
			},
		},
		{
			name: "overrides replace values and set confidence to high",
			hpa:  buildAssumptionsHPA(),
			overrides: AssumptionOverrides{
				Tolerance:               &tolerance,
				SyncPeriod:              &syncPeriod,
				CPUInitializationPeriod: &cpuInit,
				InitialReadinessDelay:   &readinessDelay,
			},
			check: func(t *testing.T, result *ControllerAssumptions) {
				for _, a := range []Assumption{
					result.GlobalTolerance,
					result.SyncPeriod,
					result.CPUInitializationPeriod,
					result.InitialReadinessDelay,
				} {
					if a.Source != "overridden" {
						t.Errorf("expected source 'overridden' for %s, got %s", a.Name, a.Source)
					}
					if a.Confidence != "high" {
						t.Errorf("expected confidence 'high' for %s, got %s", a.Name, a.Confidence)
					}
				}
				if result.GlobalTolerance.Value != "0.20" {
					t.Errorf("expected tolerance value 0.20, got %s", result.GlobalTolerance.Value)
				}
				if result.SyncPeriod.Value != "30s" {
					t.Errorf("expected sync period value 30s, got %s", result.SyncPeriod.Value)
				}
			},
		},
		{
			name: "observed profile upgrades default fields",
			hpa:  buildAssumptionsHPA(),
			observed: &ControllerProfile{
				Source:                  "kube-system/kube-controller-manager-1",
				SyncPeriod:              "10s",
				Tolerance:               "0.05",
				CPUInitializationPeriod: "3m",
				InitialReadinessDelay:   "45s",
				DownscaleStabilization:  "120s",
			},
			check: func(t *testing.T, result *ControllerAssumptions) {
				if result.SyncPeriod.Source != "kube-system/kube-controller-manager-1" {
					t.Errorf("expected observed source, got %s", result.SyncPeriod.Source)
				}
				if result.SyncPeriod.Value != "10s" {
					t.Errorf("expected observed sync period 10s, got %s", result.SyncPeriod.Value)
				}
				if result.SyncPeriod.Confidence != "medium" {
					t.Errorf("expected observed confidence medium, got %s", result.SyncPeriod.Confidence)
				}
			},
		},
		{
			name: "observed profile does not override hpa.spec values",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				h := buildAssumptionsHPA()
				h.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: int32Ptr(600),
					},
				}
				return h
			}(),
			observed: &ControllerProfile{
				Source:                 "kube-system/kube-controller-manager-1",
				DownscaleStabilization: "120s",
			},
			check: func(t *testing.T, result *ControllerAssumptions) {
				// hpa.spec should win over observed profile
				if result.DownscaleStabilization.Source != "hpa.spec" {
					t.Errorf("expected hpa.spec source, got %s", result.DownscaleStabilization.Source)
				}
				if result.DownscaleStabilization.Value != "600s" {
					t.Errorf("expected 600s, got %s", result.DownscaleStabilization.Value)
				}
			},
		},
		{
			name: "overrides take priority over observed profile",
			hpa:  buildAssumptionsHPA(),
			overrides: AssumptionOverrides{
				SyncPeriod: &syncPeriod,
			},
			observed: &ControllerProfile{
				Source:     "kube-system/kube-controller-manager-1",
				SyncPeriod: "10s",
			},
			check: func(t *testing.T, result *ControllerAssumptions) {
				if result.SyncPeriod.Source != "overridden" {
					t.Errorf("expected overridden source, got %s", result.SyncPeriod.Source)
				}
				if result.SyncPeriod.Value != "30s" {
					t.Errorf("expected overridden value 30s, got %s", result.SyncPeriod.Value)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectControllerAssumptionsWithOverrides(tt.hpa, tt.overrides, tt.observed)
			tt.check(t, result)
		})
	}
}
