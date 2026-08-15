package vpa

import (
	"slices"
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These scenario tests were ported from the pkg/hpa facade test suite when the
// AnalyzeVPA compatibility forwarder was removed in v3.

func TestAnalyze_CPUUtilizationConflict(t *testing.T) {
	hpa := conflictScenarioHPA(corev1.ResourceCPU, 80)

	v := &Info{
		Name:       "web-vpa",
		TargetRef:  "Deployment/web",
		TargetKind: "Deployment",
		TargetName: "web",
		UpdateMode: "Auto",
	}

	lines := Analyze(hpa, v)
	if len(lines) == 0 {
		t.Fatal("expected warning lines for CPU conflict, got none")
	}
	for _, want := range []string{"VPA", "conflicting", `updateMode to "Off"`, "Auto"} {
		if !containsLine(lines, want) {
			t.Fatalf("expected %q in warning, got %v", want, lines)
		}
	}
}

func TestAnalyze_MemoryConflictInitialMode(t *testing.T) {
	hpa := conflictScenarioHPA(corev1.ResourceMemory, 70)

	v := &Info{
		Name:       "app-vpa",
		TargetRef:  "Deployment/web",
		TargetKind: "Deployment",
		TargetName: "web",
		UpdateMode: "Initial",
	}

	lines := Analyze(hpa, v)
	if len(lines) == 0 {
		t.Fatal("expected warning lines for memory conflict, got none")
	}
	if !containsLine(lines, "conflicting") {
		t.Fatalf("expected conflicting warning, got %v", lines)
	}
	// Initial mode does not evict existing pods, so it should not get the
	// Auto-specific eviction warning.
	if containsLine(lines, "evict and resize pods") {
		t.Fatalf("should not contain Auto eviction warning for Initial mode, got %v", lines)
	}
}

func TestAnalyze_ExternalMetricsOnlyNoConflict(t *testing.T) {
	minReplicas := int32(1)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    &minReplicas,
			MaxReplicas:    10,
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

	v := &Info{
		Name:       "web-vpa",
		TargetRef:  "Deployment/web",
		TargetKind: "Deployment",
		TargetName: "web",
		UpdateMode: "Auto",
	}

	if lines := Analyze(hpa, v); lines != nil {
		t.Fatalf("expected no warning for external-only metrics, got %v", lines)
	}
}

func conflictScenarioHPA(resource corev1.ResourceName, targetUtil int32) *autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := int32(1)
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    &minReplicas,
			MaxReplicas:    10,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: resource,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetUtil,
						},
					},
				},
			},
		},
	}
}

func containsLine(lines []string, substr string) bool {
	return slices.ContainsFunc(lines, func(line string) bool {
		return strings.Contains(line, substr)
	})
}
