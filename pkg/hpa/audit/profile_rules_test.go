package audit

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

// ---------------------------------------------------------------------------
// latencyScaleUpPolicyRule
// ---------------------------------------------------------------------------

func TestLatencyScaleUpPolicyRule(t *testing.T) {
	mk := func(beh *autoscalingv2.HorizontalPodAutoscalerBehavior) *autoscalingv2.HorizontalPodAutoscaler {
		return &autoscalingv2.HorizontalPodAutoscaler{Spec: autoscalingv2.HorizontalPodAutoscalerSpec{Behavior: beh}}
	}
	policy := func(period int32) []autoscalingv2.HPAScalingPolicy {
		return []autoscalingv2.HPAScalingPolicy{{Type: autoscalingv2.PodsScalingPolicy, Value: 1, PeriodSeconds: period}}
	}

	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		wantFindings int
		wantID       string
	}{
		{
			name:         "no scaleUp policies returns latency warning",
			hpa:          mk(nil),
			wantFindings: 1,
			wantID:       "latency-scale-up-policy",
		},
		{
			name:         "behavior with nil scaleUp still lacks policies and returns finding",
			hpa:          mk(&autoscalingv2.HorizontalPodAutoscalerBehavior{}),
			wantFindings: 1,
			wantID:       "latency-scale-up-policy",
		},
		{
			name: "policy period too long returns period finding",
			hpa: mk(&autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{Policies: policy(60)},
			}),
			wantFindings: 1,
			wantID:       "latency-scale-up-period",
		},
		{
			name: "policy period within limit returns no findings",
			hpa: mk(&autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{Policies: policy(15)},
			}),
			wantFindings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latencyScaleUpPolicyRule(tt.hpa, 1)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d: %+v", tt.wantFindings, len(got), got)
			}
			if tt.wantFindings > 0 && got[0].ID != tt.wantID {
				t.Fatalf("expected finding ID %q, got %q", tt.wantID, got[0].ID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// batchToleranceRule
// ---------------------------------------------------------------------------

func TestBatchToleranceRule(t *testing.T) {
	tol := func(v string) *resource.Quantity {
		q := resource.MustParse(v)
		return &q
	}

	tests := []struct {
		name         string
		behavior     *autoscalingv2.HorizontalPodAutoscalerBehavior
		wantFindings int
	}{
		{
			name:         "default tolerance (nil behavior) is too tight for batch",
			behavior:     nil,
			wantFindings: 1,
		},
		{
			name: "explicit scaleUp tolerance below threshold returns finding",
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{Tolerance: tol("0.1")},
			},
			wantFindings: 1,
		},
		{
			name: "explicit scaleDown tolerance above threshold returns no findings",
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{Tolerance: tol("0.5")},
			},
			wantFindings: 0,
		},
		{
			name: "scaleUp tolerance at threshold returns no findings",
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{Tolerance: tol("0.3")},
			},
			wantFindings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpa := &autoscalingv2.HorizontalPodAutoscaler{Spec: autoscalingv2.HorizontalPodAutoscalerSpec{Behavior: tt.behavior}}
			got := batchToleranceRule(hpa, 1)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d: %+v", tt.wantFindings, len(got), got)
			}
			if tt.wantFindings > 0 && got[0].ID != "batch-tolerance" {
				t.Fatalf("expected batch-tolerance finding, got %q", got[0].ID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// kedaCooldownRule
// ---------------------------------------------------------------------------

func TestKedaCooldownRule(t *testing.T) {
	mk := func(window *int32) *autoscalingv2.HorizontalPodAutoscaler {
		return &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: window},
				},
			},
		}
	}

	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		wantFindings int
	}{
		{name: "nil scaleDown returns no findings", hpa: &autoscalingv2.HorizontalPodAutoscaler{}, wantFindings: 0},
		{name: "window over 300s returns finding", hpa: mk(ptr.To(int32(600))), wantFindings: 1},
		{name: "window at 300s returns no findings", hpa: mk(ptr.To(int32(300))), wantFindings: 0},
		{name: "nil window returns no findings", hpa: mk(nil), wantFindings: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kedaCooldownRule(tt.hpa, 1)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d: %+v", tt.wantFindings, len(got), got)
			}
			if tt.wantFindings > 0 && got[0].ID != "keda-cooldown" {
				t.Fatalf("expected keda-cooldown finding, got %q", got[0].ID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// kedaScaleToZeroRule
// ---------------------------------------------------------------------------

func TestKedaScaleToZeroRule(t *testing.T) {
	tests := []struct {
		name         string
		minReplicas  int32
		wantFindings int
	}{
		{name: "minReplicas zero returns no findings", minReplicas: 0, wantFindings: 0},
		{name: "minReplicas above zero recommends scale-to-zero", minReplicas: 2, wantFindings: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kedaScaleToZeroRule(nil, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d: %+v", tt.wantFindings, len(got), got)
			}
			if tt.wantFindings > 0 && got[0].ID != "keda-scale-to-zero" {
				t.Fatalf("expected keda-scale-to-zero finding, got %q", got[0].ID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// costScaleDownRule
// ---------------------------------------------------------------------------

func TestCostScaleDownRule(t *testing.T) {
	mk := func(window *int32) *autoscalingv2.HorizontalPodAutoscaler {
		return &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: window},
				},
			},
		}
	}

	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		wantFindings int
	}{
		{name: "nil scaleDown returns no findings", hpa: &autoscalingv2.HorizontalPodAutoscaler{}, wantFindings: 0},
		{name: "window over 120s returns finding", hpa: mk(ptr.To(int32(300))), wantFindings: 1},
		{name: "window at 120s returns no findings", hpa: mk(ptr.To(int32(120))), wantFindings: 0},
		{name: "nil window returns no findings", hpa: mk(nil), wantFindings: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := costScaleDownRule(tt.hpa, 1)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d: %+v", tt.wantFindings, len(got), got)
			}
			if tt.wantFindings > 0 && got[0].ID != "cost-scaledown-window" {
				t.Fatalf("expected cost-scaledown-window finding, got %q", got[0].ID)
			}
		})
	}
}
