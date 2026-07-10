// Package hpa provides HPA analysis, health scoring, metric formatting,
// and diagnostic interpretation for HorizontalPodAutoscaler resources.
package hpa

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/suggestion"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/warmup"
)

const limitation = "[observed] This plugin uses existing HPA status, conditions, metrics, and events. It does not expose internal controller calculations."

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
// Field policy: Analysis is the JSON/YAML output surface consumed by scripts
// and external tools, so existing field names and shapes are frozen (renames
// and removals require a SchemaVersion bump; see docs/output-schema.json and
// the consistency test in output_schema_test.go). New analysis domains must
// NOT add loose scalar fields here; group them into a dedicated sub-struct
// exposed through a single pointer field (as HealthResult, CapacityContext,
// and BlockerReport do) so the struct grows by feature, stays navigable, and
// omits empty domains from serialized output via omitempty.
type Analysis struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA resource name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// Current is the current replica count from HPA status.
	Current int32 `json:"currentReplicas" yaml:"currentReplicas"`
	// Desired is the desired replica count from HPA status.
	Desired int32 `json:"desiredReplicas" yaml:"desiredReplicas"`
	// Min is the minimum replica count (defaults to 1 if spec.minReplicas is nil).
	Min int32 `json:"minReplicas" yaml:"minReplicas"`
	// Max is the maximum replica count from spec.maxReplicas.
	Max int32 `json:"maxReplicas" yaml:"maxReplicas"`
	// Health is the health state: "OK", "ERROR", "LIMITED", or "STABILIZED".
	Health string `json:"health" yaml:"health"`
	// HealthScore is the numeric health score from 0 (worst) to 100 (best).
	HealthScore int `json:"healthScore" yaml:"healthScore"`
	// HealthResult holds the typed health state, score, and individual penalty
	// signals. Populated when --debug is enabled or for JSON/YAML output.
	HealthResult *HealthResult `json:"healthResult,omitempty" yaml:"healthResult,omitempty"`
	// HiddenFactors lists HPA decision factors that influence the controller
	// but are only partially visible through public status fields.
	HiddenFactors []HiddenDecisionFactor `json:"hiddenFactors,omitempty" yaml:"hiddenFactors,omitempty"`
	// Summary is a one-line direction summary of the HPA scaling state.
	Summary string `json:"summary" yaml:"summary"`
	// SummaryKey is the stable i18n key (e.g. "dir_scale_up") that identifies
	// which branch of SummarizeDirection produced Summary. It lets renderers
	// translate Summary without re-deriving the decision switch from the
	// English text, and gives machine consumers a stable enum. Empty only when
	// Summary has been overwritten outside SummarizeDirection (e.g. the stale
	// prefix); in that case Summary renders verbatim.
	SummaryKey string `json:"summaryKey,omitempty" yaml:"summaryKey,omitempty"`
	// Conditions lists the HPA conditions sorted by priority.
	Conditions []Condition `json:"conditions" yaml:"conditions"`
	// Metrics lists formatted metric data for each current metric.
	Metrics []Metric `json:"metrics" yaml:"metrics"`
	// Behavior lists the scale-up and scale-down behavior rules, if configured.
	Behavior []BehaviorRule `json:"behavior,omitempty" yaml:"behavior,omitempty"`
	// Actions lists recommended action strings for the operator.
	Actions []string `json:"recommendedActions,omitempty" yaml:"recommendedActions,omitempty"`
	// Suggestions lists patch suggestions with safety metadata.
	Suggestions []Suggestion `json:"suggestions,omitempty" yaml:"suggestions,omitempty"`
	// Interpretation lists detailed interpretation lines with confidence labels.
	Interpretation []string `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	// KEDAInfo holds KEDA-specific analysis, populated when --keda is enabled.
	KEDAInfo *KEDAAnalysis `json:"keda,omitempty" yaml:"keda,omitempty"`
	// VPAConflict holds VPA conflict detection results, populated when --vpa is enabled.
	VPAConflict *vpa.ConflictInfo `json:"vpaConflict,omitempty" yaml:"vpaConflict,omitempty"`
	// TargetReplicas holds replica status from the scale target resource.
	TargetReplicas *TargetReplicaInfo `json:"targetReplicas,omitempty" yaml:"targetReplicas,omitempty"`
	// Debug lists verbose debug lines, populated when the debug option is enabled.
	Debug []string `json:"debug,omitempty" yaml:"debug,omitempty"`
	// ImpactMetric estimates which metric has the largest scaling impact.
	ImpactMetric *MetricImpactGuess `json:"impactMetric,omitempty" yaml:"impactMetric,omitempty"`
	// CreationTimestamp is the HPA creation time.
	CreationTimestamp metav1.Time `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"`
	// StaleStatus indicates observedGeneration lag, if detected.
	StaleStatus *StaleStatusInfo `json:"staleStatus,omitempty" yaml:"staleStatus,omitempty"`
	// StabilizationRemaining estimates seconds remaining in the scale-down stabilization window.
	StabilizationRemaining *int64 `json:"stabilizationRemaining,omitempty" yaml:"stabilizationRemaining,omitempty"`
	// ScaleToZero holds scale-to-zero information, populated when minReplicas=0.
	ScaleToZero *ScaleToZeroInfo `json:"scaleToZero,omitempty" yaml:"scaleToZero,omitempty"`
	// StructuredInterpretation provides machine-readable interpretation entries.
	StructuredInterpretation []StructuredMessage `json:"structuredInterpretation,omitempty" yaml:"structuredInterpretation,omitempty"`
	// StructuredActions provides machine-readable action entries.
	StructuredActions []StructuredMessage `json:"structuredActions,omitempty" yaml:"structuredActions,omitempty"`
	// DecisionSignals holds future-proof scaling decision data for KEP-6111 compatibility.
	// Currently unused; future HPA API versions may populate this field.
	DecisionSignals []DecisionSignal `json:"decisionSignals,omitempty" yaml:"decisionSignals,omitempty"`
	// StabilizationWindowSeconds is the configured scale-down stabilization window.
	StabilizationWindowSeconds *int32 `json:"stabilizationWindowSeconds,omitempty" yaml:"stabilizationWindowSeconds,omitempty"`
	// StabilizationSource indicates which behavior direction caused stabilization:
	// "scaleDown" or "scaleUp". Populated when StabilizationRemaining > 0.
	StabilizationSource string `json:"stabilizationSource,omitempty" yaml:"stabilizationSource,omitempty"`
	// StabilizationConfidence is the confidence label for stabilization estimates.
	// Always "medium (API limitation)" since the estimate is based on LastScaleTime.
	StabilizationConfidence string `json:"stabilizationConfidence,omitempty" yaml:"stabilizationConfidence,omitempty"`
	// MetricsDiagnostics holds per-metric health check results for the metrics pipeline.
	MetricsDiagnostics *MetricsPipelineDiagnostics `json:"metricsDiagnostics,omitempty" yaml:"metricsDiagnostics,omitempty"`
	// MetricFreshnessEntries holds per-metric freshness analysis results.
	// Populated when --metrics-freshness is enabled.
	MetricFreshnessEntries []MetricFreshness `json:"metricFreshness,omitempty" yaml:"metricFreshness,omitempty"`
	// ResourceCheck holds warnings about resource request/limit consistency with HPA targets.
	ResourceCheck *ResourceCheckResult `json:"resourceCheck,omitempty" yaml:"resourceCheck,omitempty"`
	// PodAnalysis holds per-pod readiness and resource analysis for the scale target.
	PodAnalysis *PodAnalysis `json:"podAnalysis,omitempty" yaml:"podAnalysis,omitempty"`
	// MetricDecisionTrace holds a comprehensive per-metric analysis explaining
	// which metric drove the HPA scaling decision and why. Populated when
	// multiple current metrics are present.
	MetricDecisionTrace *MetricDecisionTrace `json:"metricDecisionTrace,omitempty" yaml:"metricDecisionTrace,omitempty"`
	// DecisionTrace holds the human-oriented step-by-step HPA decision trace.
	// It is best-effort and uses only stable Kubernetes API fields.
	DecisionTrace *DecisionTrace `json:"decisionTrace,omitempty" yaml:"decisionTrace,omitempty"`
	// FlappingSimulation holds what-if analysis results from --simulate.
	FlappingSimulation *SimulationResult `json:"simulation,omitempty" yaml:"simulation,omitempty"`
	// CapacityContext holds infrastructure capacity analysis for the scale target.
	CapacityContext *CapacityContext `json:"capacityContext,omitempty" yaml:"capacityContext,omitempty"`
	// CapacityHeadroom estimates whether the cluster can absorb additional pods
	// up to maxReplicas.
	CapacityHeadroom *CapacityHeadroom `json:"capacityHeadroom,omitempty" yaml:"capacityHeadroom,omitempty"`
	// ReadinessImpact explains how not-yet-ready pods and missing PodMetrics may
	// affect HPA CPU/resource decisions.
	ReadinessImpact *ReadinessImpact `json:"readinessImpact,omitempty" yaml:"readinessImpact,omitempty"`
	// ScalePath explains the visible path from HPA desired replicas to scheduled pods.
	ScalePath *ScalePath `json:"scalePath,omitempty" yaml:"scalePath,omitempty"`
	// RolloutDiagnosis holds Deployment/StatefulSet rollout context for HPA diagnosis.
	RolloutDiagnosis *RolloutDiagnosis `json:"rolloutDiagnosis,omitempty" yaml:"rolloutDiagnosis,omitempty"`
	// ControllerProfile holds cluster-wide HPA controller timing assumptions.
	ControllerProfile *ControllerProfile `json:"controllerProfile,omitempty" yaml:"controllerProfile,omitempty"`
	// BlockerReport holds scale-out blocker analysis for the HPA scale target.
	// Populated when --capacity-deep is enabled or via the blockers subcommand.
	BlockerReport *blocker.Report `json:"blockerReport,omitempty" yaml:"blockerReport,omitempty"`
	// CapacityPlan holds a pre-flight capacity check result, diagnosing whether
	// it is safe to raise maxReplicas. Populated when --capacity-plan is enabled
	// or via the capacity subcommand.
	CapacityPlan *CapacityPlan `json:"capacityPlan,omitempty" yaml:"capacityPlan,omitempty"`
	// EnrichmentStatus holds KEDA/VPA enrichment skip reasons for diagnostic
	// output, populated during enrichment to explain why data may be absent.
	//
	// This is a JSON-mirror of internal/enrichment.Status, defined here because
	// pkg/hpa cannot import the internal/enrichment package (Go internal package
	// visibility). The internal/enrichment package converts its Status into this
	// type before attaching it to Analysis. Treat the serialised shape as a
	// public contract: add fields additively, never rename or reorder existing
	// keys.
	EnrichmentStatus *EnrichmentStatus `json:"enrichmentStatus,omitempty" yaml:"enrichmentStatus,omitempty"`
	// MetricContract holds metrics contract validation results, populated when
	// --metric-contract is enabled.
	MetricContract *MetricContractReport `json:"metricContract,omitempty" yaml:"metricContract,omitempty"`
	// GitOpsConflict holds GitOps manifest conflict detection results, populated when
	// --gitops-check is enabled or --manifest is provided.
	GitOpsConflict *GitOpsConflict `json:"gitopsConflict,omitempty" yaml:"gitopsConflict,omitempty"`
	// ChurnAnalysis holds the thrashing/churn detection result for the HPA timeline.
	// Populated when --churn-detect is enabled or during doctor command.
	ChurnAnalysis *ChurnAnalysis `json:"churnAnalysis,omitempty" yaml:"churnAnalysis,omitempty"`
	// VPAAdvisory holds the VPA-HPA coexistence advisory result, providing
	// structured recommendations when VPA and HPA target the same workload.
	// Populated when --vpa is enabled and a VPA conflict is detected.
	VPAAdvisory *vpa.Advisory `json:"vpaAdvisory,omitempty" yaml:"vpaAdvisory,omitempty"`
	// MetricHints holds troubleshooting hints for custom/external metrics,
	// identifying common failure patterns with remediation steps.
	// Populated when --metric-hints is enabled.
	MetricHints *MetricHintsReport `json:"metricHints,omitempty" yaml:"metricHints,omitempty"`
	// WarmupAnalysis holds the warmup analysis result, diagnosing why pods
	// are not yet ready after HPA scales out. Populated when --warmup is enabled
	// or during the doctor command.
	WarmupAnalysis *warmup.Analysis `json:"warmupAnalysis,omitempty" yaml:"warmupAnalysis,omitempty"`
	// ContainerAdvisor holds the ContainerResource advisor result, suggesting
	// ContainerResource metrics for multi-container workloads.
	// Populated when --container-advisor is enabled.
	ContainerAdvisor *ContainerAdvisorResult `json:"containerAdvisor,omitempty" yaml:"containerAdvisor,omitempty"`
	// BehaviorAdvisor holds the behavior tuning advisor result, analyzing
	// scaleUp/scaleDown policies, stabilization windows, and tolerance.
	// Populated when --behavior-advisor is enabled.
	BehaviorAdvisor *BehaviorAdvisorResult `json:"behaviorAdvisor,omitempty" yaml:"behaviorAdvisor,omitempty"`
	// HealthTrend holds the health score trend analysis over time.
	// Populated when --trend is enabled and sufficient history is available.
	HealthTrend *HealthTrendResult `json:"healthTrend,omitempty" yaml:"healthTrend,omitempty"`
	// StructuredDecisionTrace holds the comprehensive structured decision trace
	// with schema version, winner metric, tolerance/stabilization effects, and
	// full condition evaluation. Populated when --decision-trace-format is set.
	StructuredDecisionTrace *StructuredDecisionTrace `json:"structuredDecisionTrace,omitempty" yaml:"structuredDecisionTrace,omitempty"`
	// FlappingPrevention holds the flapping prevention advisor result with
	// what-if simulations for different stabilization window values.
	// Populated when --flapping-advisor is enabled.
	FlappingPrevention *FlappingPreventionReport `json:"flappingPrevention,omitempty" yaml:"flappingPrevention,omitempty"`
	// FlappingDiagnosis holds the result of event-based flapping detection with
	// root-cause analysis (tight target, short stabilization window, etc.).
	FlappingDiagnosis *FlappingDiagnosis `json:"flappingDiagnosis,omitempty" yaml:"flappingDiagnosis,omitempty"`
	// AdapterDiagnostics holds custom/external metrics adapter diagnostics.
	// Populated when --adapter-diagnostics is enabled.
	AdapterDiagnostics *AdapterDiagnosticsReport `json:"adapterDiagnostics,omitempty" yaml:"adapterDiagnostics,omitempty"`
	// Assumptions documents inferred/estimated values the analysis relies on
	// (tolerance, stabilizationRemaining, ...), each with its derivation source
	// and a confidence label so consumers can judge reliability.
	Assumptions []Assumption `json:"assumptions,omitempty" yaml:"assumptions,omitempty"`
	// Warnings records enrichment-pipeline errors and notable skip reasons so
	// operators can see why an expected piece of analysis is missing. Empty by
	// default; populated only when an enricher fails or a critical enrichment
	// step is skipped.
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
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

// Metric holds formatted metric data including current, target, ratio, and display text.
type Metric struct {
	Type     string   `json:"type" yaml:"type"`
	Name     string   `json:"name,omitempty" yaml:"name,omitempty"`
	Selector string   `json:"selector,omitempty" yaml:"selector,omitempty"`
	Object   string   `json:"object,omitempty" yaml:"object,omitempty"`
	Current  string   `json:"current,omitempty" yaml:"current,omitempty"`
	Target   string   `json:"target,omitempty" yaml:"target,omitempty"`
	Ratio    *float64 `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	Note     string   `json:"note,omitempty" yaml:"note,omitempty"`
	Text     string   `json:"text" yaml:"text"`
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
// JSON-mirror of internal/enrichment.Entry.
type EnrichmentStatusEntry struct {
	Source EnrichmentSource `json:"source" yaml:"source"`
	State  EnrichmentState  `json:"state" yaml:"state"`
	Reason string           `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// EnrichmentStatus holds the enrichment outcomes for all sources.
// JSON-mirror of internal/enrichment.Status; attached to Analysis for
// visibility in --debug and -o json output.
type EnrichmentStatus struct {
	KEDA *EnrichmentStatusEntry `json:"keda,omitempty" yaml:"keda,omitempty"`
	VPA  *EnrichmentStatusEntry `json:"vpa,omitempty" yaml:"vpa,omitempty"`
}
