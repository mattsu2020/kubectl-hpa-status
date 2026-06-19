package vpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// TestAnalyzeVPA_NilGuards confirms the nil guards on the canonical AnalyzeVPA
// entry point (also exercised through the pkg/hpa facade in vpa_test.go).
func TestAnalyze_NilGuards(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if got := Analyze(hpa, nil); got != nil {
		t.Fatalf("Analyze(hpa, nil) = %v, want nil", got)
	}
	if got := Analyze(nil, &Info{}); got != nil {
		t.Fatalf("Analyze(nil, vpa) = %v, want nil", got)
	}
}

func TestAnalyze_OffModeSkips(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
				}},
			},
		},
	}
	v := &Info{UpdateMode: "Off"}
	if got := Analyze(hpa, v); got != nil {
		t.Fatalf("Analyze with Off mode = %v, want nil", got)
	}
}

func TestDetermineConflictLevel_AutoIsError(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
				}},
			},
		},
	}
	v := &ConflictInfo{UpdateMode: "Auto"}
	if got := determineConflictLevel(hpa, v); got != ConflictError {
		t.Fatalf("determineConflictLevel(Auto) = %q, want %q", got, ConflictError)
	}
}
