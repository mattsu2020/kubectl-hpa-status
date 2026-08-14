package simulate

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/tolerance"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

// applyRelativeOverride handles +/- percentage relative changes.
func applyRelativeOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		return applyRelativeResourceOverride(hpa, spec, idx, value)
	case autoscalingv2.ExternalMetricSourceType:
		return applyRelativeExternalOverride(hpa, spec, idx, value)
	default:
		return fmt.Errorf("relative overrides are only supported for Resource and External metrics, not %q", spec.Type)
	}
}

func applyRelativeResourceOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	current := hpa.Status.CurrentMetrics[idx].Resource
	if current == nil {
		return fmt.Errorf("cannot apply relative change: no current value for metric %q", spec.Resource.Name)
	}
	switch spec.Resource.Target.Type {
	case autoscalingv2.UtilizationMetricType:
		if current.Current.AverageUtilization == nil {
			return fmt.Errorf("cannot apply relative change: no current utilization for metric %q", spec.Resource.Name)
		}
		newValue, err := parseRelativeValue(value, *current.Current.AverageUtilization)
		if err != nil {
			return err
		}
		hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
			Name:    spec.Resource.Name,
			Current: autoscalingv2.MetricValueStatus{AverageUtilization: &newValue},
		}
	case autoscalingv2.AverageValueMetricType:
		if current.Current.AverageValue == nil {
			return fmt.Errorf("cannot apply relative change: no current average value for metric %q", spec.Resource.Name)
		}
		newValue, err := parseRelativeQuantity(value, current.Current.AverageValue)
		if err != nil {
			return err
		}
		hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
			Name:    spec.Resource.Name,
			Current: autoscalingv2.MetricValueStatus{AverageValue: &newValue},
		}
	default:
		return fmt.Errorf("unsupported resource metric target type %q", spec.Resource.Target.Type)
	}
	return nil
}

func applyRelativeExternalOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	current := hpa.Status.CurrentMetrics[idx].External
	if current == nil {
		return fmt.Errorf("cannot apply relative change: no current value for external metric %q", spec.External.Metric.Name)
	}
	var currentQuantity *resource.Quantity
	switch spec.External.Target.Type {
	case autoscalingv2.AverageValueMetricType:
		currentQuantity = current.Current.AverageValue
	case autoscalingv2.ValueMetricType:
		currentQuantity = current.Current.Value
	default:
		return fmt.Errorf("unsupported external metric target type %q", spec.External.Target.Type)
	}
	if currentQuantity == nil {
		return fmt.Errorf("cannot apply relative change: current value shape does not match %q target for external metric %q", spec.External.Target.Type, spec.External.Metric.Name)
	}
	newValue, err := parseRelativeQuantity(value, currentQuantity)
	if err != nil {
		return err
	}
	next := autoscalingv2.MetricValueStatus{}
	if spec.External.Target.Type == autoscalingv2.AverageValueMetricType {
		next.AverageValue = &newValue
	} else {
		next.Value = &newValue
	}
	hpa.Status.CurrentMetrics[idx].External = &autoscalingv2.ExternalMetricStatus{
		Metric:  spec.External.Metric,
		Current: next,
	}
	return nil
}

// parseRelativeValue parses a relative change like +20% or -10% and applies it
// to the current int32 value, returning the new value.
func parseRelativeValue(value string, current int32) (int32, error) {
	pct, err := parseRelativePercentage(value)
	if err != nil {
		return 0, err
	}
	factor := 1.0 + pct/100.0
	result := math.Round(float64(current) * factor)
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, fmt.Errorf("relative value %q produces a non-finite result", value)
	}
	if result < float64(math.MinInt32) || result > float64(math.MaxInt32) {
		return 0, fmt.Errorf("relative value %q produces a result outside the int32 range", value)
	}
	if result < 0 {
		result = 0
	}
	return int32(result), nil
}

// parseRelativeQuantity applies a relative percentage change to a resource.Quantity.
func parseRelativeQuantity(value string, current *resource.Quantity) (resource.Quantity, error) {
	if current == nil {
		return resource.Quantity{}, fmt.Errorf("cannot apply relative value %q to a nil quantity", value)
	}
	if current.Sign() < 0 {
		return resource.Quantity{}, fmt.Errorf("cannot apply relative value %q to a negative quantity", value)
	}
	maxMilliQuantity := resource.NewMilliQuantity(math.MaxInt64, resource.DecimalSI)
	if current.Cmp(*maxMilliQuantity) > 0 {
		return resource.Quantity{}, fmt.Errorf("current quantity is outside the supported int64 milli-unit range")
	}
	pct, err := parseRelativePercentage(value)
	if err != nil {
		return resource.Quantity{}, err
	}
	if pct == 0 {
		return current.DeepCopy(), nil
	}
	factor := 1.0 + pct/100.0
	result := math.Round(float64(current.MilliValue()) * factor)
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return resource.Quantity{}, fmt.Errorf("relative value %q produces a non-finite quantity", value)
	}
	// float64(math.MaxInt64) rounds to 1<<63, which is already outside the
	// positive int64 range. Treat that boundary conservatively as overflow.
	if result < float64(math.MinInt64) || result >= float64(math.MaxInt64) {
		return resource.Quantity{}, fmt.Errorf("relative value %q produces a quantity outside the int64 range", value)
	}
	newMilliValue := int64(result)
	if newMilliValue < 0 {
		newMilliValue = 0
	}
	return *resource.NewMilliQuantity(newMilliValue, current.Format), nil
}

func parseRelativePercentage(value string) (float64, error) {
	if len(value) < 2 || !strings.HasSuffix(value, "%") {
		return 0, fmt.Errorf("invalid relative value %q: expected format like +20%% or -10%%", value)
	}
	pctText := strings.TrimSuffix(value, "%")
	pct, err := strconv.ParseFloat(pctText, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid percentage %q: %w", pctText, err)
	}
	if math.IsNaN(pct) || math.IsInf(pct, 0) {
		return 0, fmt.Errorf("invalid percentage %q: value must be finite", pctText)
	}
	factor := 1.0 + pct/100.0
	if math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 0, fmt.Errorf("invalid percentage %q: relative factor must be finite", pctText)
	}
	return pct, nil
}

// computeProjectedReplicas returns ceil(currentReplicas * ratio) bounded by min/max.
func computeProjectedReplicas(currentReplicas int32, ratio float64, minReplicas, maxReplicas int32) int32 {
	projected, usable := tolerance.ProjectedReplicasForRatio(currentReplicas, ratio)
	if !usable {
		return currentReplicas
	}
	if projected < minReplicas {
		return minReplicas
	}
	if projected > maxReplicas {
		return maxReplicas
	}
	return projected
}
