package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/simulate"
)

// init injects the hpa package functions into the simulate package to break import cycles.
// This initialization must happen before any simulate package functions are called.
func init() {
	// Inject core analysis function
	simulate.SetAnalyzeFunc(func(hpa *autoscalingv2.HorizontalPodAutoscaler, _ bool, opts simulate.AnalysisOptions) simulate.Analysis {
		analysisOpts := AnalysisOptions{
			HealthWeights: convertSimulateHealthWeights(opts.HealthWeights),
		}
		result := AnalyzeWithOptions(hpa, true, analysisOpts)

		// Convert hpa.Analysis to simulate.Analysis
		return simulate.Analysis{
			Namespace:   result.Namespace(),
			Name:        result.Name(),
			Target:      result.Target(),
			Current:     result.Current(),
			Desired:     result.Desired(),
			Min:         result.Min(),
			Max:         result.Max(),
			Health:      result.Health(),
			HealthScore: result.HealthScore(),
			Summary:     result.Summary(),
			Metrics:     convertMetricsToSimulate(result.Metrics()),
			Conditions:  convertConditionsToSimulate(result.Conditions()),
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
	simulate.SetCurrentMetricValueStatusFunc(currentMetricValueStatus)

	simulate.SetHasMetricValueForTargetFunc(hasMetricValueForTarget)

	simulate.SetMetricImpactRatioFunc(metricImpactRatio)

	simulate.SetMatchingMetricTargetFunc(matchingMetricTarget)

	// Inject tolerance functions
	simulate.SetDirectionalToleranceFunc(directionalTolerance)

	simulate.SetRatioWithinToleranceFunc(ratioWithinTolerance)

	simulate.SetToleranceDirectionFunc(func(ratio float64, _, _ *float64) string {
		return toleranceDirection(ratio)
	})

	simulate.SetEffectiveDirectionalTolerancesFunc(effectiveDirectionalTolerances)

	// Inject formatting functions
	simulate.SetEstimatedDesiredForRatioFunc(estimatedDesiredForRatio)
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
			Type:     m.Type,
			Name:     m.Name,
			Selector: m.Selector,
			Current:  m.Current,
			Target:   m.Target,
			Note:     m.Note,
			Ratio:    m.Ratio,
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
