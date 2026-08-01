package analysis

import (
	"fmt"
	"io"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
)

// The fleet path (list, scan, tui) is what runs against clusters with
// hundreds of HPAs, and it had no benchmark: pkg/hpa/bench_test.go only covers
// single-HPA analysis. These benchmarks pin the per-HPA cost of the batch pass
// and of list rendering so a regression in either shows up as a measurable
// slowdown rather than as a user-reported hang on a large cluster.

// benchFleet builds n HPAs with enough spec/status detail to reach the health
// scoring, suggestion, and metric-correlation paths instead of early returns.
func benchFleet(n int) []autoscalingv2.HorizontalPodAutoscaler {
	hpas := make([]autoscalingv2.HorizontalPodAutoscaler, 0, n)
	for i := range n {
		minReplicas := int32(2)
		targetUtil := int32(80)
		// Vary utilization so roughly a third of the fleet is over target and
		// exercises the "scaling limited" and suggestion branches.
		currentUtil := int32(40 + (i%3)*30)
		hpas = append(hpas, autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  fmt.Sprintf("team-%d", i%10),
				Name:       fmt.Sprintf("workload-%d", i),
				Generation: 1,
			},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: fmt.Sprintf("workload-%d", i)},
				MinReplicas:    &minReplicas,
				MaxReplicas:    int32(10 + i%20),
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
				CurrentReplicas: int32(2 + i%8),
				DesiredReplicas: int32(2 + i%12),
				CurrentMetrics: []autoscalingv2.MetricStatus{{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricStatus{
						Name:    corev1.ResourceCPU,
						Current: autoscalingv2.MetricValueStatus{AverageUtilization: &currentUtil},
					},
				}},
				Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: autoscalingv2.ScalingActive, Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: autoscalingv2.AbleToScale, Status: corev1.ConditionTrue, Reason: "ReadyForNewScale"},
				},
			},
		})
	}
	return hpas
}

func benchmarkAnalyzeBatch(b *testing.B, size int) {
	b.Helper()
	hpas := benchFleet(size)
	opts := Options{HealthWeights: hpaanalysis.HealthWeights{}}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		results := AnalyzeBatch(hpas, opts, BatchEnrichment{})
		if len(results) != size {
			b.Fatalf("expected %d results, got %d", size, len(results))
		}
	}
}

// BenchmarkAnalyzeBatch100 is the common single-namespace `list` size.
func BenchmarkAnalyzeBatch100(b *testing.B) { benchmarkAnalyzeBatch(b, 100) }

// BenchmarkAnalyzeBatch1000 approximates `list -A` on a large cluster.
func BenchmarkAnalyzeBatch1000(b *testing.B) { benchmarkAnalyzeBatch(b, 1000) }

// BenchmarkAnalyzeBatchWithEnrichment measures the batch pass when KEDA
// enrichment is present, which additionally runs the enrichment health
// penalties for every item.
func BenchmarkAnalyzeBatchWithEnrichment(b *testing.B) {
	const size = 500
	hpas := benchFleet(size)
	enrichment := BatchEnrichment{
		KEDA:     make(map[string]*hpakeda.Analysis, size),
		Warnings: map[string][]string{"team-0": {"KEDA ScaledObject list failed"}},
	}
	for i := range hpas {
		hpa := &hpas[i]
		enrichment.KEDA[hpa.Namespace+"/"+hpa.Name] = &hpakeda.Analysis{
			ScaledObjectName: fmt.Sprintf("so-%d", i),
			Lines:            []string{"trigger: cpu"},
		}
	}
	opts := Options{HealthWeights: hpaanalysis.HealthWeights{}}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if results := AnalyzeBatch(hpas, opts, enrichment); len(results) != size {
			b.Fatalf("expected %d results, got %d", size, len(results))
		}
	}
}

// BenchmarkWriteListText measures the table renderer that follows the batch
// pass, which is the other half of what `list` spends time on.
func benchmarkWriteListText(b *testing.B, wide bool) {
	b.Helper()
	results := AnalyzeBatch(benchFleet(500), Options{HealthWeights: hpaanalysis.HealthWeights{}}, BatchEnrichment{})
	items := make([]hpaanalysis.ListItem, 0, len(results))
	for _, result := range results {
		items = append(items, result.ListItem)
	}
	report := hpaanalysis.ListReport{APIVersion: hpaanalysis.SchemaVersion, Items: items}
	opts := hpaanalysis.ListTextOptions{Wide: wide}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := hpaanalysis.WriteListText(io.Discard, report, opts); err != nil {
			b.Fatalf("WriteListText returned error: %v", err)
		}
	}
}

func BenchmarkWriteListText(b *testing.B)     { benchmarkWriteListText(b, false) }
func BenchmarkWriteListTextWide(b *testing.B) { benchmarkWriteListText(b, true) }
