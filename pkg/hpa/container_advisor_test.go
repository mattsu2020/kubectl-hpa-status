package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAnalyzeContainerAdvisor_SingleContainer(t *testing.T) {
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

	input := ContainerAdvisorInput{
		ContainerCount:     1,
		ContainerNames:     []string{"app"},
		UsesResourceMetric: true,
	}

	result := AnalyzeContainerAdvisor(hpa, input)
	if result != nil {
		t.Error("expected nil for single container pod")
	}
}

func TestAnalyzeContainerAdvisor_MultiContainer_ResourceMetric(t *testing.T) {
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

	input := ContainerAdvisorInput{
		ContainerCount:     3,
		ContainerNames:     []string{"app", "sidecar", "init"},
		UsesResourceMetric: true,
	}

	result := AnalyzeContainerAdvisor(hpa, input)
	if result == nil {
		t.Fatal("expected non-nil result for multi-container pod with Resource metric")
	}
	if result.Confidence != ConfidenceMedium {
		t.Errorf("expected medium confidence, got %s", result.Confidence)
	}
	if result.Finding == "" {
		t.Error("expected non-empty finding")
	}
	if result.SuggestedMetric == "" {
		t.Error("expected non-empty suggested metric")
	}
}

func TestAnalyzeContainerAdvisor_AlreadyUsingContainerResource(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ContainerResourceMetricSourceType,
					ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
						Name:      corev1.ResourceCPU,
						Container: "app",
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: int32Ptr(60),
						},
					},
				},
			},
		},
	}

	input := ContainerAdvisorInput{
		ContainerCount:              3,
		ContainerNames:              []string{"app", "sidecar", "init"},
		UsesResourceMetric:          false,
		UsesContainerResourceMetric: true,
	}

	result := AnalyzeContainerAdvisor(hpa, input)
	if result != nil {
		t.Error("expected nil when already using ContainerResource")
	}
}

func TestAnalyzeContainerAdvisor_NoResourceMetric(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricSource{
						Metric: autoscalingv2.MetricIdentifier{
							Name: "queue_depth",
						},
						Target: autoscalingv2.MetricTarget{
							Type:  autoscalingv2.ValueMetricType,
							Value: resourceQuantityPtr("100"),
						},
					},
				},
			},
		},
	}

	input := ContainerAdvisorInput{
		ContainerCount:     3,
		ContainerNames:     []string{"app", "sidecar", "init"},
		UsesResourceMetric: false,
	}

	result := AnalyzeContainerAdvisor(hpa, input)
	if result != nil {
		t.Error("expected nil when no Resource metric is used")
	}
}

func TestAnalyzeContainerAdvisor_PrefersAppContainer(t *testing.T) {
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

	input := ContainerAdvisorInput{
		ContainerCount:     3,
		ContainerNames:     []string{"sidecar", "app", "init"},
		UsesResourceMetric: true,
	}

	result := AnalyzeContainerAdvisor(hpa, input)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !containsString(result.SuggestedMetric, "container: app") {
		t.Errorf("expected suggested metric to target 'app' container, got: %s", result.SuggestedMetric)
	}
}

func TestAnalyzeContainerAdvisorWithMetrics(t *testing.T) {
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

	input := ContainerAdvisorInput{
		ContainerCount:     3,
		ContainerNames:     []string{"app", "sidecar", "init"},
		UsesResourceMetric: true,
	}

	metrics := []ContainerUsageHint{
		{Container: "app", CPUPercent: 80, MemoryPercent: 60},
		{Container: "sidecar", CPUPercent: 15, MemoryPercent: 20},
		{Container: "init", CPUPercent: 5, MemoryPercent: 10},
	}

	result := AnalyzeContainerAdvisorWithMetrics(hpa, input, metrics)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Confidence != ConfidenceHigh {
		t.Errorf("expected high confidence with metrics, got %s", result.Confidence)
	}
	if len(result.ContainerUsageHints) != 3 {
		t.Errorf("expected 3 hints, got %d", len(result.ContainerUsageHints))
	}
	// Check dominant container.
	for _, h := range result.ContainerUsageHints {
		if h.Container == "app" && !h.Dominant {
			t.Error("expected 'app' container to be marked as dominant")
		}
		if h.Container != "app" && h.Dominant {
			t.Errorf("expected '%s' container to not be dominant", h.Container)
		}
	}
}

func resourceQuantityPtr(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
