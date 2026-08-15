package simulate

import autoscalingv2 "k8s.io/api/autoscaling/v2"

// ApplyObjectMetricOverride sets the current value of an Object metric on the
// HPA status, choosing AverageValue vs Value from the spec target type.
func ApplyObjectMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	quantity, err := parseMetricQuantity(value, "object")
	if err != nil {
		return err
	}
	current := autoscalingv2.MetricValueStatus{}
	if spec.Object.Target.Type == autoscalingv2.AverageValueMetricType || spec.Object.Target.AverageValue != nil {
		current.AverageValue = &quantity
	} else {
		current.Value = &quantity
	}
	hpa.Status.CurrentMetrics[idx].Object = &autoscalingv2.ObjectMetricStatus{
		Metric:          spec.Object.Metric,
		DescribedObject: spec.Object.DescribedObject,
		Current:         current,
	}
	return nil
}
