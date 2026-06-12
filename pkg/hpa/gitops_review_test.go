package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAnalyzeGitOpsReview_MaxReplicasDecreased(t *testing.T) {
	before := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 20,
			MinReplicas: int32Ptr(2),
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: "cpu",
						Target: autoscalingv2.MetricTarget{
							AverageUtilization: int32Ptr(70),
						},
					},
				},
			},
		},
	}
	after := before.DeepCopy()
	after.Spec.MaxReplicas = 5

	review := AnalyzeGitOpsReview([]GitOpsReviewInput{
		{Before: before, After: after, FilePath: "hpa.yaml"},
	})

	if review.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want %q", review.RiskLevel, "high")
	}

	found := false
	for _, f := range review.Findings {
		if f.Category == "maxReplicas" && f.Severity == "high" {
			found = true
		}
	}
	if !found {
		t.Error("expected high-severity finding for maxReplicas decrease")
	}
}

func TestAnalyzeGitOpsReview_StabilizationRemoved(t *testing.T) {
	before := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			MinReplicas: int32Ptr(2),
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: int32Ptr(300),
				},
			},
		},
	}
	after := before.DeepCopy()
	after.Spec.Behavior.ScaleDown.StabilizationWindowSeconds = int32Ptr(0)

	review := AnalyzeGitOpsReview([]GitOpsReviewInput{
		{Before: before, After: after, FilePath: "hpa.yaml"},
	})

	found := false
	for _, f := range review.Findings {
		if f.Category == "stabilization" && f.Severity == "medium" {
			found = true
		}
	}
	if !found {
		t.Error("expected medium finding for stabilization removal")
	}
}

func TestAnalyzeGitOpsReview_CPUTargetChanged(t *testing.T) {
	before := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			MinReplicas: int32Ptr(2),
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: "cpu",
						Target: autoscalingv2.MetricTarget{
							AverageUtilization: int32Ptr(70),
						},
					},
				},
			},
		},
	}
	after := before.DeepCopy()
	after.Spec.Metrics[0].Resource.Target.AverageUtilization = int32Ptr(95)

	review := AnalyzeGitOpsReview([]GitOpsReviewInput{
		{Before: before, After: after, FilePath: "hpa.yaml"},
	})

	found := false
	for _, f := range review.Findings {
		if f.Category == "target" {
			found = true
		}
	}
	if !found {
		t.Error("expected finding for CPU target change")
	}
}

func TestAnalyzeGitOpsReview_MetricRemoved(t *testing.T) {
	before := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			MinReplicas: int32Ptr(2),
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: "cpu",
						Target: autoscalingv2.MetricTarget{
							AverageUtilization: int32Ptr(70),
						},
					},
				},
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: "memory",
						Target: autoscalingv2.MetricTarget{
							AverageUtilization: int32Ptr(80),
						},
					},
				},
			},
		},
	}
	after := before.DeepCopy()
	after.Spec.Metrics = after.Spec.Metrics[:1] // remove memory metric

	review := AnalyzeGitOpsReview([]GitOpsReviewInput{
		{Before: before, After: after, FilePath: "hpa.yaml"},
	})

	found := false
	for _, f := range review.Findings {
		if f.Category == "metric" && f.Severity == "medium" {
			found = true
		}
	}
	if !found {
		t.Error("expected medium finding for removed metric")
	}
}

func TestAnalyzeGitOpsReview_NewManifest_NoMetrics(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			MinReplicas: int32Ptr(2),
			Metrics:     []autoscalingv2.MetricSpec{},
		},
	}

	review := AnalyzeGitOpsReview([]GitOpsReviewInput{
		{After: hpa, FilePath: "hpa.yaml"},
	})

	if review.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want %q", review.RiskLevel, "high")
	}
}

func TestAnalyzeGitOpsReview_NoChanges(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			MinReplicas: int32Ptr(2),
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: "cpu",
						Target: autoscalingv2.MetricTarget{
							AverageUtilization: int32Ptr(70),
						},
					},
				},
			},
		},
	}

	review := AnalyzeGitOpsReview([]GitOpsReviewInput{
		{Before: hpa.DeepCopy(), After: hpa.DeepCopy(), FilePath: "hpa.yaml"},
	})

	if review.RiskLevel != "none" {
		t.Errorf("RiskLevel = %q, want %q when no changes", review.RiskLevel, "none")
	}
	if len(review.Findings) != 0 {
		t.Errorf("Findings = %d, want 0 when no changes", len(review.Findings))
	}
}

func TestExtractCPUTarget(t *testing.T) {
	tests := []struct {
		name string
		hpa  *autoscalingv2.HorizontalPodAutoscaler
		want string
	}{
		{
			name: "with cpu target",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Metrics: []autoscalingv2.MetricSpec{
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricSource{
								Name: "cpu",
								Target: autoscalingv2.MetricTarget{
									AverageUtilization: int32Ptr(70),
								},
							},
						},
					},
				},
			},
			want: "70%",
		},
		{
			name: "no metrics",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Metrics: []autoscalingv2.MetricSpec{},
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCPUTarget(tt.hpa)
			if got != tt.want {
				t.Errorf("extractCPUTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Suppress unused import warning.
var _ = resource.MustParse
