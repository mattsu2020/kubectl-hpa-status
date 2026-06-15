package hpa

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAnalyzeVPA_CPUConflict(t *testing.T) {
	minReplicas := int32(1)
	targetUtil := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
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

	vpa := &VPAInfo{
		Name:       "web-vpa",
		TargetRef:  "Deployment/web",
		TargetKind: "Deployment",
		TargetName: "web",
		UpdateMode: "Auto",
	}

	lines := AnalyzeVPA(hpa, vpa)
	if len(lines) == 0 {
		t.Fatal("expected warning lines for CPU conflict, got none")
	}

	if !containsVPALine(lines, "VPA") {
		t.Fatalf("expected VPA reference in warning, got %v", lines)
	}
	if !containsVPALine(lines, "conflicting") {
		t.Fatalf("expected conflicting warning, got %v", lines)
	}
	if !containsVPALine(lines, "Recommender") {
		t.Fatalf("expected Recommender suggestion, got %v", lines)
	}
	if !containsVPALine(lines, "Auto") {
		t.Fatalf("expected Auto mode warning, got %v", lines)
	}
}

func TestAnalyzeVPA_MemoryConflict(t *testing.T) {
	minReplicas := int32(1)
	targetUtil := int32(70)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-hpa",
			Namespace: "production",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "app",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 20,
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

	vpa := &VPAInfo{
		Name:       "app-vpa",
		TargetRef:  "Deployment/app",
		TargetKind: "Deployment",
		TargetName: "app",
		UpdateMode: "Recommender",
	}

	lines := AnalyzeVPA(hpa, vpa)
	if len(lines) == 0 {
		t.Fatal("expected warning lines for memory conflict, got none")
	}
	if !containsVPALine(lines, "conflicting") {
		t.Fatalf("expected conflicting warning, got %v", lines)
	}
	// Should NOT contain Auto-specific warning since mode is Recommender
	if containsVPALine(lines, "evict and resize pods") {
		t.Fatalf("should not contain Auto eviction warning for Recommender mode, got %v", lines)
	}
}

func TestAnalyzeVPA_ExternalMetricsOnly_NoConflict(t *testing.T) {
	minReplicas := int32(1)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
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

	vpa := &VPAInfo{
		Name:       "web-vpa",
		TargetRef:  "Deployment/web",
		TargetKind: "Deployment",
		TargetName: "web",
		UpdateMode: "Auto",
	}

	lines := AnalyzeVPA(hpa, vpa)
	if lines != nil {
		t.Fatalf("expected no warning for external-only metrics, got %v", lines)
	}
}

func TestAnalyzeVPA_NilVPA_NoOutput(t *testing.T) {
	minReplicas := int32(1)
	targetUtil := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
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

	lines := AnalyzeVPA(hpa, nil)
	if lines != nil {
		t.Fatalf("expected nil for nil VPA, got %v", lines)
	}
}

func TestAnalyzeVPA_NilHPA_NoOutput(t *testing.T) {
	vpa := &VPAInfo{
		Name:       "web-vpa",
		TargetKind: "Deployment",
		TargetName: "web",
		UpdateMode: "Auto",
	}

	lines := AnalyzeVPA(nil, vpa)
	if lines != nil {
		t.Fatalf("expected nil for nil HPA, got %v", lines)
	}
}

func TestAnalyzeVPA_OffMode_NoWarning(t *testing.T) {
	minReplicas := int32(1)
	targetUtil := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
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

	vpa := &VPAInfo{
		Name:       "web-vpa",
		TargetRef:  "Deployment/web",
		TargetKind: "Deployment",
		TargetName: "web",
		UpdateMode: "Off",
	}

	lines := AnalyzeVPA(hpa, vpa)
	if lines != nil {
		t.Fatalf("expected no warning for VPA in Off mode, got %v", lines)
	}
}

func TestAnalyzeVPA_BothNil_NoOutput(t *testing.T) {
	lines := AnalyzeVPA(nil, nil)
	if lines != nil {
		t.Fatalf("expected nil for both nil inputs, got %v", lines)
	}
}

func containsVPALine(lines []string, substr string) bool {
	for _, line := range lines {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}
