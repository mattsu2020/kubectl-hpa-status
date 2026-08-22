// Package hpa provides HPA analysis, health scoring, metric formatting,
// and diagnostic interpretation for HorizontalPodAutoscaler resources.
package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/core"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/confidence"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/suggestion"
)

const limitation = confidence.BadgeObserved + " This plugin uses existing HPA status, conditions, metrics, and events. It does not expose internal controller calculations."

const (
	healthScoreMax = 100

	// healthPenaltyScalingInactive is the largest penalty because when the
	// metrics pipeline is unavailable the HPA cannot compute any trustworthy
	// recommendation. The controller stops producing desiredReplicas updates,
	// and the existing replica count may be stale. Operators must restore
	// metric availability before any other HPA tuning matters.
	healthPenaltyScalingInactive = 45

	// healthPenaltyUnableToScale is nearly as severe because the HPA controller
	// is explicitly reporting that it cannot act on scaling decisions, even if
	// metrics are available. Common causes include invalid scaleTargetRef,
	// RBAC issues, or the scale subresource being missing.
	healthPenaltyUnableToScale = 35

	// healthPenaltyScalingLimited indicates the HPA is capped by minReplicas
	// or maxReplicas. This is a lower penalty because capacity limits can be
	// intentional policy, but the operator should verify whether demand truly
	// requires more (or fewer) replicas.
	healthPenaltyScalingLimited = 25

	// healthPenaltyImplicitMaxReplicas is a smaller penalty than explicit
	// ScalingLimited because it is inferred from current==desired==max without
	// a ScalingLimited condition. This can be a transient status lag.
	healthPenaltyImplicitMaxReplicas = 20

	// healthPenaltyScaleDownStabilized is advisory: the HPA is deliberately
	// holding off on scale-down within the stabilization window. No urgent
	// action is needed but operators should be aware of the suppressed
	// scale-down.
	healthPenaltyScaleDownStabilized = 10

	// healthPenaltyAtMinimumReplicas is informational: the workload is at its
	// floor. The score drop is small because this can be normal behavior for
	// low-traffic periods, but it signals that the HPA has no room to scale
	// down further.
	healthPenaltyAtMinimumReplicas = 5

	// healthPenaltyKEDAInactiveTrigger is applied when a KEDA trigger reports
	// Inactive status, meaning the external event source is not producing
	// events. The HPA may not scale up even if demand increases.
	healthPenaltyKEDAInactiveTrigger = 15

	// healthPenaltyVPAConflict is applied when both VPA and HPA target the
	// same resource (CPU/memory) on the same workload, which can cause
	// conflicting scaling decisions.
	healthPenaltyVPAConflict = 20

	// healthPenaltyChurn is applied when the HPA exhibits high replica churn
	// (thrashing), indicating frequent scaling direction reversals that
	// suggest the stabilization window or tolerance needs adjustment.
	healthPenaltyChurn = 15
)

// AnalysisOptions configures the analysis behavior.
type AnalysisOptions struct {
	HealthWeights HealthWeights `json:"healthWeights,omitempty" yaml:"healthWeights,omitempty"`
	Debug         bool          `json:"debug,omitempty" yaml:"debug,omitempty"`
}

// HealthWeights holds configurable penalty values for health score computation.
// nil means "use the default penalty"; a pointer to 0 means "explicitly disable
// this penalty". Use the IntWeight helper to construct non-nil values.
type HealthWeights struct {
	ScalingInactive     *int `json:"scalingInactive,omitempty" yaml:"scalingInactive,omitempty"`
	UnableToScale       *int `json:"unableToScale,omitempty" yaml:"unableToScale,omitempty"`
	ScalingLimited      *int `json:"scalingLimited,omitempty" yaml:"scalingLimited,omitempty"`
	ImplicitMaxReplicas *int `json:"implicitMaxReplicas,omitempty" yaml:"implicitMaxReplicas,omitempty"`
	ScaleDownStabilized *int `json:"scaleDownStabilized,omitempty" yaml:"scaleDownStabilized,omitempty"`
	AtMinimumReplicas   *int `json:"atMinimumReplicas,omitempty" yaml:"atMinimumReplicas,omitempty"`
	KEDAInactiveTrigger *int `json:"kedaInactiveTrigger,omitempty" yaml:"kedaInactiveTrigger,omitempty"`
	VPAConflict         *int `json:"vpaConflict,omitempty" yaml:"vpaConflict,omitempty"`
	Churn               *int `json:"churn,omitempty" yaml:"churn,omitempty"`
}

// IntWeight returns a pointer to the given int value. Use this to set
// explicit HealthWeights values, including 0 to disable a penalty.
func IntWeight(v int) *int { return &v }

// Clone returns a deep copy of the weights. Each *int field is independently
// allocated so mutating one copy (e.g. flipping a weight to zero to disable a
// penalty) does not leak into the other. nil pointers stay nil. Use this when
// a Root copy needs to diverge its health-weight configuration.
func (w HealthWeights) Clone() HealthWeights {
	clonePtr := func(p *int) *int {
		if p == nil {
			return nil
		}
		v := *p
		return &v
	}
	return HealthWeights{
		ScalingInactive:     clonePtr(w.ScalingInactive),
		UnableToScale:       clonePtr(w.UnableToScale),
		ScalingLimited:      clonePtr(w.ScalingLimited),
		ImplicitMaxReplicas: clonePtr(w.ImplicitMaxReplicas),
		ScaleDownStabilized: clonePtr(w.ScaleDownStabilized),
		AtMinimumReplicas:   clonePtr(w.AtMinimumReplicas),
		KEDAInactiveTrigger: clonePtr(w.KEDAInactiveTrigger),
		VPAConflict:         clonePtr(w.VPAConflict),
		Churn:               clonePtr(w.Churn),
	}
}

// Analysis holds the complete analysis result for a single HPA.
//
// Storage and API policy (v4): the 13 grouped views below ARE both the
// in-memory storage and the public Go surface — reads and writes go through
// the exported group fields (a.Replicas.Current). The historical flat v1
// fields and their accessor methods were removed in v4.0.0; the changelog
// migration table maps each retired field to its group, and
// docs/analysis-storage-flip.md records the transition. Analysis serializes
// as the grouped v2 shape (see GroupedAnalysis / ProjectStatusReportV2).
//
// Field policy: new analysis domains must NOT add loose scalar fields here;
// group them into a dedicated sub-struct exposed through a single pointer
// field (as HealthResult, CapacityContext, and BlockerReport do) so the
// struct grows by feature, stays navigable, and omits empty domains from
// serialized output via omitempty.
type Analysis struct {
	// Meta carries the identity group: namespace, name, target, and the
	// creation timestamp as second-precision RFC3339 (the v2 wire shape).
	Meta MetaView `json:"meta" yaml:"meta"`
	// Replicas carries the core scaling envelope.
	Replicas ReplicasView `json:"replicas" yaml:"replicas"`
	// Decision carries the "why this replica count" signals.
	Decision DecisionView `json:"decision" yaml:"decision"`
	// Metrics carries the metric-pipeline health signals.
	Metrics MetricsView `json:"metrics" yaml:"metrics"`
	// Conditions carries HPA controller conditions and behavior configuration.
	Conditions ConditionsView `json:"conditions" yaml:"conditions"`
	// Capacity carries scheduling and cluster capacity signals.
	Capacity CapacityView `json:"capacity" yaml:"capacity"`
	// ScaleToZero carries scale-to-zero and cold-start/warmup signals.
	ScaleToZero ScaleToZeroView `json:"scaleToZero" yaml:"scaleToZero"`
	// Stability carries flapping and churn diagnosis signals.
	Stability StabilityView `json:"stability" yaml:"stability"`
	// Advisory carries VPA and container/behavior tuning advice.
	Advisory AdvisoryView `json:"advisory" yaml:"advisory"`
	// Controllers carries external controller integrations.
	Controllers ControllersView `json:"controllers" yaml:"controllers"`
	// Blockers carries apply-time gating signals.
	Blockers BlockersView `json:"blockers" yaml:"blockers"`
	// Actions carries the recommendation/explainability output.
	Actions ActionsView `json:"actions" yaml:"actions"`
	// Lifecycle carries freshness/trend/telemetry signals.
	Lifecycle LifecycleView `json:"lifecycle" yaml:"lifecycle"`

	// dynamicHealthBaseline is the immutable pre-enrichment health state used
	// to reconcile KEDA, VPA, and churn penalties without accumulating or
	// losing score through zero-value clamping. It is excluded from the wire
	// model.
	dynamicHealthBaseline *dynamicHealthBaseline
}

// HiddenDecisionFactor describes a partially visible HPA decision input such
// as missing metrics, not-yet-ready pods, tolerance, or stabilization.
type HiddenDecisionFactor struct {
	Name       string   `json:"name" yaml:"name"`
	Status     string   `json:"status" yaml:"status"`
	Evidence   []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	Impact     string   `json:"impact" yaml:"impact"`
	Confidence string   `json:"confidence" yaml:"confidence"`
}

// DecisionSignal is the stable internal shape for explicit controller scaling
// decision data. Current Kubernetes HPA status does not expose these fields;
// future structured status adapters should populate this slice and renderers
// should prefer it over best-effort inference when present.
//
// Future extensibility: When KEP-6111 (HPA Decision Explainability) lands,
// an adapter should convert the API's decision fields into DecisionSignal
// entries. The Reason field maps to the API's decision reason, Message to
// the human-readable explanation, and MetricName/Source identify the
// contributing metric or external trigger.
type DecisionSignal struct {
	Reason     string `json:"reason" yaml:"reason"`
	Message    string `json:"message,omitempty" yaml:"message,omitempty"`
	MetricName string `json:"metricName,omitempty" yaml:"metricName,omitempty"`
	Source     string `json:"source,omitempty" yaml:"source,omitempty"`
	Confidence string `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	// Classification is the user-facing evidence tier derived from Confidence:
	// "observed" (high — read directly from HPA status), "estimated" (medium —
	// inferred from visible signals), or "unknown" (low — not exposed by the
	// HPA controller). Surfacing it alongside Confidence lets tooling render a
	// consistent [observed]/[estimated]/[assumed] label without re-deriving the
	// mapping. See pkg/hpa/internal/confidence.
	Classification string `json:"classification,omitempty" yaml:"classification,omitempty"`
	// AdapterVersion identifies which adapter produced this signal.
	// "estimation-v1" for the current inference-based adapter.
	// "kep6111-v1" for the future structured output adapter.
	AdapterVersion string `json:"adapterVersion,omitempty" yaml:"adapterVersion,omitempty"`
}

// StructuredMessage provides a machine-readable representation of an
// interpretation or action line, with a reason, human message, and
// suggested next step.
type StructuredMessage struct {
	Reason         string         `json:"reason" yaml:"reason"`
	Message        string         `json:"message" yaml:"message"`
	NextStep       string         `json:"nextStep,omitempty" yaml:"nextStep,omitempty"`
	Severity       Severity       `json:"severity,omitempty" yaml:"severity,omitempty"`
	Confidence     Confidence     `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	Classification Classification `json:"classification,omitempty" yaml:"classification,omitempty"`
}

// Condition represents an HPA condition with type, status, reason, and message.
type Condition struct {
	Type    string `json:"type" yaml:"type"`
	Status  string `json:"status" yaml:"status"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// Metric is re-exported from core for source compatibility.
type Metric = core.Metric

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

// Suggestion is a type alias for suggestion.Suggestion (canonical definition
// in pkg/hpa/internal/suggestion).
type Suggestion = suggestion.Suggestion

// GuardResult is a type alias for suggestion.GuardResult.
type GuardResult = suggestion.GuardResult

// GuardBlocked is a type alias for suggestion.GuardBlocked.
type GuardBlocked = suggestion.GuardBlocked

// GuardWarning is a type alias for suggestion.GuardWarning.
type GuardWarning = suggestion.GuardWarning

// EnrichmentSource identifies which enrichment system produced a status entry.
type EnrichmentSource string

const (
	// EnrichmentSourceKEDA indicates KEDA ScaledObject enrichment.
	EnrichmentSourceKEDA EnrichmentSource = "keda"
	// EnrichmentSourceVPA indicates VerticalPodAutoscaler enrichment.
	EnrichmentSourceVPA EnrichmentSource = "vpa"
)

// EnrichmentState describes the outcome of an enrichment operation.
type EnrichmentState string

const (
	// EnrichmentStateActive means enrichment data was successfully retrieved.
	EnrichmentStateActive EnrichmentState = "active"
	// EnrichmentStateSkipped means the HPA was not relevant for this enrichment.
	EnrichmentStateSkipped EnrichmentState = "skipped"
	// EnrichmentStateDisabled means the enrichment source was not requested.
	EnrichmentStateDisabled EnrichmentState = "disabled"
	// EnrichmentStateUnavailable means the required CRD is not installed.
	EnrichmentStateUnavailable EnrichmentState = "unavailable"
	// EnrichmentStateError means enrichment failed due to an error.
	EnrichmentStateError EnrichmentState = "error"
)

// EnrichmentStatusEntry records the outcome for a single enrichment source.
// It is the canonical model shared by analysis output and internal enrichment.
type EnrichmentStatusEntry struct {
	Source EnrichmentSource `json:"source" yaml:"source"`
	State  EnrichmentState  `json:"state" yaml:"state"`
	Reason string           `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// EnrichmentStatus holds the enrichment outcomes for all sources. It is
// attached to Analysis for visibility in --debug and structured output.
type EnrichmentStatus struct {
	KEDA *EnrichmentStatusEntry `json:"keda,omitempty" yaml:"keda,omitempty"`
	VPA  *EnrichmentStatusEntry `json:"vpa,omitempty" yaml:"vpa,omitempty"`
}
