package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/simulate"
)

// init injects the hpa package functions into the simulate package to break import cycles.
// This initialization must happen before any simulate package functions are called.
func init() {
	// Inject core analysis function
	simulate.SetAnalyzeFunc(func(hpa *autoscalingv2.HorizontalPodAutoscaler, includeMetrics bool, opts simulate.AnalysisOptions) simulate.Analysis {
		analysisOpts := AnalysisOptions{
			HealthWeights: convertSimulateHealthWeights(opts.HealthWeights),
		}
		result := AnalyzeWithOptions(hpa, true, analysisOpts)

		// Convert hpa.Analysis to simulate.Analysis
		return simulate.Analysis{
			Namespace:   result.Namespace,
			Name:        result.Name,
			Target:      result.Target,
			Current:     result.Current,
			Desired:     result.Desired,
			Min:         result.Min,
			Max:         result.Max,
			Health:      result.Health,
			HealthScore: result.HealthScore,
			Summary:     result.Summary,
			Metrics:     convertMetricsToSimulate(result.Metrics),
			Conditions:  convertConditionsToSimulate(result.Conditions),
		}
	})

	// Inject metric identity functions
	simulate.SetMetricIDFromSpecFunc(func(spec autoscalingv2.MetricSpec) (simulate.MetricID, error) {
		id, err := MetricIDFromSpec(spec)
		return simulate.MetricID(id), err
	})

	simulate.SetMetricIDFromStatusFunc(func(status autoscalingv2.MetricStatus) (simulate.MetricID, error) {
		id, err := MetricIDFromStatus(status)
		return simulate.MetricID(id), err
	})

	// Inject metric handler functions
	simulate.SetCurrentMetricValueStatusFunc(func(metric autoscalingv2.MetricStatus) (autoscalingv2.MetricValueStatus, bool) {
		return currentMetricValueStatus(metric)
	})

	simulate.SetHasMetricValueForTargetFunc(func(v autoscalingv2.MetricValueStatus, targetType autoscalingv2.MetricTargetType) bool {
		return hasMetricValueForTarget(v, targetType)
	})

	simulate.SetMetricImpactRatioFunc(func(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
		return metricImpactRatio(hpa, metric)
	})

	simulate.SetMatchingMetricTargetFunc(func(hpa *autoscalingv2.HorizontalPodAutoscaler, current autoscalingv2.MetricStatus) (*autoscalingv2.MetricTarget, bool) {
		return matchingMetricTarget(hpa, current)
	})

	simulate.SetFormatMetricTargetFunc(func(target autoscalingv2.MetricTarget) string {
		return FormatMetricTarget(target)
	})

	// Inject tolerance functions
	simulate.SetDirectionalToleranceFunc(func(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) (float64, bool) {
		return directionalTolerance(hpa, ratio)
	})

	simulate.SetRatioWithinToleranceFunc(func(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) (bool, float64) {
		return ratioWithinTolerance(hpa, ratio)
	})

	simulate.SetToleranceDirectionFunc(func(ratio float64, scaleUp, scaleDown *float64) string {
		return toleranceDirection(ratio)
	})

	simulate.SetEffectiveDirectionalTolerancesFunc(func(hpa *autoscalingv2.HorizontalPodAutoscaler) (scaleUp, scaleDown float64) {
		return effectiveDirectionalTolerances(hpa)
	})

	// Inject formatting functions
	simulate.SetFormatMetricValueStatusFunc(func(v autoscalingv2.MetricValueStatus) string {
		return FormatMetricValueStatus(v)
	})

	simulate.SetRepeatCharFunc(func(count int, char string) string {
		return repeatChar(char, count)
	})

	simulate.SetFormatDurationFunc(func(seconds int32) string {
		return FormatDuration(int64(seconds))
	})

	simulate.SetEstimatedDesiredForRatioFunc(func(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) int32 {
		return estimatedDesiredForRatio(hpa, ratio)
	})
}

// convertSimulateHealthWeights converts simulate.HealthWeights to hpa.HealthWeights
func convertSimulateHealthWeights(w simulate.HealthWeights) HealthWeights {
	return HealthWeights{
		ScalingLimited:      intPtr(w.Limited),
		UnableToScale:       intPtr(w.NotReady),
		ScaleDownStabilized: intPtr(w.Falling),
	}
}

func intPtr(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// convertMetricsToSimulate converts hpa.Metric slice to simulate.Metric slice
func convertMetricsToSimulate(metrics []Metric) []simulate.Metric {
	result := make([]simulate.Metric, len(metrics))
	for i, m := range metrics {
		result[i] = simulate.Metric{
			Type:            m.Type,
			Name:            m.Name,
			Selector:        m.Selector,
			Current:         m.Current,
			Target:          m.Target,
			Note:            m.Note,
			Ratio:           m.Ratio,
		}
	}
	return result
}

// convertConditionsToSimulate converts hpa.Condition slice to simulate.Condition slice
func convertConditionsToSimulate(conditions []Condition) []simulate.Condition {
	result := make([]simulate.Condition, len(conditions))
	for i, c := range conditions {
		result[i] = simulate.Condition{
			Type:    c.Type,
			Status:  c.Status,
			Reason:  c.Reason,
			Message: c.Message,
		}
	}
	return result
}
