package core

import (
	"k8s.io/apimachinery/pkg/api/resource"
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

func TestZeroQuantityIsAReading(t *testing.T) {
	zero := resource.MustParse("0")
	if got := FormatMetricValue(nil, &zero); got != "0" {
		t.Fatalf("average = %q", got)
	}
	for _, value := range []autoscalingv2.MetricValueStatus{{Value: &zero}, {AverageValue: &zero}} {
		if got := FormatMetricValueStatus(value); got != "0" {
			t.Fatalf("value = %q", got)
		}
	}
	if got := FormatMetricValueStatus(autoscalingv2.MetricValueStatus{}); got != "<unknown>" {
		t.Fatalf("missing = %q", got)
	}
}
