package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMetricIDDistinguishesContainerAndSelector(t *testing.T) {
	containerA, err := MetricIDFromSpec(autoscalingv2.MetricSpec{
		Type: autoscalingv2.ContainerResourceMetricSourceType,
		ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
			Name: corev1.ResourceCPU, Container: "app",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	containerB := containerA
	containerB.Container = "sidecar"
	if containerA == containerB {
		t.Fatal("container name must be part of metric identity")
	}

	externalA, err := MetricIDFromSpec(autoscalingv2.MetricSpec{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricSource{Metric: autoscalingv2.MetricIdentifier{
			Name:     "queue_depth",
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "a"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	externalB := externalA
	externalB.Selector = "queue=b"
	if externalA == externalB {
		t.Fatal("selector must be part of metric identity")
	}
}

func TestMetricIdentityMatchesSpecAndStatus(t *testing.T) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "jobs"}}
	spec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricSource{Metric: autoscalingv2.MetricIdentifier{
			Name: "queue_depth", Selector: selector,
		}},
	}
	status := autoscalingv2.MetricStatus{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricStatus{Metric: autoscalingv2.MetricIdentifier{
			Name: "queue_depth", Selector: selector.DeepCopy(),
		}},
	}
	if !metricIdentityMatches(spec, status) {
		t.Fatal("equivalent metric identities did not match")
	}
	status.External.Metric.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "other"}}
	if metricIdentityMatches(spec, status) {
		t.Fatal("different selectors unexpectedly matched")
	}
}

func TestMetricIDRejectsInvalidSelector(t *testing.T) {
	_, err := MetricIDFromSpec(autoscalingv2.MetricSpec{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricSource{Metric: autoscalingv2.MetricIdentifier{
			Name: "queue_depth",
			Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: "queue", Operator: metav1.LabelSelectorOperator("invalid"),
			}}},
		}},
	})
	if err == nil {
		t.Fatal("invalid selector should return an error")
	}
}
