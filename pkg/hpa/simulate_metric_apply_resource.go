package hpa

import (
	"fmt"
	"strconv"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

// parseMetricQuantity parses a value string into a resource.Quantity, wrapping
// the parse error with metricType/name context.
func parseMetricQuantity(value, metricType string) (resource.Quantity, error) {
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("invalid %s metric quantity %q: %w", metricType, value, err)
	}
	return q, nil
}

// applyResourceMetricOverride preserves the value field selected by the
// metric target so ratio projection remains aligned with the spec.
func applyResourceMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	resName := spec.Resource.Name
	switch spec.Resource.Target.Type {
	case autoscalingv2.UtilizationMetricType:
		parsed, err := strconv.ParseInt(strings.TrimSuffix(value, "%"), 10, 32)
		if err != nil {
			return fmt.Errorf("invalid utilization value %q: %w", value, err)
		}
		util := int32(parsed)
		hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
			Name: resName,
			Current: autoscalingv2.MetricValueStatus{
				AverageUtilization: &util,
			},
		}
	case autoscalingv2.AverageValueMetricType:
		q, err := parseMetricQuantity(value, "resource")
		if err != nil {
			return err
		}
		hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
			Name: resName,
			Current: autoscalingv2.MetricValueStatus{
				AverageValue: &q,
			},
		}
	default:
		return fmt.Errorf("unsupported resource metric target type %q", spec.Resource.Target.Type)
	}
	return nil
}
