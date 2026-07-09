package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildSuggestionsAddsBehaviorAndToleranceRecommendations(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 5
	hpa.Status.DesiredReplicas = 5
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceCPU, 70)}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceCPU, 75)}

	got := Analyze(hpa, true)
	if !containsSuggestion(got.Suggestions, "Add explicit scale-up policy") {
		t.Fatalf("expected scale-up policy suggestion, got %#v", got.Suggestions)
	}
	if !containsSuggestion(got.Suggestions, "Review scale-up tolerance") {
		t.Fatalf("expected tolerance suggestion, got %#v", got.Suggestions)
	}
}

func TestBuildSuggestions(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(hpa *autoscalingv2.HorizontalPodAutoscaler)
		wantSuggestion string
		wantPresent    bool
	}{
		{
			name: "NoRaiseMaxReplicasWhenCurrentReplicasZero",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 0
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
				}
			},
			wantSuggestion: "Raise maxReplicas",
			wantPresent:    false,
		},
		{
			name: "RaiseMaxReplicasWhenCurrentReplicasPositive",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 10
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
				}
			},
			wantSuggestion: "Raise maxReplicas",
			wantPresent:    true,
		},
		{
			name: "NoLowerMinReplicasWhenMinIsOne",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				minReplicas := int32(1)
				hpa.Spec.MinReplicas = &minReplicas
				hpa.Status.CurrentReplicas = 1
				hpa.Status.DesiredReplicas = 1
				hpa.Spec.MaxReplicas = 10
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooFewReplicas"},
				}
			},
			wantSuggestion: "Lower minReplicas",
			wantPresent:    false,
		},
		{
			name: "LowerMinReplicasWhenMinAboveOne",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				minReplicas := int32(3)
				hpa.Spec.MinReplicas = &minReplicas
				hpa.Status.CurrentReplicas = 3
				hpa.Status.DesiredReplicas = 3
				hpa.Spec.MaxReplicas = 10
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooFewReplicas"},
				}
			},
			wantSuggestion: "Lower minReplicas",
			wantPresent:    true,
		},
		{
			name: "NoShortenStabilizationAtDefault300s",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				window := int32(300)
				hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
				}
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
				}
			},
			wantSuggestion: "Shorten scale-down stabilization",
			wantPresent:    false,
		},
		{
			name: "ShortenStabilizationAtExplicitlyHighWindow",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				window := int32(600)
				hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
				}
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
				}
			},
			wantSuggestion: "Shorten scale-down stabilization",
			wantPresent:    true,
		},
		{
			name: "ShortenStabilizationAtExplicitlySetBelow300s",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				window := int32(120)
				hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
				}
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
				}
			},
			wantSuggestion: "Shorten scale-down stabilization",
			wantPresent:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := baseHPA()
			tc.setup(hpa)
			minReplicas := *hpa.Spec.MinReplicas
			suggestions := BuildSuggestions(hpa, minReplicas)
			got := containsSuggestion(suggestions, tc.wantSuggestion)
			if got != tc.wantPresent {
				t.Fatalf("suggestion %q present = %v, want %v (suggestions: %#v)", tc.wantSuggestion, got, tc.wantPresent, suggestions)
			}
		})
	}
}
