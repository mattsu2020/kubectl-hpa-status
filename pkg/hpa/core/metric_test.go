package core

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

type testFormatter struct{}

func (testFormatter) FormatStatus(_ *autoscalingv2.HorizontalPodAutoscaler, status autoscalingv2.MetricStatus) Metric {
	return Metric{Type: string(status.Type), Text: "formatted"}
}

func TestFormatMetricStatus(t *testing.T) {
	status := autoscalingv2.MetricStatus{Type: autoscalingv2.ResourceMetricSourceType}
	got := FormatMetricStatus(nil, status, func(autoscalingv2.MetricSourceType) MetricStatusFormatter { return testFormatter{} })
	if got.Text != "formatted" {
		t.Fatalf("Text = %q", got.Text)
	}
	if got := FormatMetricStatus(nil, autoscalingv2.MetricStatus{}, nil); got.Text == "" {
		t.Fatal("empty status must retain fallback text")
	}
}
