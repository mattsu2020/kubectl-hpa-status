package hpa

import (
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func baseHPA() *autoscalingv2.HorizontalPodAutoscaler {
	return testutil.BuildHPA("default", "web",
		testutil.WithMinMax(2, 10),
		testutil.WithReplicas(2, 2),
		testutil.WithGeneration(1),
		testutil.WithScaleTargetRef("Deployment", "web"),
		testutil.WithConditions(
			autoscalingv2.HorizontalPodAutoscalerCondition{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		),
	)
}

func containsSuggestion(suggestions []Suggestion, title string) bool {
	for _, suggestion := range suggestions {
		if suggestion.Title == title {
			return true
		}
	}
	return false
}

func resourceMetricSpec(name corev1.ResourceName, target int32) autoscalingv2.MetricSpec {
	return autoscalingv2.MetricSpec{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: name,
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: &target,
			},
		},
	}
}

func resourceMetricStatus(name corev1.ResourceName, current int32) autoscalingv2.MetricStatus {
	return autoscalingv2.MetricStatus{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricStatus{
			Name: name,
			Current: autoscalingv2.MetricValueStatus{
				AverageUtilization: &current,
			},
		},
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// boolPtr returns a pointer to b, used for optional table-driven bool fields.

func boolPtr(b bool) *bool { return &b }

// TestWriteStatusTextWithOptions_RendersWarnings verifies that Analysis.Warnings
// are surfaced in plain-text output. Previously Warnings only appeared in
// JSON/YAML output, leaving text users unaware of enrichment failures.
