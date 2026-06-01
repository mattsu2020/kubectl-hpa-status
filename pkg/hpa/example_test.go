package hpa_test

import (
	"fmt"
	"os"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ExampleAnalyze() {
	minReplicas := int32(2)
	hpaObj := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    &minReplicas,
			MaxReplicas:    10,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: ptrToInt32(80),
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 5,
			CurrentMetrics: []autoscalingv2.MetricStatus{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricStatus{
						Name: corev1.ResourceCPU,
						Current: autoscalingv2.MetricValueStatus{
							AverageUtilization: ptrToInt32(90),
						},
					},
				},
			},
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
			},
		},
	}

	result := hpa.Analyze(hpaObj, false)
	_, _ = fmt.Fprintf(os.Stdout, "HPA: %s/%s\n", result.Namespace, result.Name)
	_, _ = fmt.Fprintf(os.Stdout, "Health: %s (score %d)\n", result.Health, result.HealthScore)
	_, _ = fmt.Fprintf(os.Stdout, "Replicas: current=%d desired=%d\n", result.Current, result.Desired)
	// Output:
	// HPA: default/web
	// Health: OK (score 100)
	// Replicas: current=3 desired=5
}

func ExampleAnalyzeWithOptions() {
	minReplicas := int32(1)
	hpaObj := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "api"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "api"},
			MinReplicas:    &minReplicas,
			MaxReplicas:    5,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 2,
			DesiredReplicas: 2,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
			},
		},
	}

	result := hpa.AnalyzeWithOptions(hpaObj, true, hpa.AnalysisOptions{
		Debug: true,
	})
	_, _ = fmt.Fprintf(os.Stdout, "Debug lines: %d\n", len(result.Debug))
	// Output:
	// Debug lines: 3
}

func ExampleHealth() {
	minReplicas := int32(2)
	hpaObj := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "worker"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "worker"},
			MinReplicas:    &minReplicas,
			MaxReplicas:    10,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 3,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
			},
		},
	}

	state, score := hpa.Health(hpaObj, minReplicas)
	_, _ = fmt.Fprintf(os.Stdout, "Health: %s, Score: %d\n", state, score)
	// Output:
	// Health: OK, Score: 100
}

func ptrToInt32(v int32) *int32 {
	return &v
}
