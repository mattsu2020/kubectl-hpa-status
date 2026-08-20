package hpa

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/confidence"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/tolerance"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// collectBase populates namespace, name, target, replicas, summary, and
// creation timestamp from the HPA source.
func collectBase(src *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) Analysis {
	summary, summaryKey := SummarizeDirectionWithKey(src, minReplicas)
	return *NewAnalysis(FlatAnalysis{
		Namespace:         src.Namespace,
		Name:              src.Name,
		Target:            fmt.Sprintf("%s/%s", src.Spec.ScaleTargetRef.Kind, src.Spec.ScaleTargetRef.Name),
		Current:           src.Status.CurrentReplicas,
		Desired:           src.Status.DesiredReplicas,
		Min:               minReplicas,
		Max:               src.Spec.MaxReplicas,
		Conditions:        make([]Condition, 0, len(src.Status.Conditions)),
		Metrics:           make([]Metric, 0, len(src.Status.CurrentMetrics)),
		Summary:           summary,
		SummaryKey:        summaryKey,
		CreationTimestamp: src.CreationTimestamp,
	})
}

// collectConditions populates the Conditions slice from HPA status conditions,
// sorted by priority.
func collectConditions(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	for _, condition := range prioritizedConditions(src.Status.Conditions) {
		a.SetConditions(append(a.Conditions(), Condition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		}))
	}
	return a
}

// collectMetrics populates the Metrics slice from HPA current metrics.
func collectMetrics(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	for _, metric := range src.Status.CurrentMetrics {
		a.SetMetrics(append(a.Metrics(), FormatMetricStatus(src, metric)))
	}
	return a
}

// collectBehavior populates the Behavior slice from HPA spec behavior rules.
func collectBehavior(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	a.SetBehavior(FormatBehavior(src))
	return a
}

// detectStaleStatus checks for observedGeneration lag and prefixes the summary.
func detectStaleStatus(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	if src.Status.ObservedGeneration != nil && *src.Status.ObservedGeneration < src.Generation {
		a.SetSummary("[STALE STATUS] " + a.Summary())
		a.SetStaleStatus(&StaleStatusInfo{
			ObservedGeneration: *src.Status.ObservedGeneration,
			CurrentGeneration:  src.Generation,
			Diff:               src.Generation - *src.Status.ObservedGeneration,
		})
	}
	return a
}

// detectImpactMetric estimates which metric has the largest scaling impact.
func detectImpactMetric(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	guess, ok := MostInfluentialMetric(src)
	if !ok {
		return a
	}
	// Limits, stabilization, and missing metric status can hide the controller's
	// actual winning recommendation.
	if winnerHiddenByControllerState(src) {
		guess.Confidence = string(ConfidenceLow)
		guess.Note += "; controller state may hide the actual winner"
	} else {
		guess.Confidence = string(ConfidenceMedium)
	}
	a.SetImpactMetric(&guess)
	return a
}

// detectScaleToZero checks minReplicas==0 and populates cold-start information.
func detectScaleToZero(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) Analysis {
	if minReplicas != 0 {
		return a
	}
	info := &ScaleToZeroInfo{Enabled: true}
	if src.Status.DesiredReplicas == 0 && src.Status.CurrentReplicas > 0 {
		info.ColdStart = true
		info.Note = "Cold start: scaling from 0 to 1 may experience additional delay; the first metric evaluation must complete before replicas are provisioned."
	} else if src.Status.DesiredReplicas == 0 && src.Status.CurrentReplicas == 0 {
		info.Note = "HPA is at zero replicas (scaled to zero). The next scale-up requires a cold start."
	}
	a.SetScaleToZero(info)
	return a
}

// detectStabilization estimates remaining seconds in the scale-down
// stabilization window and populates the source and confidence fields.
func detectStabilization(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	if remaining := estimateStabilizationRemaining(src); remaining != nil {
		a.SetStabilizationRemaining(remaining)
	}
	if window := scaleDownStabilizationWindow(src); window != nil {
		a.SetStabilizationWindowSeconds(window)
	}
	if a.StabilizationRemaining() != nil && *a.StabilizationRemaining() > 0 {
		a.SetStabilizationSource(detectStabilizationSource(src))
		a.SetStabilizationConfidence(stabilizationConfidenceLabel)
	}
	return a
}

// attachInterpretation populates interpretation, actions, suggestions, and
// structured outputs when interpretation is enabled.
func attachInterpretation(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, includeInterpretation bool) Analysis {
	if !includeInterpretation {
		return a
	}
	a.SetActions(RecommendedActions(src, minReplicas))
	a.SetSuggestions(BuildSuggestions(src, minReplicas))
	a.SetInterpretation(Interpret(src, minReplicas))
	a.SetStructuredInterpretation(buildStructuredInterpretation(src, minReplicas))
	a.SetStructuredActions(buildStructuredActions(src, minReplicas))
	return a
}

// attachHealth computes and attaches the typed health result.
func attachHealth(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, opts AnalysisOptions) Analysis {
	healthResult := HealthWithWeights(src, minReplicas, opts.HealthWeights)
	a.SetHealth(string(healthResult.State))
	a.SetHealthScore(healthResult.Score)
	a.SetHealthResult(&healthResult)
	a.dynamicHealthBaseline = newDynamicHealthBaseline(healthResult.Score, healthResult.State, healthResult.Signals)
	return a
}

// detectMetricDecisionTrace builds a comprehensive per-metric decision trace
// when multiple current metrics are present.
func detectMetricDecisionTrace(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) Analysis {
	if len(src.Status.CurrentMetrics) <= 1 {
		return a
	}
	a.SetMetricDecisionTrace(BuildMetricDecisionTrace(src, minReplicas))
	return a
}

// correlateStabilizationChurn adds an interpretation line when both
// stabilization is active AND churn is detected, warning that the
// stabilization window may be too short.
func correlateStabilizationChurn(a Analysis) Analysis {
	if a.StabilizationRemaining() == nil || *a.StabilizationRemaining() <= 0 {
		return a
	}
	if a.ChurnAnalysis() == nil {
		return a
	}
	if a.ChurnAnalysis().Level != churn.ChurnHigh && a.ChurnAnalysis().Level != churn.ChurnCritical {
		return a
	}
	line := confidence.BadgeEstimated + " Churn detected while stabilization window is active — consider increasing scaleDown.stabilizationWindowSeconds to reduce thrashing."
	for _, existing := range a.Interpretation() {
		if existing == line {
			return a
		}
	}
	a.SetInterpretation(append(a.Interpretation(), line))
	return a
}

// FinalizeAnalysis applies post-enrichment derivations that depend on fields
// populated after the initial AnalyzeWithOptions pass. ChurnAnalysis, for
// example, is built from Events in the cmd layer, so the stabilization/churn
// correlation cannot run inside AnalyzeWithOptions (where ChurnAnalysis is
// still nil). Call this once all enrichment is complete.
func FinalizeAnalysis(a Analysis) Analysis {
	a = collectAssumptions(a)
	return correlateStabilizationChurn(a)
}

// collectAssumptions records the inferred values and estimates the analysis
// relies on, with their source and confidence. This makes the tool's built-in
// assumptions visible in structured output so operators (and future
// KEP-based controller config reads) can distinguish measured vs assumed.
func collectAssumptions(a Analysis) Analysis {
	// Finalization may be invoked by more than one workflow layer. Rebuild the
	// assumptions owned by this phase instead of appending duplicates.
	assumptions := make([]Assumption, 0, len(a.Assumptions())+2)
	for _, assumption := range a.Assumptions() {
		if assumption.Name != "tolerance" && assumption.Name != "stabilizationRemaining" {
			assumptions = append(assumptions, assumption)
		}
	}
	assumptions = append(assumptions, Assumption{
		Name:       "tolerance",
		Value:      fmt.Sprintf("%g", tolerance.DefaultTolerance),
		Source:     "assumed-controller-default",
		Confidence: "medium",
	})
	if a.StabilizationRemaining() != nil && *a.StabilizationRemaining() > 0 {
		assumptions = append(assumptions, Assumption{
			Name:       "stabilizationRemaining",
			Value:      fmt.Sprintf("%ds", *a.StabilizationRemaining()),
			Source:     "lastScaleTimeApproximation",
			Confidence: "low",
		})
	}
	a.SetAssumptions(assumptions)
	return a
}

// attachDebug adds verbose debug lines when enabled.
func attachDebug(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler, opts AnalysisOptions) Analysis {
	if !opts.Debug {
		return a
	}
	a.SetDebug(DebugLines(src, a))
	return a
}

// attachDecisionSignals populates DecisionSignals using the versioned adapter.
// When KEP-6111 structured output is available, it uses FromStructuredOutput.
// Otherwise, it falls back to the rich estimation pipeline.
func attachDecisionSignals(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler, adapter DecisionAdapter) Analysis {
	if src == nil {
		return a
	}

	// Use the new DecisionSignals interface if the adapter implements it.
	signalsAdapter, ok := adapter.(DecisionSignals)
	if ok {
		signals := signalsAdapter.FromStructuredOutput(src)
		if signals == nil {
			signals = signalsAdapter.FromEstimation(src)
		}
		if len(signals) > 0 {
			a.SetDecisionSignals(signals)
		}
		return a
	}

	// Fallback to legacy adapter interface.
	signals := adapter.FromStructuredOutput(src)
	if signals == nil {
		signals = adapter.FromEstimation(src)
	}
	if len(signals) > 0 {
		a.SetDecisionSignals(signals)
	}
	return a
}
