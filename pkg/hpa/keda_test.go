package hpa

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAnalyzeKEDA_TriggerMismatch(t *testing.T) {
	minReplicas := int32(1)
	target := resource.MustParse("5")
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keda-hpa-worker",
			Namespace: "default",
			Labels:    map[string]string{"scaledobject.keda.sh/name": "worker"},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 50,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricSource{
						Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
						Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
					},
				},
			},
		},
	}

	keda := &KEDAAnalysis{
		ScaledObjectName: "worker",
		Triggers: []KEDATriggerSummary{
			{Type: "kafka", Name: "my-topic"},
		},
	}

	lines := AnalyzeKEDA(hpa, keda)

	if !containsKEDALine(lines, "no matching KEDA trigger") {
		t.Fatalf("expected trigger mismatch warning, got %v", lines)
	}
	if !containsKEDALine(lines, "1 trigger(s)") {
		t.Fatalf("expected trigger count, got %v", lines)
	}
}

func TestAnalyzeKEDA_PollingIntervalMismatch(t *testing.T) {
	minReplicas := int32(1)
	window := int32(600)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keda-hpa-worker",
			Namespace: "default",
			Labels:    map[string]string{"scaledobject.keda.sh/name": "worker"},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 50,
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &window,
				},
			},
		},
	}

	polling := int32(30)
	keda := &KEDAAnalysis{
		ScaledObjectName: "worker",
		PollingInterval:  &polling,
		Triggers:         []KEDATriggerSummary{{Type: "cpu"}},
	}

	lines := AnalyzeKEDA(hpa, keda)

	if !containsKEDALine(lines, "polling interval is 30s but HPA scaleDown stabilization is 600s") {
		t.Fatalf("expected polling mismatch, got %v", lines)
	}
}

func TestAnalyzeKEDA_ReplicaBoundMismatch(t *testing.T) {
	minReplicas := int32(2)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keda-hpa-worker",
			Namespace: "default",
			Labels:    map[string]string{"scaledobject.keda.sh/name": "worker"},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
		},
	}

	kedaMin := int32(1)
	kedaMax := int32(100)
	keda := &KEDAAnalysis{
		ScaledObjectName: "worker",
		MinReplicaCount:  &kedaMin,
		MaxReplicaCount:  &kedaMax,
		Triggers:         []KEDATriggerSummary{{Type: "cpu"}},
	}

	lines := AnalyzeKEDA(hpa, keda)

	if !containsKEDALine(lines, "minReplicaCount=1 differs from HPA minReplicas=2") {
		t.Fatalf("expected min mismatch, got %v", lines)
	}
	if !containsKEDALine(lines, "maxReplicaCount=100 differs from HPA maxReplicas=10") {
		t.Fatalf("expected max mismatch, got %v", lines)
	}
}

func TestAnalyzeKEDA_NilInputs(t *testing.T) {
	if lines := AnalyzeKEDA(nil, &KEDAAnalysis{}); lines != nil {
		t.Fatalf("expected nil for nil HPA, got %v", lines)
	}
	if lines := AnalyzeKEDA(&autoscalingv2.HorizontalPodAutoscaler{}, nil); lines != nil {
		t.Fatalf("expected nil for nil KEDA, got %v", lines)
	}
}

func TestAnalyzeKEDA_NoTriggers(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keda-hpa-worker",
			Namespace: "default",
			Labels:    map[string]string{"scaledobject.keda.sh/name": "worker"},
		},
	}
	keda := &KEDAAnalysis{
		ScaledObjectName: "worker",
	}

	lines := AnalyzeKEDA(hpa, keda)
	if !containsKEDALine(lines, "no triggers defined") {
		t.Fatalf("expected no-triggers warning, got %v", lines)
	}
}

func containsKEDALine(lines []string, substr string) bool {
	for _, line := range lines {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

func init() {
	// Ensure corev1 is imported for condition checks.
	_ = corev1.ConditionTrue
}
