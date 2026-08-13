package hpa

import autoscalingv2 "k8s.io/api/autoscaling/v2"

// applyPodsMetricOverride sets the current AverageValue of a Pods metric.
func applyPodsMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	q, err := parseMetricQuantity(value, "pods")
	if err != nil {
		return err
	}
	hpa.Status.CurrentMetrics[idx].Pods = &autoscalingv2.PodsMetricStatus{
		Metric: autoscalingv2.MetricIdentifier{
			Name:     spec.Pods.Metric.Name,
			Selector: spec.Pods.Metric.Selector,
		},
		Current: autoscalingv2.MetricValueStatus{
			AverageValue: &q,
		},
	}
	return nil
}
