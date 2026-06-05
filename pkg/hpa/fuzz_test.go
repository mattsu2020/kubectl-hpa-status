package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func FuzzAnalyze(f *testing.F) {
	minReplicas := int32(2)
	seed := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "fuzz", Generation: 1},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    &minReplicas,
			MaxReplicas:    10,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 5,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
			},
		},
	}
	f.Add(0, 0, 0, 0, 0, 0, true, true)
	f.Fuzz(func(_ *testing.T, current, desired, minVal, maxVal, metricCurrent, metricTarget int, hasMetrics, hasConditions bool) {
		// Clamp values to reasonable ranges
		if minVal < 0 {
			minVal = 0
		}
		if maxVal < minVal {
			maxVal = minVal
		}
		if current < 0 {
			current = 0
		}
		if desired < 0 {
			desired = 0
		}
		if metricTarget <= 0 {
			metricTarget = 1
		}
		if metricCurrent < 0 {
			metricCurrent = 0
		}

		hpa := seed.DeepCopy()
		hpa.Status.CurrentReplicas = int32(current)
		hpa.Status.DesiredReplicas = int32(desired)
		minR := int32(minVal)
		hpa.Spec.MinReplicas = &minR
		hpa.Spec.MaxReplicas = int32(maxVal)

		if hasMetrics {
			metricCur := int32(metricCurrent)
			metricTgt := int32(metricTarget)
			hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &metricTgt,
						},
					},
				},
			}
			hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricStatus{
						Name: corev1.ResourceCPU,
						Current: autoscalingv2.MetricValueStatus{
							AverageUtilization: &metricCur,
						},
					},
				},
			}
		} else {
			hpa.Spec.Metrics = nil
			hpa.Status.CurrentMetrics = nil
		}

		if hasConditions {
			hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
			}
		} else {
			hpa.Status.Conditions = nil
		}

		// These calls should never panic
		analysis := Analyze(hpa, true)
		_ = analysis.Summary
		_ = analysis.Health
		_ = analysis.HealthScore

		_, _ = Health(hpa, int32(minVal))
		_ = HealthWithWeights(hpa, int32(minVal), HealthWeights{})
		_ = SummarizeDirection(hpa, int32(minVal))
		_ = Interpret(hpa, int32(minVal))
		_ = RecommendedActions(hpa, int32(minVal))
		_ = BuildSuggestions(hpa, int32(minVal))
		_ = DebugLines(hpa, analysis)
		_, _ = MetricOutsideTarget(hpa)
		_, _ = MostInfluentialMetric(hpa)
		_ = FormatBehavior(hpa)
		_ = ExternalMetricDiagnostics(hpa)
		_ = ObjectMetricDiagnostics(hpa)
		_ = KEDADiagnostics(hpa)
	})
}

func FuzzAnalyzeNil(f *testing.F) {
	f.Add(0)
	f.Fuzz(func(_ *testing.T, _ int) {
		// nil HPA should never panic
		analysis := Analyze(nil, true)
		_ = analysis.Health
	})
}
