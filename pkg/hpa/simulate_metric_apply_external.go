package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// applyExternalMetricOverride sets the current value of an External metric,
// choosing AverageValue vs Value from the spec target type.
func applyExternalMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	q, err := parseMetricQuantity(value, "external")
	if err != nil {
		return err
	}
	current := autoscalingv2.MetricValueStatus{}
	switch spec.External.Target.Type {
	case autoscalingv2.AverageValueMetricType:
		current.AverageValue = &q
	case autoscalingv2.ValueMetricType:
		current.Value = &q
	default:
		return fmt.Errorf("unsupported external metric target type %q", spec.External.Target.Type)
	}
	hpa.Status.CurrentMetrics[idx].External = &autoscalingv2.ExternalMetricStatus{
		Metric:  spec.External.Metric,
		Current: current,
	}
	return nil
}
