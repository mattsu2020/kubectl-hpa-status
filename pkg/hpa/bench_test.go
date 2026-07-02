package hpa

import (
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/lint"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// benchHPA returns a realistic HPA fixture for benchmarking the analysis
// pipeline. It carries one resource metric (CPU) with both spec and status,
// a ScalingActive condition, and replica/min/max values that exercise the
// scoring and suggestion paths without hitting early returns.
func benchHPA() *autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := int32(2)
	targetUtil := int32(80)
	currentUtil := int32(95)
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "bench", Generation: 1},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    &minReplicas,
			MaxReplicas:    20,
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: &targetUtil,
					},
				},
			}},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 8,
			DesiredReplicas: 12,
			CurrentMetrics: []autoscalingv2.MetricStatus{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name: corev1.ResourceCPU,
					Current: autoscalingv2.MetricValueStatus{
						AverageUtilization: &currentUtil,
					},
				},
			}},
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: autoscalingv2.ScalingActive, Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
			},
		},
	}
}

// BenchmarkAnalyze measures the top-level analysis pipeline with
// interpretation enabled (the heaviest path, used by `status`).
func BenchmarkAnalyze(b *testing.B) {
	hpa := benchHPA()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Analyze(hpa, true)
	}
}

// BenchmarkAnalyzeNoInterpret measures the lighter analysis path (no
// interpretation text generation), used by list/batch rendering.
func BenchmarkAnalyzeNoInterpret(b *testing.B) {
	hpa := benchHPA()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Analyze(hpa, false)
	}
}

// BenchmarkAnalyzeWithOptions measures the phased core with custom health
// weights and debug enabled (the most feature-rich invocation).
func BenchmarkAnalyzeWithOptions(b *testing.B) {
	hpa := benchHPA()
	opts := AnalysisOptions{
		HealthWeights: HealthWeights{
			ScalingLimited:      IntWeight(20),
			ImplicitMaxReplicas: IntWeight(10),
			AtMinimumReplicas:   IntWeight(5),
		},
		Debug: true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = AnalyzeWithOptions(hpa, true, opts)
	}
}

// BenchmarkBuildSuggestions measures the suggestion rule engine.
func BenchmarkBuildSuggestions(b *testing.B) {
	hpa := benchHPA()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildSuggestions(hpa, 2)
	}
}

// BenchmarkLintHPA measures the lint rule evaluation.
func BenchmarkLintHPA(b *testing.B) {
	hpa := benchHPA()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lint.Run(hpa)
	}
}

// BenchmarkAuditHPA measures the audit rule evaluation.
func BenchmarkAuditHPA(b *testing.B) {
	hpa := benchHPA()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = audit.Run(hpa, 2)
	}
}
