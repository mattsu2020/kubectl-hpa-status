// Package hpa provides HPA analysis, health scoring, metric formatting,
// and diagnostic interpretation for HorizontalPodAutoscaler resources.
package hpa

import (
	"fmt"
	"sort"
	"strings"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const limitation = "[confidence: high] This plugin uses existing HPA status, conditions, metrics, and events. It does not expose internal controller calculations."

const (
	healthScoreMax                   = 100
	healthPenaltyScalingInactive     = 45
	healthPenaltyUnableToScale       = 35
	healthPenaltyScalingLimited      = 25
	healthPenaltyImplicitMaxReplicas = 20
	healthPenaltyScaleDownStabilized = 10
	healthPenaltyAtMinimumReplicas   = 5
)

// AnalysisOptions configures the analysis behavior.
type AnalysisOptions struct {
	HealthWeights HealthWeights `json:"healthWeights,omitempty" yaml:"healthWeights,omitempty"`
	Debug         bool          `json:"debug,omitempty" yaml:"debug,omitempty"`
}

// HealthWeights holds configurable penalty values for health score computation.
type HealthWeights struct {
	ScalingInactive     int `json:"scalingInactive,omitempty" yaml:"scalingInactive,omitempty"`
	UnableToScale       int `json:"unableToScale,omitempty" yaml:"unableToScale,omitempty"`
	ScalingLimited      int `json:"scalingLimited,omitempty" yaml:"scalingLimited,omitempty"`
	ImplicitMaxReplicas int `json:"implicitMaxReplicas,omitempty" yaml:"implicitMaxReplicas,omitempty"`
	ScaleDownStabilized int `json:"scaleDownStabilized,omitempty" yaml:"scaleDownStabilized,omitempty"`
	AtMinimumReplicas   int `json:"atMinimumReplicas,omitempty" yaml:"atMinimumReplicas,omitempty"`
}

// Analysis holds the complete analysis result for a single HPA.
type Analysis struct {
	Namespace                string              `json:"namespace" yaml:"namespace"`
	Name                     string              `json:"name" yaml:"name"`
	Target                   string              `json:"target" yaml:"target"`
	Current                  int32               `json:"currentReplicas" yaml:"currentReplicas"`
	Desired                  int32               `json:"desiredReplicas" yaml:"desiredReplicas"`
	Min                      int32               `json:"minReplicas" yaml:"minReplicas"`
	Max                      int32               `json:"maxReplicas" yaml:"maxReplicas"`
	Health                   string              `json:"health" yaml:"health"`
	HealthScore              int                 `json:"healthScore" yaml:"healthScore"`
	Summary                  string              `json:"summary" yaml:"summary"`
	Conditions               []Condition         `json:"conditions" yaml:"conditions"`
	Metrics                  []Metric            `json:"metrics" yaml:"metrics"`
	Behavior                 []BehaviorRule      `json:"behavior,omitempty" yaml:"behavior,omitempty"`
	Actions                  []string            `json:"recommendedActions,omitempty" yaml:"recommendedActions,omitempty"`
	Suggestions              []Suggestion        `json:"suggestions,omitempty" yaml:"suggestions,omitempty"`
	Interpretation           []string            `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	KEDAInfo                 *KEDAAnalysis       `json:"keda,omitempty" yaml:"keda,omitempty"`
	VPAConflict              *VPAConflictInfo    `json:"vpaConflict,omitempty" yaml:"vpaConflict,omitempty"`
	TargetReplicas           *TargetReplicaInfo  `json:"targetReplicas,omitempty" yaml:"targetReplicas,omitempty"`
	Debug                    []string            `json:"debug,omitempty" yaml:"debug,omitempty"`
	ImpactMetric             *MetricImpactGuess  `json:"impactMetric,omitempty" yaml:"impactMetric,omitempty"`
	CreationTimestamp        metav1.Time         `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"`
	StaleStatus              *StaleStatusInfo    `json:"staleStatus,omitempty" yaml:"staleStatus,omitempty"`
	StabilizationRemaining   *int64              `json:"stabilizationRemaining,omitempty" yaml:"stabilizationRemaining,omitempty"`
	ScaleToZero              *ScaleToZeroInfo    `json:"scaleToZero,omitempty" yaml:"scaleToZero,omitempty"`
	StructuredInterpretation []StructuredMessage `json:"structuredInterpretation,omitempty" yaml:"structuredInterpretation,omitempty"`
	StructuredActions        []StructuredMessage `json:"structuredActions,omitempty" yaml:"structuredActions,omitempty"`
	DecisionSignals          []DecisionSignal    `json:"decisionSignals,omitempty" yaml:"decisionSignals,omitempty"`
}

// DecisionSignal is the stable internal shape for explicit controller scaling
// decision data. Current Kubernetes HPA status does not expose these fields;
// future structured status adapters should populate this slice and renderers
// should prefer it over best-effort inference when present.
type DecisionSignal struct {
	Reason     string `json:"reason" yaml:"reason"`
	Message    string `json:"message,omitempty" yaml:"message,omitempty"`
	MetricName string `json:"metricName,omitempty" yaml:"metricName,omitempty"`
	Source     string `json:"source,omitempty" yaml:"source,omitempty"`
	Confidence string `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}

// StructuredMessage provides a machine-readable representation of an
// interpretation or action line, with a reason, human message, and
// suggested next step.
type StructuredMessage struct {
	Reason   string `json:"reason" yaml:"reason"`
	Message  string `json:"message" yaml:"message"`
	NextStep string `json:"nextStep,omitempty" yaml:"nextStep,omitempty"`
	Severity string `json:"severity,omitempty" yaml:"severity,omitempty"` // "warning", "error", "info"
}

// Condition represents an HPA condition with type, status, reason, and message.
type Condition struct {
	Type    string `json:"type" yaml:"type"`
	Status  string `json:"status" yaml:"status"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// Metric holds formatted metric data including current, target, ratio, and display text.
type Metric struct {
	Type    string   `json:"type" yaml:"type"`
	Name    string   `json:"name,omitempty" yaml:"name,omitempty"`
	Current string   `json:"current,omitempty" yaml:"current,omitempty"`
	Target  string   `json:"target,omitempty" yaml:"target,omitempty"`
	Ratio   *float64 `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	Note    string   `json:"note,omitempty" yaml:"note,omitempty"`
	Text    string   `json:"text" yaml:"text"`
}

// MetricImpactGuess estimates which resource metric has the most impact on scaling.
type MetricImpactGuess struct {
	Name       string  `json:"name" yaml:"name"`
	Ratio      float64 `json:"ratio" yaml:"ratio"`
	Note       string  `json:"note" yaml:"note"`
	Confidence string  `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}

// StaleStatusInfo holds details about observedGeneration lag.
type StaleStatusInfo struct {
	ObservedGeneration int64 `json:"observedGeneration" yaml:"observedGeneration"`
	CurrentGeneration  int64 `json:"currentGeneration" yaml:"currentGeneration"`
	Diff               int64 `json:"diff" yaml:"diff"`
}

// ScaleToZeroInfo holds scale-to-zero related information.
type ScaleToZeroInfo struct {
	Enabled   bool   `json:"enabled" yaml:"enabled"`
	ColdStart bool   `json:"coldStart,omitempty" yaml:"coldStart,omitempty"`
	Note      string `json:"note,omitempty" yaml:"note,omitempty"`
}

// BehaviorRule describes a scale-up or scale-down behavior policy.
type BehaviorRule struct {
	Direction                  string   `json:"direction" yaml:"direction"`
	StabilizationWindowSeconds *int32   `json:"stabilizationWindowSeconds,omitempty" yaml:"stabilizationWindowSeconds,omitempty"`
	SelectPolicy               string   `json:"selectPolicy,omitempty" yaml:"selectPolicy,omitempty"`
	Policies                   []string `json:"policies,omitempty" yaml:"policies,omitempty"`
	Text                       string   `json:"text" yaml:"text"`
}

// Suggestion holds a recommended HPA patch with safety metadata.
type Suggestion struct {
	Title         string   `json:"title" yaml:"title"`
	Description   string   `json:"description" yaml:"description"`
	Command       string   `json:"command,omitempty" yaml:"command,omitempty"`
	Patch         string   `json:"patch,omitempty" yaml:"patch,omitempty"`
	Risk          string   `json:"risk,omitempty" yaml:"risk,omitempty"`
	Preconditions []string `json:"preconditions,omitempty" yaml:"preconditions,omitempty"`
	Warnings      []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	Apply         bool     `json:"apply,omitempty" yaml:"apply,omitempty"`
}

// KEDAAnalysis holds KEDA-specific information attached to an HPA Analysis.
// Populated only when --keda is enabled and the HPA is KEDA-managed.
type KEDAAnalysis struct {
	ScaledObjectName string               `json:"scaledObjectName" yaml:"scaledObjectName"`
	Triggers         []KEDATriggerSummary `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	PollingInterval  *int32               `json:"pollingInterval,omitempty" yaml:"pollingInterval,omitempty"`
	CooldownPeriod   *int32               `json:"cooldownPeriod,omitempty" yaml:"cooldownPeriod,omitempty"`
	MinReplicaCount  *int32               `json:"minReplicaCount,omitempty" yaml:"minReplicaCount,omitempty"`
	MaxReplicaCount  *int32               `json:"maxReplicaCount,omitempty" yaml:"maxReplicaCount,omitempty"`
	Lines            []string             `json:"lines,omitempty" yaml:"lines,omitempty"`
}

// KEDATriggerSummary is a display-oriented summary of a KEDA trigger.
type KEDATriggerSummary struct {
	Type string `json:"type" yaml:"type"`
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

// TargetReplicaInfo holds replica status from the scale target resource.
// When not-ready pods exist, HPA scaling calculations may be affected.
type TargetReplicaInfo struct {
	TotalReplicas int32 `json:"totalReplicas" yaml:"totalReplicas"`
	ReadyReplicas int32 `json:"readyReplicas" yaml:"readyReplicas"`
	NotReady      int32 `json:"notReady" yaml:"notReady"`
}

// Analyze produces an Analysis for the given HPA using default options.
func Analyze(src *autoscalingv2.HorizontalPodAutoscaler, includeInterpretation bool) Analysis {
	return AnalyzeWithOptions(src, includeInterpretation, AnalysisOptions{})
}

// AnalyzeWithOptions produces an Analysis with custom health weights and debug settings.
func AnalyzeWithOptions(src *autoscalingv2.HorizontalPodAutoscaler, includeInterpretation bool, opts AnalysisOptions) Analysis {
	if src == nil {
		return Analysis{
			Health:      "ERROR",
			HealthScore: 0,
			Summary:     "HPA data is unavailable.",
			Interpretation: []string{
				"[confidence: high] HPA input was nil; no Kubernetes status can be analyzed.",
			},
		}
	}

	minReplicas := int32(1)
	if src.Spec.MinReplicas != nil {
		minReplicas = *src.Spec.MinReplicas
	}

	analysis := Analysis{
		Namespace:         src.Namespace,
		Name:              src.Name,
		Target:            fmt.Sprintf("%s/%s", src.Spec.ScaleTargetRef.Kind, src.Spec.ScaleTargetRef.Name),
		Current:           src.Status.CurrentReplicas,
		Desired:           src.Status.DesiredReplicas,
		Min:               minReplicas,
		Max:               src.Spec.MaxReplicas,
		Summary:           SummarizeDirection(src, minReplicas),
		CreationTimestamp: src.CreationTimestamp,
	}

	for _, condition := range prioritizedConditions(src.Status.Conditions) {
		analysis.Conditions = append(analysis.Conditions, Condition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}

	for _, metric := range src.Status.CurrentMetrics {
		analysis.Metrics = append(analysis.Metrics, FormatMetricStatus(src, metric))
	}

	analysis.Behavior = FormatBehavior(src)

	// Prefix summary with [STALE STATUS] when the controller has not yet observed the latest spec.
	if src.Status.ObservedGeneration != nil && *src.Status.ObservedGeneration < src.Generation {
		analysis.Summary = "[STALE STATUS] " + analysis.Summary
		analysis.StaleStatus = &StaleStatusInfo{
			ObservedGeneration: *src.Status.ObservedGeneration,
			CurrentGeneration:  src.Generation,
			Diff:               src.Generation - *src.Status.ObservedGeneration,
		}
	}

	if guess, ok := MostInfluentialMetric(src); ok {
		// When desiredReplicas == maxReplicas, the winner metric cannot be reliably determined
		if src.Status.DesiredReplicas == src.Spec.MaxReplicas {
			guess.Confidence = "low"
			guess.Note = "desiredReplicas == maxReplicas so the winner metric cannot be reliably determined"
		} else {
			guess.Confidence = "medium"
		}
		analysis.ImpactMetric = &guess
	}

	// Scale-to-zero detection
	if minReplicas == 0 {
		info := &ScaleToZeroInfo{Enabled: true}
		if src.Status.DesiredReplicas == 0 && src.Status.CurrentReplicas > 0 {
			info.ColdStart = true
			info.Note = "Cold start: scaling from 0 to 1 may experience additional delay; the first metric evaluation must complete before replicas are provisioned."
		} else if src.Status.DesiredReplicas == 0 && src.Status.CurrentReplicas == 0 {
			info.Note = "HPA is at zero replicas (scaled to zero). The next scale-up requires a cold start."
		}
		analysis.ScaleToZero = info
	}

	// Stabilization remaining time estimation
	if remaining := estimateStabilizationRemaining(src); remaining != nil {
		analysis.StabilizationRemaining = remaining
	}

	if includeInterpretation {
		analysis.Actions = RecommendedActions(src, minReplicas)
		analysis.Suggestions = BuildSuggestions(src, minReplicas)
		analysis.Interpretation = Interpret(src, minReplicas)
		analysis.Interpretation = append(analysis.Interpretation, KEDADiagnostics(src)...)
		analysis.StructuredInterpretation = buildStructuredInterpretation(src, minReplicas)
		analysis.StructuredActions = buildStructuredActions(src, minReplicas)
	}
	analysis.Health, analysis.HealthScore = HealthWithWeights(src, minReplicas, opts.HealthWeights)
	if opts.Debug {
		analysis.Debug = DebugLines(src, analysis)
	}

	return analysis
}

// Health computes the health state and score using default penalty weights.
func Health(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) (string, int) {
	return HealthWithWeights(hpa, minReplicas, HealthWeights{})
}

// HealthWithWeights computes the health state and score using configurable penalty weights.
func HealthWithWeights(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, weights HealthWeights) (string, int) {
	if hpa == nil {
		return "ERROR", 0
	}
	weights = defaultHealthWeights(weights)

	score := healthScoreMax
	health := "OK"
	for _, condition := range hpa.Status.Conditions {
		switch {
		case condition.Type == "ScalingActive" && condition.Status != corev1.ConditionTrue:
			score -= weights.ScalingInactive
			health = "ERROR"
		case condition.Type == "AbleToScale" && condition.Status != corev1.ConditionTrue:
			score -= weights.UnableToScale
			health = "ERROR"
		case condition.Type == "ScalingLimited" && condition.Status == corev1.ConditionTrue:
			score -= weights.ScalingLimited
			if health != "ERROR" {
				health = "LIMITED"
			}
		case condition.Type == "AbleToScale" && condition.Reason == "ScaleDownStabilized":
			score -= weights.ScaleDownStabilized
			if health == "OK" {
				health = "STABILIZED"
			}
		}
	}
	if hpa.Status.CurrentReplicas == hpa.Status.DesiredReplicas && hpa.Status.CurrentReplicas == hpa.Spec.MaxReplicas {
		score -= weights.ImplicitMaxReplicas
		if health == "OK" {
			health = "LIMITED"
		}
	}
	if hpa.Status.DesiredReplicas == minReplicas {
		score -= weights.AtMinimumReplicas
	}
	if score < 0 {
		score = 0
	}
	return health, score
}

func defaultHealthWeights(weights HealthWeights) HealthWeights {
	if weights.ScalingInactive == 0 {
		weights.ScalingInactive = healthPenaltyScalingInactive
	}
	if weights.UnableToScale == 0 {
		weights.UnableToScale = healthPenaltyUnableToScale
	}
	if weights.ScalingLimited == 0 {
		weights.ScalingLimited = healthPenaltyScalingLimited
	}
	if weights.ImplicitMaxReplicas == 0 {
		weights.ImplicitMaxReplicas = healthPenaltyImplicitMaxReplicas
	}
	if weights.ScaleDownStabilized == 0 {
		weights.ScaleDownStabilized = healthPenaltyScaleDownStabilized
	}
	if weights.AtMinimumReplicas == 0 {
		weights.AtMinimumReplicas = healthPenaltyAtMinimumReplicas
	}
	return weights
}

// SummarizeDirection returns a one-line summary of the HPA scaling direction.
func SummarizeDirection(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) string {
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		return "HPA cannot currently compute a scaling recommendation from metrics."
	}
	if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas > 0 {
		if minReplicas == 0 {
			return "HPA wants to scale to zero (cold start will occur on next scale-up)."
		}
		return "HPA has no visible desired replica recommendation in status."
	}
	if minReplicas == 0 && hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas == 0 {
		return "HPA is scaled to zero (minReplicas=0); awaiting trigger to scale up."
	}

	current := hpa.Status.CurrentReplicas
	desired := hpa.Status.DesiredReplicas

	switch {
	case desired > current:
		return "HPA currently wants to scale up."
	case desired < current:
		return "HPA currently wants to scale down."
	case desired == hpa.Spec.MaxReplicas:
		return "HPA is at maxReplicas."
	case desired == minReplicas && minReplicas == 0:
		return "HPA is at minReplicas (scale-to-zero enabled)."
	case desired == minReplicas:
		return "HPA is at minReplicas."
	default:
		return "HPA currently keeps the replica count unchanged."
	}
}

// Interpret generates detailed interpretation lines with confidence labels.
func Interpret(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []string {
	var lines []string

	if hpa.Status.ObservedGeneration != nil && *hpa.Status.ObservedGeneration < hpa.Generation {
		lines = append(lines, fmt.Sprintf("[confidence: high] Warning: status.observedGeneration=%d is behind metadata.generation=%d; the status may not reflect the latest spec.", *hpa.Status.ObservedGeneration, hpa.Generation))
	}

	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		lines = append(lines,
			fmt.Sprintf("[confidence: high] ScalingActive is %s: %s - %s", condition.Status, condition.Reason, condition.Message),
			"[confidence: high] The HPA is not reporting a reliable scale direction while metric evaluation is inactive.",
			"[confidence: high] This plugin avoids treating desiredReplicas=0 as a scale-down recommendation in this state.",
			limitation,
		)
		lines = append(lines, ExternalMetricDiagnostics(hpa)...)
		return lines
	}

	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Status != corev1.ConditionTrue {
		lines = append(lines,
			fmt.Sprintf("[confidence: high] AbleToScale is %s: %s - %s", condition.Status, condition.Reason, condition.Message))
	} else if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		if remaining := estimateStabilizationRemaining(hpa); remaining != nil && *remaining > 0 {
			lines = append(lines,
				fmt.Sprintf("[confidence: high] Scale down appears stabilized: %s (approximately %d seconds remaining before scale-down is allowed).", condition.Message, *remaining))
		} else {
			lines = append(lines,
				fmt.Sprintf("[confidence: medium] Scale down appears stabilized: %s", condition.Message))
		}
	}

	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			lines = append(lines, "[confidence: high] ScalingLimited reports that the visible desired replica count is constrained by maxReplicas.")
		case minReplicas:
			lines = append(lines, "[confidence: high] ScalingLimited reports that the visible desired replica count is constrained by minReplicas.")
		default:
			lines = append(lines, "[confidence: high] The recommendation is reported as limited.")
		}
	}

	if hpa.Status.DesiredReplicas > hpa.Status.CurrentReplicas {
		lines = append(lines, "[confidence: high] desiredReplicas is greater than currentReplicas, so the HPA is recommending scale up.")
	} else if hpa.Status.DesiredReplicas < hpa.Status.CurrentReplicas {
		lines = append(lines, "[confidence: high] desiredReplicas is less than currentReplicas, so the HPA is recommending scale down.")
	} else {
		lines = append(lines, "[confidence: high] desiredReplicas equals currentReplicas, so no immediate replica change is visible from status.")
		if hpa.Status.DesiredReplicas != hpa.Spec.MaxReplicas && hpa.Status.DesiredReplicas != minReplicas {
			if metric, ok := MetricOutsideTarget(hpa); ok {
				deviation := metric.Ratio - 1.0
				if deviation < 0 {
					deviation = -deviation
				}
				if deviation < 0.1 {
					lines = append(lines, fmt.Sprintf("[tolerance-confirmed] [confidence: high] %s metric ratio is %.3f (within ±10%% of target); the Kubernetes default tolerance band of 0.1 (10%%) explains why replicas are unchanged despite %s being %.1f%% %s target.", metric.Name, metric.Ratio, metric.Name, (metric.Ratio-1)*100, metric.Note))
				} else {
					lines = append(lines, fmt.Sprintf("[confidence: medium] %s metric ratio is approximately %.3f, which is close to the target.", metric.Name, metric.Ratio))
					lines = append(lines, "[confidence: medium] This is consistent with tolerance-based no-scale. Kubernetes commonly uses a tolerance band around the target, but HPA status does not expose tolerance as an explicit reason.")
				}
				lines = append(lines, "[confidence: high] The plugin avoids claiming the exact internal reason because rounding, stabilization, or conservative metric handling may also affect the final result.")
			}
		}
	}

	if hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas && len(hpa.Status.CurrentMetrics) > 1 {
		lines = append(lines, "[confidence: high] desiredReplicas == maxReplicas; the winning metric cannot be reliably determined because the replica cap may hide the true metric winner.")
	} else if guess, ok := MostInfluentialMetric(hpa); ok && len(hpa.Status.CurrentMetrics) > 1 {
		lines = append(lines, fmt.Sprintf("[confidence: medium] Among visible resource utilization metrics, %s has the largest distance from target (ratio %.3f).", guess.Name, guess.Ratio))
		lines = append(lines, "[confidence: high] This is only an impact estimate; the API does not expose per-metric replica recommendations or the final metric winner.")
	} else if len(hpa.Status.CurrentMetrics) > 1 {
		lines = append(lines, "[confidence: high] Multiple current metrics are reported, but the API does not expose per-metric replica recommendations or which metric would have selected the recommendation before replica limits were applied.")
		lines = append(lines, "[confidence: high] Events and human-readable messages can hint at the contributing metric, but they are not a stable decision record.")
	}

	// Scale-to-zero interpretation
	if minReplicas == 0 {
		if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas == 0 {
			lines = append(lines, "[confidence: high] Scale-to-zero is enabled (minReplicas=0) and the workload is currently at zero replicas. The next scale-up requires a cold start which may introduce additional latency.")
		} else if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas > 0 {
			lines = append(lines, "[confidence: high] Scale-to-zero is enabled (minReplicas=0) and the HPA wants to scale to zero. Note: scaling from 0 back to 1 requires a cold start.")
		}
	}

	lines = append(lines, ExternalMetricDiagnostics(hpa)...)
	lines = append(lines, ObjectMetricDiagnostics(hpa)...)
	lines = append(lines, limitation)

	return lines
}

// ExternalMetricDiagnostics generates diagnostic lines for external metric issues.
func ExternalMetricDiagnostics(hpa *autoscalingv2.HorizontalPodAutoscaler) []string {
	var lines []string
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ExternalMetricSourceType || spec.External == nil {
			continue
		}
		if !hasCurrentExternalMetric(hpa, spec.External.Metric.Name) {
			lines = append(lines, fmt.Sprintf("[confidence: high] External metric %q is configured but no matching current metric status is reported; check the external metrics adapter, selector, and metric freshness.", spec.External.Metric.Name))
			continue
		}
		if metric, ok := currentExternalMetric(hpa, spec.External.Metric.Name); ok {
			formatted := FormatMetricStatus(hpa, metric)
			if formatted.Ratio != nil {
				lines = append(lines, fmt.Sprintf("[confidence: medium] External metric %q is %.3fx its target; stale or delayed adapter data can make HPA decisions lag behind workload demand.", spec.External.Metric.Name, *formatted.Ratio))
			}
		}
	}
	return lines
}

// ObjectMetricDiagnostics generates diagnostic lines for object metric issues.
func ObjectMetricDiagnostics(hpa *autoscalingv2.HorizontalPodAutoscaler) []string {
	var lines []string
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ObjectMetricSourceType || spec.Object == nil {
			continue
		}
		if metric, ok := currentObjectMetric(hpa, spec.Object.Metric.Name); ok {
			formatted := FormatMetricStatus(hpa, metric)
			object := fmt.Sprintf("%s/%s", spec.Object.DescribedObject.Kind, spec.Object.DescribedObject.Name)
			if formatted.Ratio != nil {
				lines = append(lines, fmt.Sprintf("[confidence: medium] Object metric %q on %s is %.3fx its target; compare this object-level load with per-pod load before changing replica limits.", spec.Object.Metric.Name, object, *formatted.Ratio))
			}
		} else {
			lines = append(lines, fmt.Sprintf("[confidence: high] Object metric %q is configured but no matching current metric status is reported; verify the described object and metric adapter output.", spec.Object.Metric.Name))
		}
	}
	return lines
}

// KEDADiagnostics generates diagnostic lines when the HPA appears KEDA-managed.
func KEDADiagnostics(hpa *autoscalingv2.HorizontalPodAutoscaler) []string {
	if !looksLikeKEDAManaged(hpa) {
		return nil
	}
	lines := []string{
		"[confidence: medium] This HPA appears to be managed by KEDA. HPA status explains the final autoscaling object, but KEDA ScaledObject, TriggerAuthentication, and scaler errors may explain missing external metrics.",
	}
	if len(hpa.Spec.Metrics) == 0 {
		lines = append(lines, "[confidence: high] KEDA-style HPA has no visible spec.metrics; check whether KEDA has reconciled the ScaledObject successfully.")
	}
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ExternalMetricSourceType && spec.External != nil {
			lines = append(lines, fmt.Sprintf("[confidence: medium] For KEDA external metric %q, inspect the ScaledObject status.conditions and keda-operator logs if HPA currentMetrics is missing or stale.", spec.External.Metric.Name))
		}
	}
	return lines
}

func looksLikeKEDAManaged(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for key, value := range hpa.Labels {
		if strings.Contains(strings.ToLower(key), "keda.sh") || strings.Contains(strings.ToLower(value), "keda") {
			return true
		}
	}
	for key, value := range hpa.Annotations {
		if strings.Contains(strings.ToLower(key), "keda.sh") || strings.Contains(strings.ToLower(value), "keda") {
			return true
		}
	}
	return strings.HasPrefix(hpa.Name, "keda-hpa-")
}

// FindCondition returns the HPA condition matching the given type, or nil.
func FindCondition(hpa *autoscalingv2.HorizontalPodAutoscaler, conditionType string) *autoscalingv2.HorizontalPodAutoscalerCondition {
	for i := range hpa.Status.Conditions {
		if string(hpa.Status.Conditions[i].Type) == conditionType {
			return &hpa.Status.Conditions[i]
		}
	}
	return nil
}

func calculateRatioAndNote(currentVal autoscalingv2.MetricValueStatus, targetVal autoscalingv2.MetricTarget, targetStr string) (*float64, string) {
	var ratio *float64
	var note string

	if currentVal.AverageUtilization != nil {
		ratio = utilizationRatio(currentVal.AverageUtilization, targetStr)
		note = CompareMetricToTarget(currentVal.AverageUtilization, targetStr)
	} else if currentVal.AverageValue != nil && targetVal.AverageValue != nil {
		ratio = quantityRatio(currentVal.AverageValue, targetVal.AverageValue)
		note = CompareQuantityToTarget(currentVal.AverageValue, targetVal.AverageValue)
	} else if currentVal.Value != nil && targetVal.Value != nil {
		ratio = quantityRatio(currentVal.Value, targetVal.Value)
		note = CompareQuantityToTarget(currentVal.Value, targetVal.Value)
	}
	return ratio, note
}

// FormatMetricStatus formats a metric status entry into a Metric struct.
func FormatMetricStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	switch metric.Type {
	case "":
		return Metric{Text: "Metric status is present, but details are unavailable"}
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource == nil {
			return Metric{Type: "Resource", Text: "Resource metric: <missing status>"}
		}
		targetSpec := FindResourceTargetSpec(hpa, string(metric.Resource.Name))
		target := FormatMetricTarget(targetSpec)
		current := FormatMetricValue(metric.Resource.Current.AverageUtilization, metric.Resource.Current.AverageValue)
		ratio, note := calculateRatioAndNote(metric.Resource.Current, targetSpec, target)
		text := fmt.Sprintf("Resource %s current=%s target=%s", metric.Resource.Name, current, target)
		if ratio != nil {
			text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
		}
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "Resource",
			Name:    string(metric.Resource.Name),
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if metric.ContainerResource == nil {
			return Metric{Type: "ContainerResource", Text: "ContainerResource metric: <missing status>"}
		}
		targetSpec := FindContainerResourceTargetSpec(hpa, string(metric.ContainerResource.Name), metric.ContainerResource.Container)
		target := FormatMetricTarget(targetSpec)
		current := FormatMetricValueStatus(metric.ContainerResource.Current)
		ratio, note := calculateRatioAndNote(metric.ContainerResource.Current, targetSpec, target)
		text := fmt.Sprintf("ContainerResource %s/%s current=%s target=%s", metric.ContainerResource.Container, metric.ContainerResource.Name, current, target)
		if ratio != nil {
			text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
		}
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "ContainerResource",
			Name:    fmt.Sprintf("%s/%s", metric.ContainerResource.Container, metric.ContainerResource.Name),
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	case autoscalingv2.PodsMetricSourceType:
		if metric.Pods == nil {
			return Metric{Type: "Pods", Text: "Pods metric: <missing status>"}
		}
		targetSpec := FindPodsTargetSpec(hpa, metric.Pods.Metric.Name)
		target := FormatMetricTarget(targetSpec)
		current := FormatMetricValueStatus(metric.Pods.Current)
		ratio, note := calculateRatioAndNote(metric.Pods.Current, targetSpec, target)
		text := fmt.Sprintf("Pods %s current=%s target=%s", metric.Pods.Metric.Name, current, target)
		if ratio != nil {
			text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
		}
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "Pods",
			Name:    metric.Pods.Metric.Name,
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	case autoscalingv2.ObjectMetricSourceType:
		if metric.Object == nil {
			return Metric{Type: "Object", Text: "Object metric: <missing status>"}
		}
		targetSpec := FindObjectTargetSpec(hpa, metric.Object.Metric.Name)
		target := FormatMetricTarget(targetSpec)
		current := FormatMetricValueStatus(metric.Object.Current)
		ratio, note := calculateRatioAndNote(metric.Object.Current, targetSpec, target)
		name := fmt.Sprintf("%s/%s", metric.Object.DescribedObject.Kind, metric.Object.DescribedObject.Name)
		text := fmt.Sprintf("Object %s %s current=%s target=%s", name, metric.Object.Metric.Name, current, target)
		if ratio != nil {
			text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
		}
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "Object",
			Name:    metric.Object.Metric.Name,
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	case autoscalingv2.ExternalMetricSourceType:
		if metric.External == nil {
			return Metric{Type: "External", Text: "External metric: <missing status>"}
		}
		targetSpec := FindExternalTargetSpec(hpa, metric.External.Metric.Name)
		target := FormatMetricTarget(targetSpec)
		current := FormatMetricValueStatus(metric.External.Current)
		ratio, note := calculateRatioAndNote(metric.External.Current, targetSpec, target)
		text := fmt.Sprintf("External %s current=%s target=%s", metric.External.Metric.Name, current, target)
		if ratio != nil {
			text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
		}
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "External",
			Name:    metric.External.Metric.Name,
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	default:
		return Metric{
			Type: string(metric.Type),
			Text: fmt.Sprintf("%s metric is present, but this plugin does not know how to format it in detail", metric.Type),
		}
	}
}

// FindResourceTargetSpec returns the target specification for a resource metric.
func FindResourceTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType &&
			spec.Resource != nil &&
			string(spec.Resource.Name) == name {
			return spec.Resource.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindResourceTarget returns the formatted target string for a resource metric.
func FindResourceTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindResourceTargetSpec(hpa, name))
}

// FindContainerResourceTargetSpec returns the target spec for a container resource metric.
func FindContainerResourceTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name, container string) autoscalingv2.MetricTarget {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ContainerResourceMetricSourceType &&
			spec.ContainerResource != nil &&
			string(spec.ContainerResource.Name) == name &&
			spec.ContainerResource.Container == container {
			return spec.ContainerResource.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindContainerResourceTarget returns the formatted target for a container resource metric.
func FindContainerResourceTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name, container string) string {
	return FormatMetricTarget(FindContainerResourceTargetSpec(hpa, name, container))
}

// FindPodsTargetSpec returns the target specification for a pods metric.
func FindPodsTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.PodsMetricSourceType &&
			spec.Pods != nil &&
			spec.Pods.Metric.Name == name {
			return spec.Pods.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindPodsTarget returns the formatted target string for a pods metric.
func FindPodsTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindPodsTargetSpec(hpa, name))
}

// FindObjectTargetSpec returns the target specification for an object metric.
func FindObjectTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ObjectMetricSourceType &&
			spec.Object != nil &&
			spec.Object.Metric.Name == name {
			return spec.Object.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindObjectTarget returns the formatted target string for an object metric.
func FindObjectTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindObjectTargetSpec(hpa, name))
}

// FindExternalTargetSpec returns the target specification for an external metric.
func FindExternalTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ExternalMetricSourceType &&
			spec.External != nil &&
			spec.External.Metric.Name == name {
			return spec.External.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindExternalTarget returns the formatted target string for an external metric.
func FindExternalTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindExternalTargetSpec(hpa, name))
}

func hasCurrentExternalMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) bool {
	_, ok := currentExternalMetric(hpa, name)
	return ok
}

func currentExternalMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (autoscalingv2.MetricStatus, bool) {
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type == autoscalingv2.ExternalMetricSourceType &&
			metric.External != nil &&
			metric.External.Metric.Name == name {
			return metric, true
		}
	}
	return autoscalingv2.MetricStatus{}, false
}

func currentObjectMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (autoscalingv2.MetricStatus, bool) {
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type == autoscalingv2.ObjectMetricSourceType &&
			metric.Object != nil &&
			metric.Object.Metric.Name == name {
			return metric, true
		}
	}
	return autoscalingv2.MetricStatus{}, false
}

// FormatMetricTarget returns a human-readable string for a metric target.
func FormatMetricTarget(target autoscalingv2.MetricTarget) string {
	switch target.Type {
	case autoscalingv2.UtilizationMetricType:
		if target.AverageUtilization != nil {
			return fmt.Sprintf("%d%%", *target.AverageUtilization)
		}
	case autoscalingv2.AverageValueMetricType:
		if target.AverageValue != nil {
			return target.AverageValue.String()
		}
	case autoscalingv2.ValueMetricType:
		if target.Value != nil {
			return target.Value.String()
		}
	}
	return "<unknown>"
}

// FormatMetricValue returns a formatted string for utilization or average value.
func FormatMetricValue(utilization *int32, averageValue *resource.Quantity) string {
	if utilization != nil {
		return fmt.Sprintf("%d%%", *utilization)
	}
	if averageValue != nil && !averageValue.IsZero() {
		return averageValue.String()
	}
	return "<unknown>"
}

// FormatMetricValueStatus returns a formatted string for a metric value status.
func FormatMetricValueStatus(value autoscalingv2.MetricValueStatus) string {
	if value.AverageUtilization != nil {
		return fmt.Sprintf("%d%%", *value.AverageUtilization)
	}
	if value.AverageValue != nil && !value.AverageValue.IsZero() {
		return value.AverageValue.String()
	}
	if value.Value != nil && !value.Value.IsZero() {
		return value.Value.String()
	}
	return "<unknown>"
}

// FormatBehavior extracts and formats HPA behavior rules.
func FormatBehavior(hpa *autoscalingv2.HorizontalPodAutoscaler) []BehaviorRule {
	if hpa.Spec.Behavior == nil {
		return nil
	}

	var out []BehaviorRule
	if rule := FormatBehaviorRule("scaleUp", hpa.Spec.Behavior.ScaleUp); rule != nil {
		out = append(out, *rule)
	}
	if rule := FormatBehaviorRule("scaleDown", hpa.Spec.Behavior.ScaleDown); rule != nil {
		out = append(out, *rule)
	}
	return out
}

// FormatBehaviorRule formats a single behavior rule (scaleUp or scaleDown).
func FormatBehaviorRule(direction string, rules *autoscalingv2.HPAScalingRules) *BehaviorRule {
	if rules == nil {
		return nil
	}

	rule := BehaviorRule{
		Direction:                  direction,
		StabilizationWindowSeconds: rules.StabilizationWindowSeconds,
	}
	if rules.SelectPolicy != nil {
		rule.SelectPolicy = string(*rules.SelectPolicy)
	}
	if rules.Tolerance != nil && !rules.Tolerance.IsZero() {
		rule.Policies = append(rule.Policies, "tolerance "+rules.Tolerance.String())
	}
	for _, policy := range rules.Policies {
		rule.Policies = append(rule.Policies, fmt.Sprintf("%s %d per %ds", policy.Type, policy.Value, policy.PeriodSeconds))
	}

	var parts []string
	if rule.StabilizationWindowSeconds != nil {
		parts = append(parts, fmt.Sprintf("stabilizationWindow=%ds", *rule.StabilizationWindowSeconds))
	}
	if rule.SelectPolicy != "" {
		parts = append(parts, "selectPolicy="+rule.SelectPolicy)
	}
	if len(rule.Policies) > 0 {
		parts = append(parts, "policies="+strings.Join(rule.Policies, ", "))
	}
	if len(parts) == 0 {
		parts = append(parts, "custom behavior is present")
	}
	rule.Text = direction + ": " + strings.Join(parts, "; ")
	return &rule
}

// RecommendedActions generates actionable recommendation strings.
func RecommendedActions(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []string {
	var actions []string
	if hpa.Status.ObservedGeneration != nil && *hpa.Status.ObservedGeneration < hpa.Generation {
		actions = append(actions, "Wait for the HPA controller to observe the latest spec generation before trusting this status.")
	}
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		actions = append(actions, "Check metrics-server or custom/external metrics adapters; ScalingActive is not True.")
		actions = append(actions, staleMetricActions(hpa)...)
		return actions
	}
	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		if window := scaleDownStabilizationWindow(hpa); window != nil {
			actions = append(actions, fmt.Sprintf("CPU or memory may already be low, but scale-down is stabilized; wait up to about %ds or review spec.behavior.scaleDown.stabilizationWindowSeconds.", *window))
		} else {
			actions = append(actions, "CPU or memory may already be low, but scale-down is stabilized; review HPA behavior and recent recommendations.")
		}
	}
	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			actions = append(actions, "HPA is capped at maxReplicas; raise maxReplicas or reduce load/target utilization if more capacity is expected.")
		case minReplicas:
			actions = append(actions, "HPA is capped at minReplicas; lower minReplicas if scale-down below this point is expected.")
		}
	}
	if len(actions) == 0 && hpa.Status.DesiredReplicas == hpa.Status.CurrentReplicas {
		actions = append(actions, "No immediate action is visible from HPA status; inspect metrics and recent Events if behavior is unexpected.")
	}
	return actions
}

func staleMetricActions(hpa *autoscalingv2.HorizontalPodAutoscaler) []string {
	var actions []string
	for _, spec := range hpa.Spec.Metrics {
		switch {
		case spec.Type == autoscalingv2.ExternalMetricSourceType && spec.External != nil:
			actions = append(actions, fmt.Sprintf("Verify external metric %q in the external metrics API; if it is retired, remove it from spec.metrics so it no longer blocks scaling.", spec.External.Metric.Name))
		case spec.Type == autoscalingv2.ObjectMetricSourceType && spec.Object != nil:
			actions = append(actions, fmt.Sprintf("Verify object metric %q and its described object %s/%s before changing replica bounds.", spec.Object.Metric.Name, spec.Object.DescribedObject.Kind, spec.Object.DescribedObject.Name))
		}
	}
	return actions
}

// buildStructuredInterpretation mirrors the key cases from Interpret() and
// returns machine-readable StructuredMessage entries.
func buildStructuredInterpretation(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []StructuredMessage {
	var msgs []StructuredMessage

	// Stale status (observedGeneration lag)
	if hpa.Status.ObservedGeneration != nil && *hpa.Status.ObservedGeneration < hpa.Generation {
		msgs = append(msgs, StructuredMessage{
			Reason:   "StaleStatus",
			Message:  fmt.Sprintf("observedGeneration=%d is behind generation=%d", *hpa.Status.ObservedGeneration, hpa.Generation),
			NextStep: "Wait for HPA controller to process latest spec",
			Severity: "warning",
		})
	}

	// ScalingActive not True
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		msgs = append(msgs, StructuredMessage{
			Reason:   "ScalingInactive",
			Message:  fmt.Sprintf("ScalingActive is %s: %s - %s", condition.Status, condition.Reason, condition.Message),
			NextStep: "Check metrics-server or custom metrics adapters",
			Severity: "error",
		})
		return msgs
	}

	// AbleToScale not True
	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Status != corev1.ConditionTrue {
		msgs = append(msgs, StructuredMessage{
			Reason:   "UnableToScale",
			Message:  fmt.Sprintf("AbleToScale is %s: %s - %s", condition.Status, condition.Reason, condition.Message),
			Severity: "error",
		})
	} else if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		nextStep := ""
		if remaining := estimateStabilizationRemaining(hpa); remaining != nil && *remaining > 0 {
			nextStep = fmt.Sprintf("Scale-down stabilized; approximately %d seconds remaining", *remaining)
		}
		msgs = append(msgs, StructuredMessage{
			Reason:   "ScaleDownStabilized",
			Message:  condition.Message,
			NextStep: nextStep,
			Severity: "info",
		})
	}

	// ScalingLimited
	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			msgs = append(msgs, StructuredMessage{
				Reason:   "LimitedByMaxReplicas",
				Message:  "desiredReplicas is constrained by maxReplicas",
				NextStep: "Raise maxReplicas or reduce load/target utilization",
				Severity: "warning",
			})
		case minReplicas:
			msgs = append(msgs, StructuredMessage{
				Reason:   "LimitedByMinReplicas",
				Message:  "desiredReplicas is constrained by minReplicas",
				NextStep: "Lower minReplicas if scale-down below this point is expected",
				Severity: "warning",
			})
		default:
			msgs = append(msgs, StructuredMessage{
				Reason:   "ScalingLimited",
				Message:  "The recommendation is reported as limited",
				Severity: "warning",
			})
		}
	}

	// Tolerance-confirmed no-scale
	if hpa.Status.DesiredReplicas == hpa.Status.CurrentReplicas &&
		hpa.Status.DesiredReplicas != hpa.Spec.MaxReplicas &&
		hpa.Status.DesiredReplicas != minReplicas {
		if metric, ok := MetricOutsideTarget(hpa); ok {
			deviation := metric.Ratio - 1.0
			if deviation < 0 {
				deviation = -deviation
			}
			if deviation < 0.1 {
				msgs = append(msgs, StructuredMessage{
					Reason:   "ToleranceNoScale",
					Message:  fmt.Sprintf("%s metric ratio is %.3f (within ±10%% of target)", metric.Name, metric.Ratio),
					Severity: "info",
				})
			}
		}
	}

	// maxReplicas winner hidden
	if hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas && len(hpa.Status.CurrentMetrics) > 1 {
		msgs = append(msgs, StructuredMessage{
			Reason:   "MaxReplicasWinnerHidden",
			Message:  "desiredReplicas == maxReplicas; the winning metric cannot be reliably determined",
			Severity: "info",
		})
	}

	// Scale-to-zero
	if minReplicas == 0 {
		if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas == 0 {
			msgs = append(msgs, StructuredMessage{
				Reason:   "ScaleToZero",
				Message:  "Scale-to-zero enabled and workload is at zero replicas",
				NextStep: "Next scale-up requires a cold start which may introduce additional latency",
				Severity: "info",
			})
		} else if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas > 0 {
			msgs = append(msgs, StructuredMessage{
				Reason:   "ScaleToZero",
				Message:  "Scale-to-zero enabled and HPA wants to scale to zero",
				NextStep: "Scaling from 0 back to 1 requires a cold start",
				Severity: "info",
			})
		}
	}

	// VPA conflict

	return msgs
}

// buildStructuredActions mirrors the key cases from RecommendedActions() and
// returns machine-readable StructuredMessage entries.
func buildStructuredActions(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []StructuredMessage {
	var msgs []StructuredMessage

	// Wait for generation
	if hpa.Status.ObservedGeneration != nil && *hpa.Status.ObservedGeneration < hpa.Generation {
		msgs = append(msgs, StructuredMessage{
			Reason:   "WaitForGeneration",
			Message:  "Status does not reflect the latest spec",
			NextStep: "Wait for controller reconciliation",
			Severity: "warning",
		})
	}

	// ScalingActive not True → check metrics
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		msgs = append(msgs, StructuredMessage{
			Reason:   "RestoreMetrics",
			Message:  "ScalingActive is not True",
			NextStep: "Check metrics-server or custom/external metrics adapters",
			Severity: "error",
		})
		return msgs
	}

	// ScaleDownStabilized
	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		nextStep := "Review HPA behavior and recent recommendations"
		if window := scaleDownStabilizationWindow(hpa); window != nil {
			nextStep = fmt.Sprintf("Wait up to about %ds or review spec.behavior.scaleDown.stabilizationWindowSeconds", *window)
		}
		msgs = append(msgs, StructuredMessage{
			Reason:   "WaitForStabilization",
			Message:  "Scale-down is stabilized",
			NextStep: nextStep,
			Severity: "info",
		})
	}

	// ScalingLimited
	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			msgs = append(msgs, StructuredMessage{
				Reason:   "RaiseMaxReplicas",
				Message:  "HPA is capped at maxReplicas",
				NextStep: "Raise maxReplicas or reduce load/target utilization if more capacity is expected",
				Severity: "warning",
			})
		case minReplicas:
			msgs = append(msgs, StructuredMessage{
				Reason:   "LowerMinReplicas",
				Message:  "HPA is capped at minReplicas",
				NextStep: "Lower minReplicas if scale-down below this point is expected",
				Severity: "warning",
			})
		}
	}

	return msgs
}

// DebugLines generates verbose debug information lines.
func DebugLines(hpa *autoscalingv2.HorizontalPodAutoscaler, analysis Analysis) []string {
	var lines []string
	lines = append(lines, fmt.Sprintf("replicas: current=%d desired=%d min=%d max=%d diff=%+d", analysis.Current, analysis.Desired, analysis.Min, analysis.Max, analysis.Desired-analysis.Current))
	lines = append(lines, fmt.Sprintf("health: state=%s score=%d", analysis.Health, analysis.HealthScore))
	for _, metric := range analysis.Metrics {
		if metric.Ratio == nil {
			lines = append(lines, fmt.Sprintf("metric %s/%s: current=%s target=%s ratio=<unknown> note=%q", metric.Type, metric.Name, metric.Current, metric.Target, metric.Note))
			continue
		}
		lines = append(lines, fmt.Sprintf("metric %s/%s: current=%s target=%s ratio=%.3f note=%q", metric.Type, metric.Name, metric.Current, metric.Target, *metric.Ratio, metric.Note))
	}
	for _, condition := range hpa.Status.Conditions {
		lines = append(lines, fmt.Sprintf("condition %s=%s reason=%s", condition.Type, condition.Status, condition.Reason))
	}
	if analysis.ImpactMetric != nil {
		lines = append(lines, fmt.Sprintf("impactEstimate: metric=%s ratio=%.3f confidence=medium", analysis.ImpactMetric.Name, analysis.ImpactMetric.Ratio))
	}
	return lines
}

func scaleDownStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) *int32 {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}
	return hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
}

// estimateStabilizationRemaining estimates how many seconds remain before
// the scale-down stabilization window expires. Returns nil if the HPA is
// not in a ScaleDownStabilized state or required data is unavailable.
func estimateStabilizationRemaining(hpa *autoscalingv2.HorizontalPodAutoscaler) *int64 {
	condition := FindCondition(hpa, "AbleToScale")
	if condition == nil || condition.Reason != "ScaleDownStabilized" {
		return nil
	}
	window := scaleDownStabilizationWindow(hpa)
	if window == nil {
		return nil
	}
	if hpa.Status.LastScaleTime == nil {
		return nil
	}
	elapsed := time.Since(hpa.Status.LastScaleTime.Time).Seconds()
	remaining := int64(float64(*window) - elapsed)
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}

// CompareMetricToTarget returns a comparison description for utilization vs target.
func CompareMetricToTarget(utilization *int32, target string) string {
	if utilization == nil || !strings.HasSuffix(target, "%") {
		return ""
	}

	targetUtilization, ok := parsePercent(target)
	if !ok {
		return ""
	}

	switch {
	case *utilization > targetUtilization:
		return "current value is above target"
	case *utilization < targetUtilization:
		return "current value is below target"
	default:
		return "current value equals target"
	}
}

// MetricOutsideTarget finds a resource metric whose ratio differs from 1.0.
func MetricOutsideTarget(hpa *autoscalingv2.HorizontalPodAutoscaler) (MetricImpactGuess, bool) {
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type != autoscalingv2.ResourceMetricSourceType || metric.Resource == nil {
			continue
		}
		ratio := utilizationRatio(metric.Resource.Current.AverageUtilization, FindResourceTarget(hpa, string(metric.Resource.Name)))
		if ratio != nil && *ratio != 1 {
			return MetricImpactGuess{Name: string(metric.Resource.Name), Ratio: *ratio}, true
		}
	}

	return MetricImpactGuess{}, false
}

// MostInfluentialMetric estimates which resource metric has the largest scaling impact.
func MostInfluentialMetric(hpa *autoscalingv2.HorizontalPodAutoscaler) (MetricImpactGuess, bool) {
	var best MetricImpactGuess
	var bestScore float64

	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type != autoscalingv2.ResourceMetricSourceType || metric.Resource == nil {
			continue
		}
		ratio := utilizationRatio(metric.Resource.Current.AverageUtilization, FindResourceTarget(hpa, string(metric.Resource.Name)))
		if ratio == nil {
			continue
		}
		distance := *ratio - 1
		if distance < 0 {
			distance = -distance
		}

		// Score by estimated replica impact: ratio * currentReplicas gives
		// a rough estimate of how many replicas this metric would want.
		// Higher impact = more likely to be the winner.
		replicaImpact := distance * float64(hpa.Status.CurrentReplicas)

		if replicaImpact > bestScore {
			bestScore = replicaImpact
			note := "largest visible utilization ratio distance from target"
			if hpa.Status.CurrentReplicas > 0 {
				note = fmt.Sprintf("estimated replica impact %.1f (ratio distance %.3f × %d current replicas)", replicaImpact, distance, hpa.Status.CurrentReplicas)
			}
			best = MetricImpactGuess{
				Name:  string(metric.Resource.Name),
				Ratio: *ratio,
				Note:  note,
			}
		}
	}

	return best, bestScore > 0
}

func prioritizedConditions(conditions []autoscalingv2.HorizontalPodAutoscalerCondition) []autoscalingv2.HorizontalPodAutoscalerCondition {
	out := append([]autoscalingv2.HorizontalPodAutoscalerCondition(nil), conditions...)
	priority := map[autoscalingv2.HorizontalPodAutoscalerConditionType]int{
		"ScalingActive":  0,
		"AbleToScale":    1,
		"ScalingLimited": 2,
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := priority[out[i].Type]
		right := priority[out[j].Type]
		if _, ok := priority[out[i].Type]; !ok {
			left = 100
		}
		if _, ok := priority[out[j].Type]; !ok {
			right = 100
		}
		return left < right
	})
	return out
}

func utilizationRatio(utilization *int32, target string) *float64 {
	if utilization == nil {
		return nil
	}
	targetUtilization, ok := parsePercent(target)
	if !ok || targetUtilization == 0 {
		return nil
	}
	ratio := float64(*utilization) / float64(targetUtilization)
	return &ratio
}

func parsePercent(value string) (int32, bool) {
	if !strings.HasSuffix(value, "%") {
		return 0, false
	}
	var percent int32
	if _, err := fmt.Sscanf(strings.TrimSuffix(value, "%"), "%d", &percent); err != nil {
		return 0, false
	}
	return percent, true
}

func quantityRatio(current, target *resource.Quantity) *float64 {
	if current == nil || target == nil || target.IsZero() {
		return nil
	}
	ratio := current.AsApproximateFloat64() / target.AsApproximateFloat64()
	return &ratio
}

// CompareQuantityToTarget returns a comparison description for quantity values.
func CompareQuantityToTarget(current, target *resource.Quantity) string {
	if current == nil || target == nil {
		return ""
	}
	cmp := current.Cmp(*target)
	switch {
	case cmp > 0:
		return "current value is above target"
	case cmp < 0:
		return "current value is below target"
	default:
		return "current value equals target"
	}
}
