package simulate

import (
	"fmt"
	"strconv"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// ApplyContainerResourceMetricOverride sets the current utilization or
// average value of a ContainerResource metric, matching the spec target type.
func ApplyContainerResourceMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	current := autoscalingv2.MetricValueStatus{}
	if spec.ContainerResource.Target.Type == autoscalingv2.UtilizationMetricType {
		parsed, err := strconv.ParseInt(strings.TrimSuffix(value, "%"), 10, 32)
		if err != nil {
			return fmt.Errorf("invalid utilization value %q: %w", value, err)
		}
		utilization := int32(parsed)
		current.AverageUtilization = &utilization
	} else {
		quantity, err := parseMetricQuantity(value, "container resource")
		if err != nil {
			return err
		}
		current.AverageValue = &quantity
	}
	hpa.Status.CurrentMetrics[idx].ContainerResource = &autoscalingv2.ContainerResourceMetricStatus{
		Name: spec.ContainerResource.Name, Container: spec.ContainerResource.Container, Current: current,
	}
	return nil
}
