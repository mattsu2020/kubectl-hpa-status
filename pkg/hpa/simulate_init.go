package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/simulate"
)

// This init injects the two hpa-root dependencies the simulate package cannot
// reach directly — the full analysis pipeline and the per-type metric handler
// registry — to break the import cycle. All other shared computation (metric
// identity, tolerance math) lives in pkg/hpa/internal/* and is called by
// simulate directly, so it needs no registration and cannot panic.
//
// This initialization runs whenever pkg/hpa is linked into the binary; a
// blank import is enough. If it has not run, simulate entry points return
// errors wrapping simulate.ErrDependencyMissing instead of panicking.
func init() {
	// Inject core analysis function
	simulate.SetAnalyzeFunc(func(hpa *autoscalingv2.HorizontalPodAutoscaler, _ bool, opts simulate.AnalysisOptions) simulate.Analysis {
		analysisOpts := AnalysisOptions{
			HealthWeights: convertSimulateHealthWeights(opts.HealthWeights),
		}
		result := AnalyzeWithOptions(hpa, true, analysisOpts)

		// Convert hpa.Analysis to simulate.Analysis
		return simulate.Analysis{
			Namespace:   result.Meta.Namespace,
			Name:        result.Meta.Name,
			Target:      result.Meta.Target,
			Current:     result.Replicas.Current,
			Desired:     result.Replicas.Desired,
			Min:         result.Replicas.Min,
			Max:         result.Replicas.Max,
			Health:      result.Decision.Health,
			HealthScore: result.Decision.HealthScore,
			Summary:     result.Decision.Summary,
			Metrics:     convertMetricsToSimulate(result.Metrics.Metrics),
			Conditions:  convertConditionsToSimulate(result.Conditions.Conditions),
		}
	})

	// Inject the metric impact-ratio helper backed by the handler registry.
	simulate.SetMetricImpactRatioFunc(metricImpactRatio)
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
