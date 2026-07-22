package testutil

import (
	"context"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewFakeClient(t *testing.T) {
	hpa := BuildHPA("default", "web")
	client := NewFakeClient(hpa)
	got, err := client.AutoscalingV2().HorizontalPodAutoscalers("default").Get(context.Background(), "web", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected the fake client to be pre-loaded with the HPA: %v", err)
	}
	if got.Name != "web" {
		t.Fatalf("got.Name = %q, want web", got.Name)
	}
}

func TestNewFakeClientWithEvents(t *testing.T) {
	hpa := BuildHPA("default", "web")
	event := BuildEvent("default", "web", "SuccessfulRescale", "New size: 5")
	client := NewFakeClientWithEvents([]*autoscalingv2.HorizontalPodAutoscaler{hpa}, []*corev1.Event{event})

	events, err := client.CoreV1().Events("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("unexpected error listing events: %v", err)
	}
	if len(events.Items) != 1 || events.Items[0].Reason != "SuccessfulRescale" {
		t.Fatalf("expected the fake client to be pre-loaded with the event, got %+v", events.Items)
	}
}

func TestWithReplicas(t *testing.T) {
	hpa := BuildHPA("default", "web", WithReplicas(3, 5))
	if hpa.Status.CurrentReplicas != 3 || hpa.Status.DesiredReplicas != 5 {
		t.Fatalf("unexpected status: %+v", hpa.Status)
	}
}

func TestWithMinMax(t *testing.T) {
	hpa := BuildHPA("default", "web", WithMinMax(2, 20))
	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != 2 || hpa.Spec.MaxReplicas != 20 {
		t.Fatalf("unexpected spec: %+v", hpa.Spec)
	}
}

func TestWithConditions(t *testing.T) {
	cond := autoscalingv2.HorizontalPodAutoscalerCondition{Type: autoscalingv2.AbleToScale, Status: corev1.ConditionTrue}
	hpa := BuildHPA("default", "web", WithConditions(cond))
	if len(hpa.Status.Conditions) != 1 || hpa.Status.Conditions[0].Type != autoscalingv2.AbleToScale {
		t.Fatalf("unexpected conditions: %+v", hpa.Status.Conditions)
	}
}

func TestWithScalingActiveFalse(t *testing.T) {
	hpa := BuildHPA("default", "web",
		WithConditions(autoscalingv2.HorizontalPodAutoscalerCondition{Type: autoscalingv2.ScalingActive, Status: corev1.ConditionTrue}),
		WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	if len(hpa.Status.Conditions) != 1 {
		t.Fatalf("expected the prior ScalingActive condition to be replaced, got %+v", hpa.Status.Conditions)
	}
	cond := hpa.Status.Conditions[0]
	if cond.Status != corev1.ConditionFalse || cond.Reason != "FailedGetResourceMetric" {
		t.Fatalf("unexpected condition: %+v", cond)
	}
}

func TestWithScalingLimitedTrue(t *testing.T) {
	hpa := BuildHPA("default", "web", WithScalingLimitedTrue("TooManyReplicas"))
	found := false
	for _, c := range hpa.Status.Conditions {
		if c.Type == autoscalingv2.ScalingLimited && c.Status == corev1.ConditionTrue && c.Reason == "TooManyReplicas" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a ScalingLimited=True/TooManyReplicas condition, got %+v", hpa.Status.Conditions)
	}
}

func TestBuildEvent(t *testing.T) {
	event := BuildEvent("default", "web", "SuccessfulRescale", "New size: 5")
	if event.Namespace != "default" || event.InvolvedObject.Name != "web" || event.Reason != "SuccessfulRescale" {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestBuildEventWithTimestamp(t *testing.T) {
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	event := BuildEventWithTimestamp("default", "web", "SuccessfulRescale", "New size: 5", ts)
	if !event.LastTimestamp.Time.Equal(ts) {
		t.Fatalf("LastTimestamp = %v, want %v", event.LastTimestamp.Time, ts)
	}
	if !event.EventTime.Time.Equal(ts) {
		t.Fatalf("EventTime = %v, want %v", event.EventTime.Time, ts)
	}
}

func TestWithExternalMetric(t *testing.T) {
	hpa := BuildHPA("default", "web", WithExternalMetric("queue_depth", "5"))
	if len(hpa.Spec.Metrics) != 1 || hpa.Spec.Metrics[0].External.Metric.Name != "queue_depth" {
		t.Fatalf("unexpected metrics: %+v", hpa.Spec.Metrics)
	}
	if len(hpa.Status.CurrentMetrics) != 0 {
		t.Fatalf("expected no current-metric status, got %+v", hpa.Status.CurrentMetrics)
	}
}

func TestWithExternalMetricWithStatus(t *testing.T) {
	hpa := BuildHPA("default", "web", WithExternalMetricWithStatus("queue_depth", "5", "8"))
	if len(hpa.Status.CurrentMetrics) != 1 {
		t.Fatalf("expected one current-metric status, got %+v", hpa.Status.CurrentMetrics)
	}
	got := hpa.Status.CurrentMetrics[0].External.Current.Value
	if got == nil || got.String() != "8" {
		t.Fatalf("unexpected current value: %v", got)
	}
}

func TestWithScaleDownStabilized(t *testing.T) {
	hpa := BuildHPA("default", "web", WithScaleDownStabilized())
	found := false
	for _, c := range hpa.Status.Conditions {
		if c.Type == autoscalingv2.AbleToScale && c.Reason == "ScaleDownStabilized" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an AbleToScale/ScaleDownStabilized condition, got %+v", hpa.Status.Conditions)
	}
}

func TestWithScaleDownStabilizationWindow(t *testing.T) {
	hpa := BuildHPA("default", "web", WithScaleDownStabilizationWindow(600))
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil ||
		hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil ||
		*hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != 600 {
		t.Fatalf("unexpected behavior: %+v", hpa.Spec.Behavior)
	}
}

func TestWithKEDALabels(t *testing.T) {
	hpa := BuildHPA("default", "web", WithKEDALabels("web-so"))
	if hpa.Labels["app.kubernetes.io/managed-by"] != "keda-operator" {
		t.Fatalf("expected managed-by label, got %+v", hpa.Labels)
	}
	if hpa.Labels["scaledobject.keda.sh/name"] != "web-so" {
		t.Fatalf("expected scaledobject name label, got %+v", hpa.Labels)
	}
}

func TestWithDesiredAtMax(t *testing.T) {
	hpa := BuildHPA("default", "web", WithMinMax(1, 10), WithDesiredAtMax())
	if hpa.Status.CurrentReplicas != 10 || hpa.Status.DesiredReplicas != 10 {
		t.Fatalf("unexpected status: %+v", hpa.Status)
	}
}

func TestWithBehavior(t *testing.T) {
	behavior := &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleUp: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: int32Ptr(0)},
	}
	hpa := BuildHPA("default", "web", WithBehavior(behavior))
	if hpa.Spec.Behavior != behavior {
		t.Fatalf("expected behavior to be set directly, got %+v", hpa.Spec.Behavior)
	}
}

func int32Ptr(v int32) *int32 { return &v }
