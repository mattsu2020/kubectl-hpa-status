package gitops

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestReviewIdentifiesCPURegardlessOfMetricOrder(t *testing.T) {
	for _, memoryFirst := range []bool{true, false} {
		metric := func(name corev1.ResourceName, target int32) autoscalingv2.MetricSpec {
			return autoscalingv2.MetricSpec{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{
				Name: name, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: ptr.To(target)},
			}}
		}
		before := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "web"}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 10,
			Metrics: []autoscalingv2.MetricSpec{metric(corev1.ResourceMemory, 60), metric(corev1.ResourceCPU, 70)},
		}}
		cpuIndex, memoryIndex := 1, 0
		if !memoryFirst {
			before.Spec.Metrics[0], before.Spec.Metrics[1] = before.Spec.Metrics[1], before.Spec.Metrics[0]
			cpuIndex, memoryIndex = 0, 1
		}
		after := before.DeepCopy()
		after.Spec.Metrics[memoryIndex].Resource.Target.AverageUtilization = ptr.To(int32(95))
		review := AnalyzeReview([]ReviewInput{{Before: before, After: after}})
		if len(review.Findings) != 0 {
			t.Fatalf("memory-only change reported as CPU change: %+v", review.Findings)
		}
		after.Spec.Metrics[cpuIndex].Resource.Target.AverageUtilization = ptr.To(int32(80))
		review = AnalyzeReview([]ReviewInput{{Before: before, After: after}})
		if len(review.Findings) != 1 || !strings.Contains(review.Findings[0].Message, "CPU target changed from 70% to 80%") {
			t.Fatalf("CPU change was not identified: %+v", review.Findings)
		}
		fresh := AnalyzeReview([]ReviewInput{{After: &autoscalingv2.HorizontalPodAutoscaler{Spec: autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 10, Metrics: []autoscalingv2.MetricSpec{metric(corev1.ResourceMemory, 95)}}}}})
		if len(fresh.Findings) != 0 {
			t.Fatalf("memory-only HPA reported as high CPU target: %+v", fresh.Findings)
		}
	}
}
