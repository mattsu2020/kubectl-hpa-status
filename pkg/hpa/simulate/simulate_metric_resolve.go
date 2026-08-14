package simulate

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// resolveMetricSpec finds one unambiguous spec metric matching the given name
// (case-insensitive). Ambiguous name-only references deliberately fail closed.
func resolveMetricSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (autoscalingv2.MetricSpec, bool) {
	spec, err := resolveMetricSpecUnique(hpa, name)
	if err != nil {
		return autoscalingv2.MetricSpec{}, false
	}
	return spec, true
}

func resolveMetricSpecUnique(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (autoscalingv2.MetricSpec, error) {
	index, err := resolveMetricSpecIndexUnique(hpa, name)
	if err != nil {
		return autoscalingv2.MetricSpec{}, err
	}
	return hpa.Spec.Metrics[index], nil
}

func resolveMetricSpecIndexUnique(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (int, error) {
	if hpa == nil {
		return -1, ErrNilHPA
	}
	matches := make([]int, 0, 1)
	for i, metric := range hpa.Spec.Metrics {
		if metricSpecNameMatches(metric, name) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return -1, ErrMetricNotFound
	case 1:
		return matches[0], nil
	default:
		return -1, fmt.Errorf("%w: %q matches %d metrics; use a unique metric name or remove duplicate selector/container variants", ErrMetricAmbiguous, name, len(matches))
	}
}

func findCurrentMetricForSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec) (int, error) {
	if hpa == nil {
		return -1, ErrNilHPA
	}
	specID, err := metricIDFromSpecInvoker(spec)
	if err != nil {
		return -1, err
	}
	match := -1
	for i, current := range hpa.Status.CurrentMetrics {
		currentID, currentErr := metricIDFromStatusInvoker(current)
		if currentErr != nil || currentID != specID {
			continue
		}
		if match >= 0 {
			return -1, fmt.Errorf("%w: current status contains duplicate identity %#v", ErrMetricAmbiguous, specID)
		}
		match = i
	}
	return match, nil
}

// findCurrentMetric returns the canonical current metric for one unambiguous
// spec name. It fails closed when multiple spec or status identities match.
func findCurrentMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (int, bool) {
	spec, err := resolveMetricSpecUnique(hpa, name)
	if err != nil {
		return -1, false
	}
	index, err := findCurrentMetricForSpec(hpa, spec)
	if err != nil || index < 0 {
		return -1, false
	}
	return index, true
}
