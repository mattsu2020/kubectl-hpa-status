package fleet

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// makeHPA builds a minimal HPA with explicit current/desired/maxReplicas
// counts for fleet aggregation testing. The scale target kind defaults to
// Deployment.
func makeHPA(namespace, name string, current, desired, maxReplicas int32) autoscalingv2.HorizontalPodAutoscaler {
	return autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: maxReplicas,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: name,
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: current,
			DesiredReplicas: desired,
		},
	}
}

func TestBuildReport_Aggregation(t *testing.T) {
	hpas := []autoscalingv2.HorizontalPodAutoscaler{
		// At maxReplicas: current == max, no additional headroom.
		makeHPA("ns-a", "at-max", 10, 10, 10),
		// Headroom: current=4, max=10 → 6 additional pods.
		makeHPA("ns-a", "headroom", 4, 4, 10),
		// current=0 falls back to desired=3; max=8 → 5 additional.
		makeHPA("ns-b", "zero-current", 0, 3, 8),
	}
	report := BuildReport(hpas, "max-surge")

	if report.HPAs != 3 {
		t.Fatalf("HPAs = %d, want 3", report.HPAs)
	}
	// Current pods: 10 + 4 + 3 (desired fallback) = 17
	if report.CurrentPods != 17 {
		t.Fatalf("CurrentPods = %d, want 17", report.CurrentPods)
	}
	// AtMaxReplicas counts only the first HPA.
	if report.AtMaxReplicas != 1 {
		t.Fatalf("AtMaxReplicas = %d, want 1", report.AtMaxReplicas)
	}
	// All three have no configured metrics.
	if report.WithoutConfiguredMetric != 3 {
		t.Fatalf("WithoutConfiguredMetric = %d, want 3", report.WithoutConfiguredMetric)
	}
	// Additional: 0 + 6 + 5 = 11
	if report.AdditionalPods != 11 {
		t.Fatalf("AdditionalPods = %d, want 11", report.AdditionalPods)
	}
	// Worst case: 10 + 10 + 8 = 28
	if report.WorstCasePods != 28 {
		t.Fatalf("WorstCasePods = %d, want 28", report.WorstCasePods)
	}
	// TopRisks only includes HPAs with additional > 0 (headroom, zero-current).
	if len(report.TopRisks) != 2 {
		t.Fatalf("TopRisks len = %d, want 2", len(report.TopRisks))
	}
	// Sorted by AdditionalPods descending: headroom(6) before zero-current(5).
	if report.TopRisks[0].Name != "headroom" {
		t.Fatalf("TopRisks[0].Name = %s, want headroom", report.TopRisks[0].Name)
	}
	if report.TopRisks[1].Name != "zero-current" {
		t.Fatalf("TopRisks[1].Name = %s, want zero-current", report.TopRisks[1].Name)
	}
}

func TestBuildReport_TopRisksTruncatedToTen(t *testing.T) {
	// 12 HPAs each with headroom → all qualify for TopRisks, but only 10 kept.
	hpas := make([]autoscalingv2.HorizontalPodAutoscaler, 12)
	for i := range hpas {
		hpas[i] = makeHPA("ns", "hpa", 1, 1, 2)
		hpas[i].Name = "hpa-" + string(rune('a'+i))
	}
	report := BuildReport(hpas, "max-surge")
	if len(report.TopRisks) != 10 {
		t.Fatalf("TopRisks len = %d, want 10 (truncation)", len(report.TopRisks))
	}
}

func TestBuildReport_TopRisksTieBreakByNamespaceThenName(t *testing.T) {
	// Equal AdditionalPods forces the secondary tie-breakers to decide order.
	hpas := []autoscalingv2.HorizontalPodAutoscaler{
		makeHPA("ns-b", "zzz", 1, 1, 3), // additional 2
		makeHPA("ns-a", "mmm", 1, 1, 3), // additional 2
		makeHPA("ns-a", "aaa", 1, 1, 3), // additional 2
	}
	report := BuildReport(hpas, "max-surge")
	if len(report.TopRisks) != 3 {
		t.Fatalf("TopRisks len = %d, want 3", len(report.TopRisks))
	}
	// Expected order: ns-a/aaa, ns-a/mmm, ns-b/zzz
	want := []string{"aaa", "mmm", "zzz"}
	for i, w := range want {
		if report.TopRisks[i].Name != w {
			t.Fatalf("TopRisks[%d].Name = %s, want %s", i, report.TopRisks[i].Name, w)
		}
	}
}

func TestBuildReport_EmptyInput(t *testing.T) {
	report := BuildReport(nil, "max-surge")
	if report.HPAs != 0 || report.CurrentPods != 0 || len(report.TopRisks) != 0 {
		t.Fatalf("empty input should yield zero report, got %+v", report)
	}
}

func TestBuildReport_NegativeAdditionalClampedToZero(t *testing.T) {
	// If current somehow exceeds max (shouldn't normally happen), additional
	// must be clamped to 0 rather than going negative.
	hpas := []autoscalingv2.HorizontalPodAutoscaler{
		makeHPA("ns", "over", 15, 15, 10),
	}
	report := BuildReport(hpas, "max-surge")
	if report.AdditionalPods != 0 {
		t.Fatalf("AdditionalPods = %d, want 0 (clamped)", report.AdditionalPods)
	}
	if len(report.TopRisks) != 0 {
		t.Fatalf("TopRisks len = %d, want 0 (no headroom)", len(report.TopRisks))
	}
}
