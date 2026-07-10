package cmd

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildMetricContractResourceCurrentDataIsReadOnlyLookup(t *testing.T) {
	spec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name:   corev1.ResourceCPU,
			Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType},
		},
	}

	current := map[string]bool{}
	metric := buildMetricContractMetric(spec, current)
	if metric.HasCurrentData {
		t.Fatal("Resource metric without status.currentMetrics must report hasCurrentData=false")
	}
	if len(current) != 0 {
		t.Fatalf("building a spec metric must not mutate the current-data map: %#v", current)
	}

	current["Resource/cpu"] = true
	metric = buildMetricContractMetric(spec, current)
	if !metric.HasCurrentData {
		t.Fatal("matching Resource status metric must report hasCurrentData=true")
	}
}
