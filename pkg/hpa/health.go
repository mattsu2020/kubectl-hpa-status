package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

const (
	enrichmentPenaltyKEDAInactive = "KEDA trigger inactive"
	enrichmentPenaltyVPAConflict  = "VPA conflict detected"
	enrichmentPenaltyChurn        = "High replica churn (thrashing) detected"
)

// Health computes the health state and score using default penalty weights.
func Health(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) (string, int) {
	result := HealthWithWeights(hpa, minReplicas, HealthWeights{})
	return string(result.State), result.Score
}

// HealthAccumulator centralizes health score updates so that all penalties
// (condition-based and enrichment-based) flow through a single mechanism.
// This prevents score/signal drift and makes penalty application auditable.
type HealthAccumulator struct {
	result HealthResult
}

// NewHealthAccumulator creates an accumulator starting at the given base score.
func NewHealthAccumulator(baseScore int) *HealthAccumulator {
	return &HealthAccumulator{
		result: HealthResult{Score: baseScore},
	}
}

// AddPenalty records a health penalty with reason and severity.
func (h *HealthAccumulator) AddPenalty(reason string, penalty int, severity HealthState) {
	h.result.Score -= penalty
	h.result.Signals = append(h.result.Signals, HealthSignal{
		Reason:   reason,
		Penalty:  penalty,
		Severity: severity,
	})
}

// SetState overrides the health state classification.
func (h *HealthAccumulator) SetState(state HealthState) {
	h.result.State = state
}

// Result returns a copy of the accumulated health result.
func (h *HealthAccumulator) Result() HealthResult {
	// Return a copy to preserve immutability
	signals := make([]HealthSignal, len(h.result.Signals))
	copy(signals, h.result.Signals)
	return HealthResult{
		State:   h.result.State,
		Score:   h.result.Score,
		Signals: signals,
	}
}

// hasCondition reports whether the HPA has a condition with the given type and status.
func hasCondition(conditions []autoscalingv2.HorizontalPodAutoscalerCondition, conditionType string, status corev1.ConditionStatus) bool {
	for _, c := range conditions {
		if string(c.Type) == conditionType && c.Status == status {
			return true
		}
	}
	return false
}

// hasMetricAboveTarget reports whether any current metric has a ratio above 1.0,
// indicating visible scaling pressure.
func hasMetricAboveTarget(currentMetrics []autoscalingv2.MetricStatus, hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, metric := range currentMetrics {
		_, ratio := metricImpactRatio(hpa, metric)
		if ratio != nil && *ratio > 1.0 {
			return true
		}
	}
	return false
}

// HealthWithWeights computes the typed health result using configurable penalty weights.
// Each penalty applied is recorded as a HealthSignal for transparency.
func HealthWithWeights(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, weights HealthWeights) HealthResult {
	if hpa == nil {
		return HealthResult{State: HealthError, Score: 0}
	}
	w := resolveWeights(weights)

	acc := NewHealthAccumulator(healthScoreMax)
	health := HealthOK

	health = applyConditionPenalties(acc, hpa.Status.Conditions, w, health)
	health = applyMaxReplicasCeilingPenalty(acc, hpa, w, health)
	if hpa.Status.DesiredReplicas == minReplicas && hasCondition(hpa.Status.Conditions, ConditionScalingLimited, corev1.ConditionTrue) {
		acc.AddPenalty("At minimum replicas with ScalingLimited", w.atMinimumReplicas, health)
	}
	acc.SetState(health)
	result := acc.Result()
	if result.Score < 0 {
		result.Score = 0
	}
	return result
}

// applyConditionPenalties walks HPA conditions and applies the matching penalty, returning the updated worst-case health state.
func applyConditionPenalties(acc *HealthAccumulator, conditions []autoscalingv2.HorizontalPodAutoscalerCondition, w resolvedWeights, health HealthState) HealthState {
	for _, condition := range conditions {
		switch {
		case condition.Type == ConditionScalingActive && condition.Status != corev1.ConditionTrue:
			acc.AddPenalty("ScalingActive is not True", w.scalingInactive, HealthError)
			health = HealthError
		case condition.Type == ConditionAbleToScale && condition.Status != corev1.ConditionTrue:
			acc.AddPenalty("AbleToScale is not True", w.unableToScale, HealthError)
			health = HealthError
		case condition.Type == ConditionScalingLimited && condition.Status == corev1.ConditionTrue:
			acc.AddPenalty("ScalingLimited is True", w.scalingLimited, HealthLimited)
			if health != HealthError {
				health = HealthLimited
			}
		case condition.Type == ConditionAbleToScale && condition.Reason == "ScaleDownStabilized":
			acc.AddPenalty("ScaleDownStabilized", w.scaleDownStabilized, HealthStabilized)
			if health == HealthOK {
				health = HealthStabilized
			}
		}
	}
	return health
}

// applyMaxReplicasCeilingPenalty applies the implicit maxReplicas penalty when replicas are pinned at max with pressure.
func applyMaxReplicasCeilingPenalty(acc *HealthAccumulator, hpa *autoscalingv2.HorizontalPodAutoscaler, w resolvedWeights, health HealthState) HealthState {
	if hpa.Status.CurrentReplicas != hpa.Status.DesiredReplicas || hpa.Status.DesiredReplicas != hpa.Spec.MaxReplicas {
		return health
	}
	hasLimited := hasCondition(hpa.Status.Conditions, ConditionScalingLimited, corev1.ConditionTrue)
	hasPressure := hasMetricAboveTarget(hpa.Status.CurrentMetrics, hpa)
	// ScalingLimited already carries the explicit ceiling penalty. The implicit
	// signal exists only for controllers/status snapshots that expose pressure
	// at maxReplicas without the condition, so applying both would double-count
	// the same capacity constraint.
	if hasLimited || !hasPressure {
		return health
	}
	acc.AddPenalty("Implicit maxReplicas ceiling (current==desired==max with pressure)", w.implicitMaxReplicas, HealthLimited)
	if health == HealthOK {
		health = HealthLimited
	}
	return health
}

// resolvedWeights is the internal resolved form of HealthWeights where all
// nil pointers have been replaced with default penalty values.
type resolvedWeights struct {
	scalingInactive     int
	unableToScale       int
	scalingLimited      int
	implicitMaxReplicas int
	scaleDownStabilized int
	atMinimumReplicas   int
	kedaInactiveTrigger int
	vpaConflict         int
	churn               int
}

func resolveWeights(w HealthWeights) resolvedWeights {
	return resolvedWeights{
		scalingInactive:     weightOr(w.ScalingInactive, healthPenaltyScalingInactive),
		unableToScale:       weightOr(w.UnableToScale, healthPenaltyUnableToScale),
		scalingLimited:      weightOr(w.ScalingLimited, healthPenaltyScalingLimited),
		implicitMaxReplicas: weightOr(w.ImplicitMaxReplicas, healthPenaltyImplicitMaxReplicas),
		scaleDownStabilized: weightOr(w.ScaleDownStabilized, healthPenaltyScaleDownStabilized),
		atMinimumReplicas:   weightOr(w.AtMinimumReplicas, healthPenaltyAtMinimumReplicas),
		kedaInactiveTrigger: weightOr(w.KEDAInactiveTrigger, healthPenaltyKEDAInactiveTrigger),
		vpaConflict:         weightOr(w.VPAConflict, healthPenaltyVPAConflict),
		churn:               weightOr(w.Churn, healthPenaltyChurn),
	}
}

// weightOr returns the pointed-to value, or the default if nil.
func weightOr(w *int, defaultVal int) int {
	if w != nil {
		return *w
	}
	return defaultVal
}

type dynamicHealthBaseline struct {
	score   int
	state   HealthState
	signals []HealthSignal
}

func newDynamicHealthBaseline(score int, state HealthState, signals []HealthSignal) *dynamicHealthBaseline {
	return &dynamicHealthBaseline{
		score:   score,
		state:   state,
		signals: append([]HealthSignal(nil), signals...),
	}
}

func isDynamicHealthSignal(reason string) bool {
	switch reason {
	case enrichmentPenaltyKEDAInactive, enrichmentPenaltyVPAConflict, enrichmentPenaltyChurn:
		return true
	default:
		return false
	}
}

func ensureDynamicHealthBaseline(a *Analysis) *dynamicHealthBaseline {
	if a.dynamicHealthBaseline != nil {
		return a.dynamicHealthBaseline
	}
	score := a.HealthScore
	state := HealthState(a.Health)
	var signals []HealthSignal
	removedDynamicSignal := false
	if a.HealthResult != nil {
		for _, signal := range a.HealthResult.Signals {
			if isDynamicHealthSignal(signal.Reason) {
				// Best-effort recovery for a value produced by an older
				// in-memory caller that did not retain a baseline.
				score += signal.Penalty
				removedDynamicSignal = true
				continue
			}
			signals = append(signals, signal)
		}
	}
	if removedDynamicSignal {
		state = healthStateFromSignals(signals)
	}
	if score > healthScoreMax {
		score = healthScoreMax
	}
	a.dynamicHealthBaseline = newDynamicHealthBaseline(score, state, signals)
	return a.dynamicHealthBaseline
}

func healthStateFromSignals(signals []HealthSignal) HealthState {
	state := HealthOK
	for _, signal := range signals {
		switch signal.Severity {
		case HealthError:
			return HealthError
		case HealthLimited:
			if state != HealthError {
				state = HealthLimited
			}
		case HealthStabilized:
			if state == HealthOK {
				state = HealthStabilized
			}
		}
	}
	return state
}

func hasInactiveKEDATrigger(a *Analysis) bool {
	if a.KEDAInfo == nil {
		return false
	}
	for _, trigger := range a.KEDAInfo.Triggers {
		if strings.EqualFold(trigger.Status, "Inactive") || strings.EqualFold(trigger.Status, "False") {
			return true
		}
	}
	return false
}

func hasHighChurn(a *Analysis) bool {
	return a.ChurnAnalysis != nil &&
		(a.ChurnAnalysis.Level == churn.ChurnHigh || a.ChurnAnalysis.Level == churn.ChurnCritical)
}

func reconcileDynamicHealthPenalties(a *Analysis, weights HealthWeights) {
	if a == nil {
		return
	}
	baseline := ensureDynamicHealthBaseline(a)
	resolved := resolveWeights(weights)
	acc := NewHealthAccumulator(baseline.score)
	acc.result.Signals = append(acc.result.Signals, baseline.signals...)

	hasDynamicPenalty := false
	if hasInactiveKEDATrigger(a) {
		acc.AddPenalty(enrichmentPenaltyKEDAInactive, resolved.kedaInactiveTrigger, HealthLimited)
		hasDynamicPenalty = true
	}
	if a.VPAConflict != nil {
		acc.AddPenalty(enrichmentPenaltyVPAConflict, resolved.vpaConflict, HealthLimited)
		hasDynamicPenalty = true
	}
	if hasHighChurn(a) {
		acc.AddPenalty(enrichmentPenaltyChurn, resolved.churn, HealthLimited)
		hasDynamicPenalty = true
	}

	state := baseline.state
	if hasDynamicPenalty && state != HealthError {
		state = HealthLimited
	}
	acc.SetState(state)
	result := acc.Result()
	if result.Score < 0 {
		result.Score = 0
	}
	a.HealthScore = result.Score
	a.Health = string(result.State)
	a.HealthResult = &result
}

// ApplyEnrichmentPenalties reconciles KEDA and VPA enrichment data with the
// immutable base health result. Repeated calls, changed weights, and signals
// becoming inactive all produce a fresh deterministic result.
func ApplyEnrichmentPenalties(a *Analysis, weights HealthWeights) {
	reconcileDynamicHealthPenalties(a, weights)
}

// ApplyChurnPenalty reconciles the churn signal with all dynamic health
// penalties. Calling it after churn subsides removes the previous penalty.
func ApplyChurnPenalty(a *Analysis, weights HealthWeights) {
	reconcileDynamicHealthPenalties(a, weights)
}
