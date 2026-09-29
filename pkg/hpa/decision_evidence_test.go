package hpa

import (
	"testing"

	autoscaling "k8s.io/api/autoscaling/v2"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestToleranceEffectUsesEstimatedEvidence(t *testing.T) {
	target, current := int32(50), int32(51)
	h := &autoscaling.HorizontalPodAutoscaler{Spec: autoscaling.HorizontalPodAutoscalerSpec{MaxReplicas: 10, Metrics: []autoscaling.MetricSpec{{Type: autoscaling.ResourceMetricSourceType, Resource: &autoscaling.ResourceMetricSource{Name: core.ResourceCPU, Target: autoscaling.MetricTarget{Type: autoscaling.UtilizationMetricType, AverageUtilization: &target}}}}}, Status: autoscaling.HorizontalPodAutoscalerStatus{CurrentReplicas: 3, DesiredReplicas: 3, CurrentMetrics: []autoscaling.MetricStatus{{Type: autoscaling.ResourceMetricSourceType, Resource: &autoscaling.ResourceMetricStatus{Name: core.ResourceCPU, Current: autoscaling.MetricValueStatus{AverageUtilization: &current}}}}}}
	for _, configured := range []bool{false, true} {
		if configured {
			tolerance := resource.MustParse("0.1")
			h.Spec.Behavior = &autoscaling.HorizontalPodAutoscalerBehavior{
				ScaleUp:   &autoscaling.HPAScalingRules{Tolerance: &tolerance},
				ScaleDown: &autoscaling.HPAScalingRules{Tolerance: &tolerance},
			}
		}
		found := false
		for _, signal := range EstimateDecisionSignals(h) {
			if signal.Reason != "ToleranceEffect" {
				continue
			}
			found = true
			if signal.Classification != "estimated" || signal.Confidence != "medium" {
				t.Errorf("configured=%v: tolerance inference must be estimated: %+v", configured, signal)
			}
		}
		if !found {
			t.Fatalf("configured=%v: missing tolerance effect signal", configured)
		}
	}
}
