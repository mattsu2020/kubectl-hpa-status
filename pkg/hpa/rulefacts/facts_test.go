package rulefacts

import (
	"reflect"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func int32ptr(v int32) *int32 { return &v }

func hpaWithMetrics(metrics ...autoscalingv2.MetricSpec) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MaxReplicas:    10,
			Metrics:        metrics,
		},
	}
}

func resUsage(name corev1.ResourceName, util *int32) *autoscalingv2.MetricSpec {
	target := autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: util}
	return &autoscalingv2.MetricSpec{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{Name: name, Target: target}}
}

func TestResourceUtilizationTargets(t *testing.T) {
	tests := []struct {
		name string
		hpa  *autoscalingv2.HorizontalPodAutoscaler
		want []ResourceUtilizationTarget
	}{
		{name: "nil hpa", hpa: nil, want: nil},
		{name: "empty", hpa: hpaWithMetrics(), want: nil},
		{
			name: "cpu+memory utilization only",
			hpa: hpaWithMetrics(
				*resUsage(corev1.ResourceCPU, int32ptr(80)),
				*resUsage(corev1.ResourceMemory, int32ptr(50)),
			),
			want: []ResourceUtilizationTarget{
				{Resource: "cpu", Percent: 80},
				{Resource: "memory", Percent: 50},
			},
		},
		{
			name: "skips non-resource metric types",
			hpa: hpaWithMetrics(
				autoscalingv2.MetricSpec{Type: autoscalingv2.PodsMetricSourceType, Pods: &autoscalingv2.PodsMetricSource{}},
				autoscalingv2.MetricSpec{Type: autoscalingv2.ObjectMetricSourceType, Object: &autoscalingv2.ObjectMetricSource{}},
			),
			want: nil,
		},
		{
			name: "skips resource absolute quantity target",
			hpa: hpaWithMetrics(
				*resUsage(corev1.ResourceCPU, nil), // no AverageUtilization
			),
			want: nil,
		},
		{
			name: "skips container-resource metric",
			hpa: hpaWithMetrics(
				autoscalingv2.MetricSpec{Type: autoscalingv2.ContainerResourceMetricSourceType},
			),
			want: nil,
		},
		{
			name: "mixed list keeps only percent-based resource targets",
			hpa: hpaWithMetrics(
				autoscalingv2.MetricSpec{Type: autoscalingv2.PodsMetricSourceType, Pods: &autoscalingv2.PodsMetricSource{}},
				*resUsage(corev1.ResourceCPU, int32ptr(70)),
				*resUsage(corev1.ResourceMemory, nil),
			),
			want: []ResourceUtilizationTarget{{Resource: "cpu", Percent: 70}},
		},
		{
			name: "nil Resource field skipped",
			hpa: hpaWithMetrics(
				autoscalingv2.MetricSpec{Type: autoscalingv2.ResourceMetricSourceType, Resource: nil},
			),
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResourceUtilizationTargets(tt.hpa)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ResourceUtilizationTargets() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// Keep unused-import guard meaningful: resource.Quantity is part of the
// wirable HPA surface even if not asserted directly here.
var _ = resource.MustParse
