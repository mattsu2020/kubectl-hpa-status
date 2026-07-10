package kube

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestExtractVPAInfo(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      "web-vpa",
				"namespace": "default",
			},
			"spec": map[string]any{
				"targetRef": map[string]any{
					"kind": "Deployment",
					"name": "web",
				},
				"updatePolicy": map[string]any{
					"updateMode": "Auto",
				},
			},
		},
	}

	info := ExtractVPAInfo(u)

	if info.Name != "web-vpa" {
		t.Fatalf("expected name 'web-vpa', got %q", info.Name)
	}
	if info.TargetRef != "Deployment/web" {
		t.Fatalf("expected targetRef 'Deployment/web', got %q", info.TargetRef)
	}
	if info.TargetKind != "Deployment" {
		t.Fatalf("expected targetKind 'Deployment', got %q", info.TargetKind)
	}
	if info.TargetName != "web" {
		t.Fatalf("expected targetName 'web', got %q", info.TargetName)
	}
	if info.UpdateMode != "Auto" {
		t.Fatalf("expected updateMode 'Auto', got %q", info.UpdateMode)
	}
}

func TestExtractVPAInfo_RecommendationsAndControlledResources(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{"name": "web-vpa"},
			"spec": map[string]any{
				"targetRef": map[string]any{"kind": "Deployment", "name": "web"},
				"resourcePolicy": map[string]any{
					"containerPolicies": []any{
						map[string]any{"controlledResources": []any{"cpu", "memory"}},
					},
				},
			},
			"status": map[string]any{
				"recommendation": map[string]any{
					"containerRecommendations": []any{
						map[string]any{
							"containerName": "app",
							"target":        map[string]any{"cpu": "250m", "memory": "256Mi"},
							"lowerBound":    map[string]any{"cpu": "100m"},
							"upperBound":    map[string]any{"memory": "512Mi"},
						},
					},
				},
			},
		},
	}

	info := ExtractVPAInfo(u)
	if len(info.ControlledResources) != 2 || info.ControlledResources[0] != "cpu" || info.ControlledResources[1] != "memory" {
		t.Fatalf("unexpected controlled resources: %#v", info.ControlledResources)
	}
	if len(info.Recommendations) != 2 {
		t.Fatalf("expected cpu and memory recommendations, got %#v", info.Recommendations)
	}
	if info.Recommendations[0].Container != "app" || info.Recommendations[0].Resource != "cpu" || info.Recommendations[0].Target != "250m" {
		t.Fatalf("unexpected cpu recommendation: %#v", info.Recommendations[0])
	}
}

func TestExtractVPAInfo_RecommenderMode(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name": "web-vpa",
			},
			"spec": map[string]any{
				"targetRef": map[string]any{
					"kind": "Deployment",
					"name": "web",
				},
				"updatePolicy": map[string]any{
					"updateMode": "Recommender",
				},
			},
		},
	}

	info := ExtractVPAInfo(u)
	if info.UpdateMode != "Recommender" {
		t.Fatalf("expected updateMode 'Recommender', got %q", info.UpdateMode)
	}
}

func TestExtractVPAInfo_OffMode(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name": "web-vpa",
			},
			"spec": map[string]any{
				"targetRef": map[string]any{
					"kind": "Deployment",
					"name": "web",
				},
				"updatePolicy": map[string]any{
					"updateMode": "Off",
				},
			},
		},
	}

	info := ExtractVPAInfo(u)
	if info.UpdateMode != "Off" {
		t.Fatalf("expected updateMode 'Off', got %q", info.UpdateMode)
	}
}

func TestExtractVPAInfo_Nil(t *testing.T) {
	info := ExtractVPAInfo(nil)
	if info.Name != "" {
		t.Fatalf("expected empty name for nil input, got %q", info.Name)
	}
}

func TestExtractVPAInfo_NoSpec(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name": "web-vpa",
			},
		},
	}

	info := ExtractVPAInfo(u)
	if info.Name != "web-vpa" {
		t.Fatalf("expected name 'web-vpa', got %q", info.Name)
	}
	if info.TargetRef != "" {
		t.Fatalf("expected empty targetRef when spec is missing, got %q", info.TargetRef)
	}
}

func TestExtractVPAInfo_NoUpdatePolicy(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name": "web-vpa",
			},
			"spec": map[string]any{
				"targetRef": map[string]any{
					"kind": "Deployment",
					"name": "web",
				},
			},
		},
	}

	info := ExtractVPAInfo(u)
	if info.UpdateMode != "" {
		t.Fatalf("expected empty updateMode when updatePolicy is missing, got %q", info.UpdateMode)
	}
	if info.TargetRef != "Deployment/web" {
		t.Fatalf("expected targetRef 'Deployment/web', got %q", info.TargetRef)
	}
}

func TestHasResourceMetrics_CPUMetric(t *testing.T) {
	targetUtil := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetUtil,
						},
					},
				},
			},
		},
	}

	if !hasResourceMetrics(hpa) {
		t.Fatal("expected hasResourceMetrics=true for CPU metric")
	}
}

func TestHasResourceMetrics_MemoryMetric(t *testing.T) {
	targetUtil := int32(70)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceMemory,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetUtil,
						},
					},
				},
			},
		},
	}

	if !hasResourceMetrics(hpa) {
		t.Fatal("expected hasResourceMetrics=true for memory metric")
	}
}

func TestHasResourceMetrics_ExternalOnly(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricSource{
						Metric: autoscalingv2.MetricIdentifier{Name: "queue-depth"},
						Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType},
					},
				},
			},
		},
	}

	if hasResourceMetrics(hpa) {
		t.Fatal("expected hasResourceMetrics=false for external-only metrics")
	}
}

func TestHasResourceMetrics_NoMetrics(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if hasResourceMetrics(hpa) {
		t.Fatal("expected hasResourceMetrics=false for no metrics")
	}
}

func TestVPAControlsHPAResourceRequiresIntersection(t *testing.T) {
	target := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{Spec: autoscalingv2.HorizontalPodAutoscalerSpec{Metrics: []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU,
			Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &target}},
	}}}}
	if vpaControlsHPAResource(hpa, []string{"memory"}) {
		t.Fatal("memory-only VPA must not conflict with a CPU-only HPA")
	}
	if !vpaControlsHPAResource(hpa, []string{"cpu"}) {
		t.Fatal("CPU-controlled VPA should overlap a CPU HPA")
	}
	if !vpaControlsHPAResource(hpa, nil) {
		t.Fatal("omitted controlledResources defaults to cpu and memory")
	}
}
