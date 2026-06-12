// Package hpa provides HPA analysis, health scoring, metric formatting,
// and diagnostic interpretation for HorizontalPodAutoscaler resources.
package hpa

import (
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// Analysis holds the complete analysis result for a single HPA.
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
	VPAConflict *VPAConflictInfo `json:"vpaConflict,omitempty" yaml:"vpaConflict,omitempty"`
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
	// Simulation holds what-if analysis results from --simulate.
	Simulation *SimulationResult `json:"simulation,omitempty" yaml:"simulation,omitempty"`
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
	BlockerReport *BlockerReport `json:"blockerReport,omitempty" yaml:"blockerReport,omitempty"`
	// CapacityPlan holds a pre-flight capacity check result, diagnosing whether
	// it is safe to raise maxReplicas. Populated when --capacity-plan is enabled
	// or via the capacity subcommand.
	CapacityPlan *CapacityPlan `json:"capacityPlan,omitempty" yaml:"capacityPlan,omitempty"`
	// EnrichmentStatus holds KEDA/VPA enrichment skip reasons for diagnostic output.
	// Populated during enrichment to explain why data may be absent.
	EnrichmentStatus interface{} `json:"enrichmentStatus,omitempty" yaml:"enrichmentStatus,omitempty"`
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
	VPAAdvisory *VPAAdvisory `json:"vpaAdvisory,omitempty" yaml:"vpaAdvisory,omitempty"`
	// MetricHints holds troubleshooting hints for custom/external metrics,
	// identifying common failure patterns with remediation steps.
	// Populated when --metric-hints is enabled.
	MetricHints *MetricHintsReport `json:"metricHints,omitempty" yaml:"metricHints,omitempty"`
	// WarmupAnalysis holds the warmup analysis result, diagnosing why pods
	// are not yet ready after HPA scales out. Populated when --warmup is enabled
	// or during the doctor command.
	WarmupAnalysis *WarmupAnalysis `json:"warmupAnalysis,omitempty" yaml:"warmupAnalysis,omitempty"`
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

// GuardResult holds policy guard decisions for suggested patches.
type GuardResult struct {
	Allowed  []Suggestion   `json:"allowed,omitempty" yaml:"allowed,omitempty"`
	Blocked  []GuardBlocked `json:"blocked,omitempty" yaml:"blocked,omitempty"`
	Warnings []GuardWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// GuardBlocked describes a suggestion blocked by a policy rule.
type GuardBlocked struct {
	Suggestion Suggestion `json:"suggestion" yaml:"suggestion"`
	Reason     string     `json:"reason" yaml:"reason"`
	PolicyRule string     `json:"policyRule" yaml:"policyRule"`
}

// GuardWarning describes a suggestion allowed with a policy warning.
type GuardWarning struct {
	Suggestion Suggestion `json:"suggestion" yaml:"suggestion"`
	Reason     string     `json:"reason" yaml:"reason"`
	PolicyRule string     `json:"policyRule" yaml:"policyRule"`
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
	Fallback         *KEDAFallbackInfo    `json:"fallback,omitempty" yaml:"fallback,omitempty"`
}

// KEDATriggerSummary is a display-oriented summary of a KEDA trigger.
type KEDATriggerSummary struct {
	Type         string `json:"type" yaml:"type"`
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
	Status       string `json:"status,omitempty" yaml:"status,omitempty"`
	Message      string `json:"message,omitempty" yaml:"message,omitempty"`
	MetricName   string `json:"metricName,omitempty" yaml:"metricName,omitempty"`
	Threshold    string `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	CurrentValue string `json:"currentValue,omitempty" yaml:"currentValue,omitempty"`
	AuthRef      string `json:"authRef,omitempty" yaml:"authRef,omitempty"`
}

// KEDAFallbackInfo holds fallback information for display.
type KEDAFallbackInfo struct {
	FailureThreshold int32 `json:"failureThreshold" yaml:"failureThreshold"`
	Replicas         int32 `json:"replicas" yaml:"replicas"`
}

// TargetReplicaInfo holds replica status from the scale target resource.
// When not-ready pods exist, HPA scaling calculations may be affected.
type TargetReplicaInfo struct {
	TotalReplicas int32 `json:"totalReplicas" yaml:"totalReplicas"`
	ReadyReplicas int32 `json:"readyReplicas" yaml:"readyReplicas"`
	NotReady      int32 `json:"notReady" yaml:"notReady"`
	Pending       int32 `json:"pending,omitempty" yaml:"pending,omitempty"`
	Unschedulable int32 `json:"unschedulable,omitempty" yaml:"unschedulable,omitempty"`
}

// MetricsPipelineDiagnostics holds the results of metrics pipeline health checks.
type MetricsPipelineDiagnostics struct {
	OverallStatus    string                 `json:"overallStatus" yaml:"overallStatus"`
	PerMetricChecks  []PerMetricHealthCheck `json:"perMetricChecks,omitempty" yaml:"perMetricChecks,omitempty"`
	RemediationSteps []string               `json:"remediationSteps,omitempty" yaml:"remediationSteps,omitempty"`
}

// PerMetricHealthCheck describes the health of a single metric source.
type PerMetricHealthCheck struct {
	MetricType  string `json:"metricType" yaml:"metricType"`
	MetricName  string `json:"metricName" yaml:"metricName"`
	Selector    string `json:"selector,omitempty" yaml:"selector,omitempty"`
	Status      string `json:"status" yaml:"status"` // "healthy", "missing", "stale"
	Details     string `json:"details,omitempty" yaml:"details,omitempty"`
	Remediation string `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

// AdapterDiagnosticsReport summarizes visible custom/external metrics adapter health.
type AdapterDiagnosticsReport struct {
	AdapterType          string                `json:"adapterType" yaml:"adapterType"`
	EndpointHealthy      bool                  `json:"endpointHealthy" yaml:"endpointHealthy"`
	AvailableMetrics     []string              `json:"availableMetrics,omitempty" yaml:"availableMetrics,omitempty"`
	AuthenticationErrors []string              `json:"authenticationErrors,omitempty" yaml:"authenticationErrors,omitempty"`
	QueryProposals       []MetricQueryProposal `json:"queryProposals,omitempty" yaml:"queryProposals,omitempty"`
	Checks               []AdapterCheck        `json:"checks,omitempty" yaml:"checks,omitempty"`
	Summary              string                `json:"summary" yaml:"summary"`
}

// AdapterCheck describes one visible adapter diagnostic check.
type AdapterCheck struct {
	Name        string `json:"name" yaml:"name"`
	Status      string `json:"status" yaml:"status"`
	Details     string `json:"details,omitempty" yaml:"details,omitempty"`
	Remediation string `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

// MetricQueryProposal suggests a metrics API query for troubleshooting.
type MetricQueryProposal struct {
	MetricName    string `json:"metricName" yaml:"metricName"`
	ProposedQuery string `json:"proposedQuery" yaml:"proposedQuery"`
	Adapter       string `json:"adapter" yaml:"adapter"`
}

// MetricFreshnessStatus represents the freshness state of a single HPA metric.
type MetricFreshnessStatus string

const (
	// FreshnessOK means the metric has recent data available.
	FreshnessOK MetricFreshnessStatus = "OK"
	// FreshnessStale means the metric data is older than expected.
	FreshnessStale MetricFreshnessStatus = "Stale"
	// FreshnessMissing means the metric has no current data in HPA status.
	FreshnessMissing MetricFreshnessStatus = "Missing"
	// FreshnessUnknown means freshness cannot be determined.
	FreshnessUnknown MetricFreshnessStatus = "Unknown"
)

// MetricFreshness holds the freshness analysis for a single HPA metric.
type MetricFreshness struct {
	// Name is the metric display name (e.g., "cpu", "queue_depth").
	Name string `json:"name" yaml:"name"`
	// Type is the metric source type (Resource, Pods, Object, External, ContainerResource).
	Type string `json:"type" yaml:"type"`
	// Status is the freshness state: OK, Stale, Missing, Unknown.
	Status string `json:"status" yaml:"status"`
	// LastSeen is the timestamp when the metric was last observed, if available.
	LastSeen *metav1.Time `json:"lastSeen,omitempty" yaml:"lastSeen,omitempty"`
	// Age is the duration since LastSeen. Zero if LastSeen is nil.
	Age time.Duration `json:"age,omitempty" yaml:"age,omitempty"`
	// Source is the metrics API serving this metric (e.g., metrics.k8s.io,
	// custom.metrics.k8s.io, external.metrics.k8s.io).
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
	// Window is the expected metric collection window (e.g., "30s" for resource metrics).
	Window string `json:"window,omitempty" yaml:"window,omitempty"`
	// APIServiceAvailable records whether the backing metrics API was visible
	// through Kubernetes API discovery at analysis time.
	APIServiceAvailable *bool `json:"apiServiceAvailable,omitempty" yaml:"apiServiceAvailable,omitempty"`
	// APIServiceMessage explains API discovery or APIService availability evidence.
	APIServiceMessage string `json:"apiServiceMessage,omitempty" yaml:"apiServiceMessage,omitempty"`
	// LastEvent is the latest HPA event related to this metric, if one was visible.
	LastEvent *Event `json:"lastEvent,omitempty" yaml:"lastEvent,omitempty"`
	// Risk describes the HPA behavior risk from stale/missing data.
	Risk string `json:"risk,omitempty" yaml:"risk,omitempty"`
	// Evidence lists observed signals supporting the freshness status.
	Evidence []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	// NextSteps lists kubectl commands or actions for remediation.
	NextSteps []string `json:"nextSteps,omitempty" yaml:"nextSteps,omitempty"`
}

// ResourceCheckResult holds warnings about resource request/limit consistency with HPA targets.
type ResourceCheckResult struct {
	Warnings []ResourceWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// ResourceWarning describes a single resource consistency issue.
type ResourceWarning struct {
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Category  string `json:"category" yaml:"category"` // "missing-requests", "zero-requests", "target-vs-request-mismatch"
	Details   string `json:"details" yaml:"details"`
	Severity  string `json:"severity" yaml:"severity"` // "warning", "error"
}

// PodAnalysis holds per-pod readiness and resource analysis for HPA scale target pods.
type PodAnalysis struct {
	Total           int32              `json:"total" yaml:"total"`
	Ready           int32              `json:"ready" yaml:"ready"`
	Unready         int32              `json:"unready" yaml:"unready"`
	Pending         int32              `json:"pending" yaml:"pending"`
	Terminating     int32              `json:"terminating" yaml:"terminating"`
	ResourceIssues  []PodResourceIssue `json:"resourceIssues,omitempty" yaml:"resourceIssues,omitempty"`
	ContainerChecks []ContainerCheck   `json:"containerChecks,omitempty" yaml:"containerChecks,omitempty"`
}

// PodResourceIssue describes a pod container missing CPU or memory requests/limits.
type PodResourceIssue struct {
	Pod       string `json:"pod" yaml:"pod"`
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Category  string `json:"category" yaml:"category"` // "missing-request", "missing-limit"
}

// HealthSnapshot records a single health observation for trend tracking.
type HealthSnapshot struct {
	Timestamp       time.Time `json:"timestamp" yaml:"timestamp"`
	HealthScore     int       `json:"healthScore" yaml:"healthScore"`
	HealthState     string    `json:"healthState" yaml:"healthState"`
	DesiredReplicas int32     `json:"desiredReplicas" yaml:"desiredReplicas"`
	CurrentReplicas int32     `json:"currentReplicas" yaml:"currentReplicas"`
	Stabilizing     bool      `json:"stabilizing,omitempty" yaml:"stabilizing,omitempty"`
}

// HealthTrendResult holds the analysis of health score history over time.
type HealthTrendResult struct {
	Snapshots        []HealthSnapshot   `json:"snapshots" yaml:"snapshots"`
	Variance         float64            `json:"variance" yaml:"variance"`
	MinScore         int                `json:"minScore" yaml:"minScore"`
	MaxScore         int                `json:"maxScore" yaml:"maxScore"`
	MeanScore        float64            `json:"meanScore" yaml:"meanScore"`
	DegradationRate  float64            `json:"degradationRate" yaml:"degradationRate"`
	FlappingDetected bool               `json:"flappingDetected" yaml:"flappingDetected"`
	FlappingSeverity string             `json:"flappingSeverity,omitempty" yaml:"flappingSeverity,omitempty"`
	Sparkline        string             `json:"sparkline,omitempty" yaml:"sparkline,omitempty"`
	Anomalies        []AnomalyDetection `json:"anomalies,omitempty" yaml:"anomalies,omitempty"`
}

// ContainerCheck verifies that a ContainerResource metric target container exists in pods.
type ContainerCheck struct {
	Container string `json:"container" yaml:"container"`
	Found     bool   `json:"found" yaml:"found"`
	Message   string `json:"message,omitempty" yaml:"message,omitempty"`
}

// SimulationResult holds the before/after comparison of an HPA simulation.
type SimulationResult struct {
	Parameter            string             `json:"parameter" yaml:"parameter"`
	OriginalValue        string             `json:"originalValue" yaml:"originalValue"`
	SimulatedValue       string             `json:"simulatedValue" yaml:"simulatedValue"`
	Before               SimulationState    `json:"before" yaml:"before"`
	After                SimulationState    `json:"after" yaml:"after"`
	Confidence           string             `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	RiskAssessment       string             `json:"riskAssessment,omitempty" yaml:"riskAssessment,omitempty"`
	Interpretation       []string           `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	MetricSimulations    []MetricSimulation `json:"metricSimulations,omitempty" yaml:"metricSimulations,omitempty"`
	TimeSeriesProjection []ProjectedState   `json:"timeSeriesProjection,omitempty" yaml:"timeSeriesProjection,omitempty"`
	RiskWarnings         []string           `json:"riskWarnings,omitempty" yaml:"riskWarnings,omitempty"`
}

// SimulationState is a snapshot of key analysis fields for before/after comparison.
type SimulationState struct {
	DesiredReplicas int32    `json:"desiredReplicas" yaml:"desiredReplicas"`
	Health          string   `json:"health" yaml:"health"`
	HealthScore     int      `json:"healthScore" yaml:"healthScore"`
	Summary         string   `json:"summary" yaml:"summary"`
	ScalingLimited  bool     `json:"scalingLimited" yaml:"scalingLimited"`
	Metrics         []Metric `json:"metrics,omitempty" yaml:"metrics,omitempty"`
}

// ProjectedState holds a single point in a time-series projection showing
// estimated replica count at a given time offset.
type ProjectedState struct {
	TimeOffset           int32   `json:"timeOffset" yaml:"timeOffset"`
	ProjectedReplicas    int32   `json:"projectedReplicas" yaml:"projectedReplicas"`
	ProjectedMetricRatio float64 `json:"projectedMetricRatio,omitempty" yaml:"projectedMetricRatio,omitempty"`
}

// SimulationExtendedOptions configures extended simulation with time-series
// projection and additional parameter overrides.
type SimulationExtendedOptions struct {
	DurationSeconds int32 `json:"durationSeconds" yaml:"durationSeconds"`
	StepSeconds     int32 `json:"stepSeconds" yaml:"stepSeconds"`
}

// MetricDecisionTrace holds a comprehensive per-metric analysis explaining
// which metric drove the HPA scaling decision and why.
type MetricDecisionTrace struct {
	// Metrics holds the per-metric analysis for every current metric.
	Metrics []MetricTraceEntry `json:"metrics" yaml:"metrics"`
	// Winner is the name of the metric estimated to have driven the decision.
	Winner string `json:"winner,omitempty" yaml:"winner,omitempty"`
	// WinnerConfidence is the confidence in the winner determination.
	WinnerConfidence Confidence `json:"winnerConfidence,omitempty" yaml:"winnerConfidence,omitempty"`
	// SelectPolicy is the resolved selectPolicy (Max, Min, Disabled) for the
	// direction that won (scaleUp or scaleDown).
	SelectPolicy string `json:"selectPolicy,omitempty" yaml:"selectPolicy,omitempty"`
	// StabilizationEffect describes how the stabilization window affected the decision.
	StabilizationEffect *StabilizationEffect `json:"stabilizationEffect,omitempty" yaml:"stabilizationEffect,omitempty"`
	// ToleranceEffect describes whether tolerance suppressed scaling.
	ToleranceEffect *ToleranceEffect `json:"toleranceEffect,omitempty" yaml:"toleranceEffect,omitempty"`
	// Summary is a human-readable one-line explanation of the decision.
	Summary string `json:"summary" yaml:"summary"`
}

// MetricTraceEntry holds the analysis for a single metric in the decision trace.
type MetricTraceEntry struct {
	// Name is the metric display name (e.g. "cpu", "http_requests").
	Name string `json:"name" yaml:"name"`
	// Type is the metric source type (Resource, External, Pods, Object, ContainerResource).
	Type string `json:"type" yaml:"type"`
	// Ratio is the current/target ratio. nil if unavailable.
	Ratio *float64 `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	// DistanceFromTarget is |ratio - 1.0|. 0 means at target.
	DistanceFromTarget float64 `json:"distanceFromTarget,omitempty" yaml:"distanceFromTarget,omitempty"`
	// ReplicaImpact estimates how many replicas this metric would add/remove.
	ReplicaImpact float64 `json:"replicaImpact,omitempty" yaml:"replicaImpact,omitempty"`
	// DesiredDirection indicates whether this metric wants scale-up, scale-down, or no-change.
	DesiredDirection string `json:"desiredDirection" yaml:"desiredDirection"` // "up", "down", "none"
	// WithinTolerance indicates whether the metric is within the tolerance band.
	WithinTolerance bool `json:"withinTolerance,omitempty" yaml:"withinTolerance,omitempty"`
	// Note is a human-readable explanation for this metric's state.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// DecisionTrace is a readable, step-by-step explanation of the visible HPA
// decision path. It intentionally avoids reimplementing the controller and
// marks inferred steps with confidence.
type DecisionTrace struct {
	Namespace           string                `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name                string                `json:"name,omitempty" yaml:"name,omitempty"`
	CurrentReplicas     int32                 `json:"currentReplicas" yaml:"currentReplicas"`
	VisibleDesired      int32                 `json:"visibleDesiredReplicas" yaml:"visibleDesiredReplicas"`
	EstimatedRawDesired int32                 `json:"estimatedRawDesiredReplicas,omitempty" yaml:"estimatedRawDesiredReplicas,omitempty"`
	MaxReplicas         int32                 `json:"maxReplicas" yaml:"maxReplicas"`
	MinReplicas         int32                 `json:"minReplicas" yaml:"minReplicas"`
	Metrics             []DecisionTraceMetric `json:"metrics,omitempty" yaml:"metrics,omitempty"`
	LimitCheck          string                `json:"limitCheck,omitempty" yaml:"limitCheck,omitempty"`
	Stabilization       string                `json:"stabilization,omitempty" yaml:"stabilization,omitempty"`
	FinalInterpretation string                `json:"finalInterpretation" yaml:"finalInterpretation"`
	Confidence          Confidence            `json:"confidence" yaml:"confidence"`
	Notes               []string              `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// DecisionTraceMetric describes one metric's visible ratio and estimated
// desired replica count.
type DecisionTraceMetric struct {
	Name       string     `json:"name" yaml:"name"`
	Type       string     `json:"type" yaml:"type"`
	Current    string     `json:"current,omitempty" yaml:"current,omitempty"`
	Target     string     `json:"target,omitempty" yaml:"target,omitempty"`
	Ratio      *float64   `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	RawDesired *int32     `json:"rawDesiredReplicas,omitempty" yaml:"rawDesiredReplicas,omitempty"`
	Formula    string     `json:"formula,omitempty" yaml:"formula,omitempty"`
	Direction  string     `json:"direction,omitempty" yaml:"direction,omitempty"`
	Confidence Confidence `json:"confidence" yaml:"confidence"`
}

// StabilizationEffect describes how the stabilization window affected the decision.
type StabilizationEffect struct {
	// WindowSeconds is the configured stabilization window duration.
	WindowSeconds int32 `json:"windowSeconds,omitempty" yaml:"windowSeconds,omitempty"`
	// RemainingSeconds estimates how many seconds remain in the window.
	RemainingSeconds *int64 `json:"remainingSeconds,omitempty" yaml:"remainingSeconds,omitempty"`
	// SuppressedScaleDown indicates whether scale-down was suppressed by the window.
	SuppressedScaleDown bool `json:"suppressedScaleDown,omitempty" yaml:"suppressedScaleDown,omitempty"`
	// Note is a human-readable explanation.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// ToleranceEffect describes whether tolerance suppressed scaling.
type ToleranceEffect struct {
	// DefaultTolerance is the Kubernetes default tolerance (0.1).
	DefaultTolerance float64 `json:"defaultTolerance" yaml:"defaultTolerance"`
	// ConfiguredTolerance is the explicitly configured tolerance, if any.
	ConfiguredTolerance *float64 `json:"configuredTolerance,omitempty" yaml:"configuredTolerance,omitempty"`
	// SuppressedMetrics lists metric names whose scaling was suppressed by tolerance.
	SuppressedMetrics []string `json:"suppressedMetrics,omitempty" yaml:"suppressedMetrics,omitempty"`
	// Note is a human-readable explanation.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// MetricSimulation holds the result of simulating a metric value change.
type MetricSimulation struct {
	// MetricName is the name of the simulated metric.
	MetricName string `json:"metricName" yaml:"metricName"`
	// OriginalValue is the current metric value before simulation.
	OriginalValue string `json:"originalValue" yaml:"originalValue"`
	// SimulatedValue is the simulated metric value.
	SimulatedValue string `json:"simulatedValue" yaml:"simulatedValue"`
	// ProjectedRatio is the estimated ratio after simulation.
	ProjectedRatio *float64 `json:"projectedRatio,omitempty" yaml:"projectedRatio,omitempty"`
	// ProjectedReplicas is the estimated desired replica count.
	ProjectedReplicas int32 `json:"projectedReplicas" yaml:"projectedReplicas"`
	// ToleranceImpact describes whether tolerance would suppress this change.
	ToleranceImpact string `json:"toleranceImpact,omitempty" yaml:"toleranceImpact,omitempty"`
	// StabilizationImpact describes whether stabilization would delay this change.
	StabilizationImpact string `json:"stabilizationImpact,omitempty" yaml:"stabilizationImpact,omitempty"`
	// RiskAssessment for this specific metric simulation.
	RiskAssessment string `json:"riskAssessment,omitempty" yaml:"riskAssessment,omitempty"`
}

// AuditSeverity represents the severity of an audit finding.
type AuditSeverity string

const (
	// AuditCritical indicates a critical finding requiring immediate attention.
	AuditCritical AuditSeverity = "critical"
	// AuditWarning indicates a finding that warrants operator attention.
	AuditWarning AuditSeverity = "warning"
	// AuditInfo indicates an informational finding or best-practice suggestion.
	AuditInfo AuditSeverity = "info"
)

// AuditFinding represents a single best-practice audit finding.
type AuditFinding struct {
	// ID is a unique identifier for the audit rule that produced this finding.
	ID string `json:"id" yaml:"id"`
	// Title is a short description of the finding.
	Title string `json:"title" yaml:"title"`
	// Description provides detailed context about the finding.
	Description string `json:"description" yaml:"description"`
	// Severity is the severity level: critical, warning, or info.
	Severity AuditSeverity `json:"severity" yaml:"severity"`
	// Category groups related findings (e.g. "stabilization", "replica-range").
	Category string `json:"category" yaml:"category"`
	// Current shows the current configuration value.
	Current string `json:"current,omitempty" yaml:"current,omitempty"`
	// Recommended shows the recommended configuration value.
	Recommended string `json:"recommended,omitempty" yaml:"recommended,omitempty"`
	// Patch is a JSON merge patch to fix the finding, if applicable.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Command is the kubectl command to apply the patch.
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// Risk indicates the risk level of applying the patch.
	Risk string `json:"risk,omitempty" yaml:"risk,omitempty"`
	// References lists URLs or docs for further reading.
	References []string `json:"references,omitempty" yaml:"references,omitempty"`
}

// AuditProfile represents a workload profile that adjusts audit rule thresholds.
type AuditProfile string

const (
	// ProfileLatency optimizes for low-latency workloads: fast scale-up, slow scale-down.
	ProfileLatency AuditProfile = "latency"
	// ProfileCost optimizes for cost efficiency: low minReplicas, aggressive scale-down.
	ProfileCost AuditProfile = "cost"
	// ProfileBatch is for batch workloads: high CPU tolerance, no urgent scale-up.
	ProfileBatch AuditProfile = "batch"
	// ProfileKEDA is for KEDA-managed workloads: scale-to-zero, trigger/cooldown focus.
	ProfileKEDA AuditProfile = "keda"
	// ProfileCritical is for critical workloads: maxReplicas headroom, capacity checks.
	ProfileCritical AuditProfile = "critical"
)

// AuditReport holds the complete audit result for an HPA.
type AuditReport struct {
	// Namespace is the HPA namespace.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// Score is the compliance score from 0 (worst) to 100 (fully compliant).
	Score int `json:"score" yaml:"score"`
	// Findings lists all audit findings.
	Findings []AuditFinding `json:"findings" yaml:"findings"`
	// Summary is a human-readable one-line summary of the audit.
	Summary string `json:"summary" yaml:"summary"`
	// Profile indicates the workload profile used for threshold adjustments, if any.
	Profile AuditProfile `json:"profile,omitempty" yaml:"profile,omitempty"`
}

// CapacityContext holds infrastructure capacity analysis for the HPA scale target.
type CapacityContext struct {
	PendingPods      []PendingPodInfo  `json:"pendingPods,omitempty" yaml:"pendingPods,omitempty"`
	QuotaConstraints []QuotaConstraint `json:"quotaConstraints,omitempty" yaml:"quotaConstraints,omitempty"`
	PDBInterference  []PDBInterference `json:"pdbInterference,omitempty" yaml:"pdbInterference,omitempty"`
	NodeHints        []string          `json:"nodeHints,omitempty" yaml:"nodeHints,omitempty"`
}

// CapacityHeadroom estimates the extra pod resources required to reach
// maxReplicas and summarizes visible cluster headroom signals.
type CapacityHeadroom struct {
	HPAName                    string   `json:"hpaName,omitempty" yaml:"hpaName,omitempty"`
	Target                     string   `json:"target,omitempty" yaml:"target,omitempty"`
	MaxReplicas                int32    `json:"maxReplicas" yaml:"maxReplicas"`
	CurrentDesired             int32    `json:"currentDesired" yaml:"currentDesired"`
	AdditionalReplicasToMax    int32    `json:"additionalReplicasToMax" yaml:"additionalReplicasToMax"`
	PodRequestCPU              string   `json:"podRequestCpu,omitempty" yaml:"podRequestCpu,omitempty"`
	PodRequestMemory           string   `json:"podRequestMemory,omitempty" yaml:"podRequestMemory,omitempty"`
	AdditionalCPUToMax         string   `json:"additionalCpuToMax,omitempty" yaml:"additionalCpuToMax,omitempty"`
	AdditionalMemoryToMax      string   `json:"additionalMemoryToMax,omitempty" yaml:"additionalMemoryToMax,omitempty"`
	ClusterSchedulableHeadroom string   `json:"clusterSchedulableHeadroom" yaml:"clusterSchedulableHeadroom"`
	Risk                       string   `json:"risk" yaml:"risk"`
	Evidence                   []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
}

// ReadinessImpact summarizes visible pod readiness and metrics gaps that may
// make HPA controller decisions differ from status.currentMetrics.
type ReadinessImpact struct {
	LikelyAffected          bool     `json:"likelyAffected" yaml:"likelyAffected"`
	TotalPods               int32    `json:"totalPods" yaml:"totalPods"`
	NotYetReadyPods         int32    `json:"notYetReadyPods" yaml:"notYetReadyPods"`
	MissingMetricPods       int32    `json:"missingMetricPods,omitempty" yaml:"missingMetricPods,omitempty"`
	InitialReadinessDelay   string   `json:"initialReadinessDelay" yaml:"initialReadinessDelay"`
	CPUInitializationPeriod string   `json:"cpuInitializationPeriod" yaml:"cpuInitializationPeriod"`
	PossibleEffects         []string `json:"possibleEffects,omitempty" yaml:"possibleEffects,omitempty"`
	Evidence                []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	NextChecks              []string `json:"nextChecks,omitempty" yaml:"nextChecks,omitempty"`
}

// ---------------------------------------------------------------------------
// Readiness Doctor types (readiness doctor command)
// ---------------------------------------------------------------------------

// ReadinessDoctorReport holds the focused readiness diagnostic for an HPA
// scale target, covering pod age distribution, probe configuration, CPU
// initialization window impact, and metric exclusion estimates.
type ReadinessDoctorReport struct {
	Namespace          string                    `json:"namespace" yaml:"namespace"`
	Name               string                    `json:"name" yaml:"name"`
	Target             string                    `json:"target" yaml:"target"`
	Summary            string                    `json:"summary" yaml:"summary"`
	PodAgeDistribution ReadinessPodAgeDistribution `json:"podAgeDistribution" yaml:"podAgeDistribution"`
	ProbeAnalysis      ReadinessProbeAnalysis     `json:"probeAnalysis" yaml:"probeAnalysis"`
	InitializationImpact ReadinessInitImpact       `json:"initializationImpact" yaml:"initializationImpact"`
	ExclusionEstimate  ReadinessExclusionEstimate  `json:"exclusionEstimate" yaml:"exclusionEstimate"`
	Recommendations    []string                   `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	NextChecks         []string                   `json:"nextChecks,omitempty" yaml:"nextChecks,omitempty"`
}

// ReadinessPodAgeDistribution summarizes pod age across the scale target.
type ReadinessPodAgeDistribution struct {
	TotalPods       int32 `json:"totalPods" yaml:"totalPods"`
	YoungPods       int32 `json:"youngPods" yaml:"youngPods"`
	MaturePods      int32 `json:"maturePods" yaml:"maturePods"`
	ReadyYoungPods  int32 `json:"readyYoungPods" yaml:"readyYoungPods"`
	NotReadyYoungPods int32 `json:"notReadyYoungPods" yaml:"notReadyYoungPods"`
}

// ReadinessProbeAnalysis evaluates probe configuration on the pod template.
type ReadinessProbeAnalysis struct {
	HasStartupProbe          bool     `json:"hasStartupProbe" yaml:"hasStartupProbe"`
	HasReadinessProbe        bool     `json:"hasReadinessProbe" yaml:"hasReadinessProbe"`
	ReadinessInitialDelaySec int32    `json:"readinessInitialDelaySec" yaml:"readinessInitialDelaySec"`
	StartupMaxDelaySec       int32    `json:"startupMaxDelaySec,omitempty" yaml:"startupMaxDelaySec,omitempty"`
	Assessment               string   `json:"assessment" yaml:"assessment"`
	Warnings                 []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// ReadinessInitImpact estimates how the CPU initialization window affects HPA.
type ReadinessInitImpact struct {
	CPUInitPeriodSeconds  int32  `json:"cpuInitPeriodSeconds" yaml:"cpuInitPeriodSeconds"`
	InitialReadinessDelay int32  `json:"initialReadinessDelaySeconds" yaml:"initialReadinessDelaySeconds"`
	EstimatedExcludedPods int32  `json:"estimatedExcludedPods" yaml:"estimatedExcludedPods"`
	ImpactDescription     string `json:"impactDescription" yaml:"impactDescription"`
}

// ReadinessExclusionEstimate estimates pods excluded from HPA metric calculation.
type ReadinessExclusionEstimate struct {
	NotReadyPods           int32  `json:"notReadyPods" yaml:"notReadyPods"`
	MissingMetricPods      int32  `json:"missingMetricPods" yaml:"missingMetricPods"`
	EstimatedExcludedCount int32  `json:"estimatedExcludedCount" yaml:"estimatedExcludedCount"`
	Explanation            string `json:"explanation" yaml:"explanation"`
}

// ReadinessDoctorInput is assembled by the cmd layer from Kubernetes API data.
type ReadinessDoctorInput struct {
	Namespace            string
	HPAName              string
	Target               string
	PodDetails           []ReadinessDoctorPod
	HasStartupProbe      bool
	HasReadinessProbe    bool
	ReadinessInitialDelay int32
	StartupMaxDelay      int32
	CPUInitPeriodSeconds int32
	InitialReadinessDelay int32
	MissingMetricPods    int32
}

// ReadinessDoctorPod describes a single pod for readiness analysis.
type ReadinessDoctorPod struct {
	Name       string
	Ready      bool
	AgeSeconds int64
}

// RolloutDiagnosis summarizes rollout state that can make an HPA look broken
// even when the HPA decision itself is reasonable.
type RolloutDiagnosis struct {
	Kind                string   `json:"kind" yaml:"kind"`
	Name                string   `json:"name" yaml:"name"`
	DesiredReplicas     int32    `json:"desiredReplicas" yaml:"desiredReplicas"`
	UpdatedReplicas     int32    `json:"updatedReplicas,omitempty" yaml:"updatedReplicas,omitempty"`
	ReadyReplicas       int32    `json:"readyReplicas,omitempty" yaml:"readyReplicas,omitempty"`
	AvailableReplicas   int32    `json:"availableReplicas,omitempty" yaml:"availableReplicas,omitempty"`
	UnavailableReplicas int32    `json:"unavailableReplicas,omitempty" yaml:"unavailableReplicas,omitempty"`
	InProgress          bool     `json:"inProgress" yaml:"inProgress"`
	Reason              string   `json:"reason,omitempty" yaml:"reason,omitempty"`
	Conditions          []string `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	PodIssues           []string `json:"podIssues,omitempty" yaml:"podIssues,omitempty"`
	NextActions         []string `json:"nextActions,omitempty" yaml:"nextActions,omitempty"`
}

// ControllerProfile describes HPA controller-manager settings that affect
// controller decisions. Values may be observed from kube-controller-manager
// arguments or default assumptions.
type ControllerProfile struct {
	Source                  string   `json:"source" yaml:"source"`
	SyncPeriod              string   `json:"syncPeriod" yaml:"syncPeriod"`
	DownscaleStabilization  string   `json:"downscaleStabilization" yaml:"downscaleStabilization"`
	InitialReadinessDelay   string   `json:"initialReadinessDelay" yaml:"initialReadinessDelay"`
	CPUInitializationPeriod string   `json:"cpuInitializationPeriod" yaml:"cpuInitializationPeriod"`
	Tolerance               string   `json:"tolerance" yaml:"tolerance"`
	Warnings                []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// DefaultControllerProfile returns the Kubernetes controller-manager default
// HPA timing settings that matter for user-facing interpretation.
func DefaultControllerProfile() ControllerProfile {
	return ControllerProfile{
		Source:                  "defaults",
		SyncPeriod:              "15s",
		DownscaleStabilization:  "5m",
		InitialReadinessDelay:   "30s",
		CPUInitializationPeriod: "5m",
		Tolerance:               "0.1",
	}
}

// ScalePath describes the visible scale-up path from the HPA recommendation
// through the workload, ReplicaSets, pods, and scheduler-facing signals.
type ScalePath struct {
	Steps            []ScalePathStep         `json:"steps" yaml:"steps"`
	BlockingPoint    string                  `json:"blockingPoint,omitempty" yaml:"blockingPoint,omitempty"`
	Evidence         []string                `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	NextActions      []string                `json:"nextActions,omitempty" yaml:"nextActions,omitempty"`
	ProbeWarnings    []string                `json:"probeWarnings,omitempty" yaml:"probeWarnings,omitempty"`
	SchedulerInfo    *ScalePathSchedulerInfo `json:"schedulerInfo,omitempty" yaml:"schedulerInfo,omitempty"`
	QuotaChecks      []ScalePathQuotaCheck   `json:"quotaChecks,omitempty" yaml:"quotaChecks,omitempty"`
	AutoscalerEvents []string                `json:"autoscalerEvents,omitempty" yaml:"autoscalerEvents,omitempty"`
}

// ScalePathStep is one hop in the HPA-to-pod scaling path.
type ScalePathStep struct {
	Name    string `json:"name" yaml:"name"`
	Summary string `json:"summary" yaml:"summary"`
}

// ScalePathTarget is the observed HPA scale target.
type ScalePathTarget struct {
	Kind            string
	Name            string
	DesiredReplicas int32
	CurrentReplicas int32
	ReadyReplicas   int32
}

// ScalePathReplicaSet is a ReplicaSet participating in the target path.
type ScalePathReplicaSet struct {
	Name            string
	DesiredReplicas int32
	CurrentReplicas int32
	ReadyReplicas   int32
}

// ScalePathPod is the pod-level state used by scale path analysis.
type ScalePathPod struct {
	Name          string
	Phase         string
	Ready         bool
	Unschedulable bool
	Reasons       []string
}

// ProbeInfo describes a probe (readiness or startup) on the pod template.
type ProbeInfo struct {
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty" yaml:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int32 `json:"periodSeconds,omitempty" yaml:"periodSeconds,omitempty"`
	TimeoutSeconds      int32 `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
	FailureThreshold    int32 `json:"failureThreshold,omitempty" yaml:"failureThreshold,omitempty"`
	SuccessThreshold    int32 `json:"successThreshold,omitempty" yaml:"successThreshold,omitempty"`
}

// ScalePathPodTemplate captures the pod template configuration relevant to
// scale-path analysis (probes, scheduling constraints).
type ScalePathPodTemplate struct {
	ReadinessProbe  *ProbeInfo        `json:"readinessProbe,omitempty" yaml:"readinessProbe,omitempty"`
	StartupProbe    *ProbeInfo        `json:"startupProbe,omitempty" yaml:"startupProbe,omitempty"`
	NodeSelector    map[string]string `json:"nodeSelector,omitempty" yaml:"nodeSelector,omitempty"`
	Tolerations     []string          `json:"tolerations,omitempty" yaml:"tolerations,omitempty"`
	AffinitySummary string            `json:"affinitySummary,omitempty" yaml:"affinitySummary,omitempty"`
	TopologySpread  []string          `json:"topologySpread,omitempty" yaml:"topologySpread,omitempty"`
}

// ScalePathSchedulerInfo describes scheduling constraints that may affect
// pod placement during scale-up.
type ScalePathSchedulerInfo struct {
	TaintConflicts            []string `json:"taintConflicts,omitempty" yaml:"taintConflicts,omitempty"`
	NodeSelectorLabels        int      `json:"nodeSelectorLabels,omitempty" yaml:"nodeSelectorLabels,omitempty"`
	AffinityConstraints       []string `json:"affinityConstraints,omitempty" yaml:"affinityConstraints,omitempty"`
	TopologySpreadConstraints []string `json:"topologySpreadConstraints,omitempty" yaml:"topologySpreadConstraints,omitempty"`
	Warning                   string   `json:"warning,omitempty" yaml:"warning,omitempty"`
}

// ScalePathQuotaCheck describes a ResourceQuota that may block scale-up.
type ScalePathQuotaCheck struct {
	Name     string `json:"name" yaml:"name"`
	Resource string `json:"resource" yaml:"resource"`
	Used     string `json:"used" yaml:"used"`
	Hard     string `json:"hard" yaml:"hard"`
	Blocking bool   `json:"blocking" yaml:"blocking"`
}

// ScalePathInput contains the observable Kubernetes API signals used to build
// a scale path. It intentionally excludes controller-internal calculations.
type ScalePathInput struct {
	Target           *ScalePathTarget
	ReplicaSets      []ScalePathReplicaSet
	Pods             []ScalePathPod
	Events           []Event
	PodTemplate      *ScalePathPodTemplate
	ResourceQuotas   []ScalePathQuotaCheck
	AutoscalerEvents []string
	NotReadyPods     []ScalePathPod
}

// PendingPodInfo describes a pending pod and its scheduling constraints.
type PendingPodInfo struct {
	Name          string   `json:"name" yaml:"name"`
	Phase         string   `json:"phase" yaml:"phase"`
	Unschedulable bool     `json:"unschedulable" yaml:"unschedulable"`
	Reasons       []string `json:"reasons,omitempty" yaml:"reasons,omitempty"`
}

// QuotaConstraint describes a ResourceQuota that limits the scale target.
type QuotaConstraint struct {
	Name     string `json:"name" yaml:"name"`
	Resource string `json:"resource" yaml:"resource"`
	Used     string `json:"used" yaml:"used"`
	Hard     string `json:"hard" yaml:"hard"`
	Message  string `json:"message" yaml:"message"`
}

// PDBInterference describes a PodDisruptionBudget that may interfere with scaling.
type PDBInterference struct {
	Name           string `json:"name" yaml:"name"`
	MinAvailable   string `json:"minAvailable,omitempty" yaml:"minAvailable,omitempty"`
	MaxUnavailable string `json:"maxUnavailable,omitempty" yaml:"maxUnavailable,omitempty"`
	Disruption     string `json:"disruption" yaml:"disruption"`
}

// TimelineSnapshot captures the state of an HPA at a single point in time.
type TimelineSnapshot struct {
	Timestamp      time.Time   `json:"timestamp" yaml:"timestamp"`
	Current        int32       `json:"currentReplicas" yaml:"currentReplicas"`
	Desired        int32       `json:"desiredReplicas" yaml:"desiredReplicas"`
	Health         string      `json:"health" yaml:"health"`
	HealthScore    int         `json:"healthScore" yaml:"healthScore"`
	TopMetric      string      `json:"topMetric" yaml:"topMetric"`
	Conditions     []Condition `json:"conditions" yaml:"conditions"`
	Summary        string      `json:"summary" yaml:"summary"`
	Interpretation []string    `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	Events         []Event     `json:"events,omitempty" yaml:"events,omitempty"`
}

// TimelineTrace holds a sequence of snapshots for a single HPA.
type TimelineTrace struct {
	HPAName   string             `json:"hpaName" yaml:"hpaName"`
	Namespace string             `json:"namespace" yaml:"namespace"`
	Start     time.Time          `json:"start" yaml:"start"`
	End       time.Time          `json:"end,omitempty" yaml:"end,omitempty"`
	Interval  time.Duration      `json:"interval" yaml:"interval"`
	Snapshots []TimelineSnapshot `json:"snapshots" yaml:"snapshots"`
}

// RetrospectiveEntry represents a single estimated scaling decision event
// reconstructed from Kubernetes events and HPA status signals.
type RetrospectiveEntry struct {
	Timestamp  time.Time `json:"timestamp" yaml:"timestamp"`
	Category   string    `json:"category" yaml:"category"`
	Message    string    `json:"message" yaml:"message"`
	Source     string    `json:"source" yaml:"source"`
	Confidence string    `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}

// RetrospectiveTimeline holds the result of reconstructing past scaling decisions
// from Kubernetes events and current HPA status.
type RetrospectiveTimeline struct {
	HPAName    string               `json:"hpaName" yaml:"hpaName"`
	Namespace  string               `json:"namespace" yaml:"namespace"`
	Since      time.Time            `json:"since" yaml:"since"`
	Until      time.Time            `json:"until" yaml:"until"`
	Entries    []RetrospectiveEntry `json:"entries" yaml:"entries"`
	Disclaimer string               `json:"disclaimer" yaml:"disclaimer"`
	Warnings   []string             `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// BlockerSeverity classifies how significantly a finding blocks scale-out.
type BlockerSeverity string

const (
	// BlockerHigh indicates a definite scale-out blocker requiring immediate attention.
	BlockerHigh BlockerSeverity = "HIGH"
	// BlockerMedium indicates a likely blocker that warrants investigation.
	BlockerMedium BlockerSeverity = "MEDIUM"
	// BlockerInfo indicates an informational finding with no blocking effect.
	BlockerInfo BlockerSeverity = "INFO"
)

// BlockerFinding represents a single detected scale-out blocker.
type BlockerFinding struct {
	// ID is a unique identifier for the detection rule that produced this finding.
	ID string `json:"id" yaml:"id"`
	// Severity is the blocker severity: HIGH, MEDIUM, or INFO.
	Severity BlockerSeverity `json:"severity" yaml:"severity"`
	// Category groups related findings: "scheduling", "quota", "application", "readiness", "info".
	Category string `json:"category" yaml:"category"`
	// Message is a human-readable description of the blocker.
	Message string `json:"message" yaml:"message"`
	// Detail provides additional context about the blocker.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
	// NextCommand suggests a kubectl command to investigate further.
	NextCommand string `json:"nextCommand,omitempty" yaml:"nextCommand,omitempty"`
}

// BlockerReport holds the complete scale-out blocker analysis for an HPA.
type BlockerReport struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA resource name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// HPAWantsScale is true when desiredReplicas > currentReplicas.
	HPAWantsScale bool `json:"hpaWantsScale" yaml:"hpaWantsScale"`
	// DesiredReplicas is the desired replica count from HPA status.
	DesiredReplicas int32 `json:"desiredReplicas" yaml:"desiredReplicas"`
	// ReadyReplicas is the count of ready pods on the scale target.
	ReadyReplicas int32 `json:"readyReplicas" yaml:"readyReplicas"`
	// Summary is a one-line summary of the blocker analysis.
	Summary string `json:"summary" yaml:"summary"`
	// Blockers lists all detected blocker findings sorted by severity.
	Blockers []BlockerFinding `json:"blockers" yaml:"blockers"`
	// Interpretation is a human-readable explanation of the overall situation.
	Interpretation string `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	// NextCommands lists suggested kubectl commands for further investigation.
	NextCommands []string `json:"nextCommands" yaml:"nextCommands"`
}

// ContainerStatusSummary holds container-level status for blocker detection.
type ContainerStatusSummary struct {
	// Pod is the pod name.
	Pod string `json:"pod" yaml:"pod"`
	// Container is the container name.
	Container string `json:"container" yaml:"container"`
	// Waiting is true when the container is in a waiting state.
	Waiting bool `json:"waiting" yaml:"waiting"`
	// WaitingReason is the reason for the waiting state (e.g. ImagePullBackOff, CrashLoopBackOff).
	WaitingReason string `json:"waitingReason,omitempty" yaml:"waitingReason,omitempty"`
	// RestartCount is the number of container restarts.
	RestartCount int32 `json:"restartCount" yaml:"restartCount"`
}

// NodeCapacitySummary holds node-level capacity information for deep analysis.
type NodeCapacitySummary struct {
	// TotalNodes is the total number of nodes in the cluster.
	TotalNodes int32 `json:"totalNodes" yaml:"totalNodes"`
	// AllocCPU is the sum of allocatable CPU across all nodes.
	AllocCPU string `json:"allocatableCpu,omitempty" yaml:"allocatableCpu,omitempty"`
	// AllocMemory is the sum of allocatable memory across all nodes.
	AllocMemory string `json:"allocatableMemory,omitempty" yaml:"allocatableMemory,omitempty"`
	// TaintedNodes is the count of nodes with at least one taint that has NoSchedule or NoExecute effect.
	TaintedNodes int32 `json:"taintedNodes,omitempty" yaml:"taintedNodes,omitempty"`
	// Hints provides actionable hints based on node capacity analysis.
	Hints []string `json:"hints,omitempty" yaml:"hints,omitempty"`
}

// BlockerInput aggregates all observable signals for scale-out blocker analysis.
// The cmd layer assembles this from multiple kube fetchers, keeping the core
// analysis in pkg/hpa free of Kubernetes API dependencies.
type BlockerInput struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string
	// DesiredReplicas is the HPA desired replica count.
	DesiredReplicas int32
	// CurrentReplicas is the HPA current replica count.
	CurrentReplicas int32
	// MinReplicas is the HPA minimum replica count.
	MinReplicas int32
	// MaxReplicas is the HPA maximum replica count.
	MaxReplicas int32
	// TargetReadyReplicas is the ready replica count from the scale target.
	TargetReadyReplicas int32
	// TargetDesiredReplicas is the desired replica count from the scale target.
	TargetDesiredReplicas int32
	// PendingPods lists pods in Pending phase with scheduling details.
	PendingPods []BlockerPodInfo
	// ReadyPods is the count of pods in Running/Ready state.
	ReadyPods int32
	// TotalPods is the total number of pods for the scale target.
	TotalPods int32
	// ContainerStatuses holds container-level status for failure detection.
	ContainerStatuses []ContainerStatusSummary
	// FailedSchedulingEvents lists events with reason FailedScheduling.
	FailedSchedulingEvents []string
	// Quotas lists ResourceQuota constraints near their limits.
	Quotas []BlockerQuotaInfo
	// NodeCapacity holds node-level capacity (only populated with --capacity-deep).
	NodeCapacity *NodeCapacitySummary
	// ScalingActive indicates whether the HPA ScalingActive condition is True.
	ScalingActive bool
}

// BlockerPodInfo holds pod-level information relevant to blocker detection.
type BlockerPodInfo struct {
	// Name is the pod name.
	Name string
	// Phase is the pod phase (Pending, Running, etc.).
	Phase string
	// Unschedulable is true when the pod has an unschedulable condition.
	Unschedulable bool
	// Reasons lists scheduling failure reasons from pod conditions.
	Reasons []string
}

// BlockerQuotaInfo holds ResourceQuota usage information for blocker detection.
type BlockerQuotaInfo struct {
	// Name is the ResourceQuota name.
	Name string
	// Resource is the resource name (e.g. requests.cpu, requests.memory).
	Resource string
	// Used is the current usage value as a string.
	Used string
	// Hard is the hard limit as a string.
	Hard string
	// Ratio is the usage ratio (used/hard), 0 if hard is zero.
	Ratio float64
}

// ---------------------------------------------------------------------------
// Capacity Plan types
// ---------------------------------------------------------------------------

// CapacityPlanInput aggregates all observable signals needed to produce a
// capacity plan. The cmd layer assembles this from multiple kube fetchers.
type CapacityPlanInput struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string
	// HPAName is the HPA resource name.
	HPAName string
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string
	// CurrentReplicas is the current replica count from HPA status.
	CurrentReplicas int32
	// MaxReplicas is the current maxReplicas from HPA spec.
	MaxReplicas int32
	// TargetMaxReplicas is the proposed new maxReplicas (default: maxReplicas*2, capped at 200).
	TargetMaxReplicas int32

	// ContainerResources holds per-container CPU and memory requests from the
	// scale target's pod template.
	ContainerResources []CapacityContainerResources
	// Quotas holds all ResourceQuota entries (not just near-limit) so the
	// analysis can compute remaining headroom.
	Quotas []CapacityQuotaInfo
	// LimitRanges holds LimitRange min/max constraints for containers and pods.
	LimitRanges []LimitRangeConstraint
	// NodeCapacity holds aggregate node allocatable resources.
	NodeCapacity *NodeCapacitySummary
	// PendingPods lists pods in Pending phase for the scale target.
	PendingPods []PendingPodInfo
	// PDBs lists PodDisruptionBudgets in the namespace.
	PDBs []PDBInterference
	// ClusterAutoscaler is true when Cluster Autoscaler is detected.
	ClusterAutoscaler bool
	// ReadyPods is the count of pods in Running/Ready state.
	ReadyPods int32
}

// CapacityContainerResources holds per-container resource requests for
// capacity projection.
type CapacityContainerResources struct {
	// Name is the container name.
	Name string
	// CPU is the CPU request as a quantity string (e.g. "250m").
	CPU string
	// Memory is the memory request as a quantity string (e.g. "512Mi").
	Memory string
}

// CapacityQuotaInfo holds full ResourceQuota usage so the capacity plan can
// compute remaining headroom.
type CapacityQuotaInfo struct {
	// Name is the ResourceQuota name.
	Name string
	// Resource is the resource type (e.g. "requests.cpu", "requests.memory").
	Resource string
	// Used is the current usage value as a string.
	Used string
	// Hard is the hard limit as a string.
	Hard string
}

// LimitRangeConstraint describes a LimitRange min/max that applies to pods or
// containers.
type LimitRangeConstraint struct {
	// Name is the LimitRange name.
	Name string
	// Type is the constraint target: "Container" or "Pod".
	Type string
	// Resource is the resource type (e.g. "cpu", "memory").
	Resource string
	// Min is the minimum allowed value (empty if no minimum).
	Min string
	// Max is the maximum allowed value (empty if no maximum).
	Max string
}

// CapacityPlan holds the result of a capacity plan analysis, diagnosing
// whether it is safe to raise HPA maxReplicas.
type CapacityPlan struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA resource name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`

	// Current state.
	CurrentReplicas int32  `json:"currentReplicas" yaml:"currentReplicas"`
	MaxReplicas     int32  `json:"maxReplicas" yaml:"maxReplicas"`
	Issue           string `json:"issue" yaml:"issue"`

	// Projected state if maxReplicas is raised.
	TargetMaxReplicas int32  `json:"targetMaxReplicas" yaml:"targetMaxReplicas"`
	AdditionalPods    int32  `json:"additionalPods" yaml:"additionalPods"`
	RequiredCPU       string `json:"requiredCpu" yaml:"requiredCpu"`
	RequiredMemory    string `json:"requiredMemory" yaml:"requiredMemory"`

	// Checks lists individual check results.
	Checks []CapacityCheckResult `json:"checks" yaml:"checks"`

	// Recommendation is the overall recommendation text.
	Recommendation string `json:"recommendation" yaml:"recommendation"`
	// Safe is true when all checks pass.
	Safe bool `json:"safe" yaml:"safe"`
	// SchedulableNow estimates how many additional pods can be scheduled
	// with current cluster resources. Zero means no headroom.
	SchedulableNow int32 `json:"schedulableNow,omitempty" yaml:"schedulableNow,omitempty"`
	// NodeAutoscalerRequired is true when node autoscaling is needed to
	// accommodate the projected maxReplicas.
	NodeAutoscalerRequired bool `json:"nodeAutoscalerRequired" yaml:"nodeAutoscalerRequired"`
	// DryRunCommand suggests a kubectl command for dry-run testing.
	DryRunCommand string `json:"dryRunCommand,omitempty" yaml:"dryRunCommand,omitempty"`
	// NextActions lists concrete remediation steps when Safe is false.
	NextActions []string `json:"nextActions,omitempty" yaml:"nextActions,omitempty"`
}

// CapacityCheckResult holds a single check result for the capacity plan.
type CapacityCheckResult struct {
	// Pass is true when the check succeeds.
	Pass bool `json:"pass" yaml:"pass"`
	// Message describes the check outcome.
	Message string `json:"message" yaml:"message"`
}

// WarmupAnalysis holds the complete warmup analysis result for an HPA that
// recently scaled out but pods are not yet ready.
type WarmupAnalysis struct {
	// Summary is the overall warmup state: "capacity_warming_up",
	// "capacity_ready", "insufficient_data".
	Summary string `json:"summary" yaml:"summary"`
	// EffectiveCapacityRatio is the ratio of ready pods to desired replicas (0.0-1.0).
	EffectiveCapacityRatio float64 `json:"effectiveCapacityRatio" yaml:"effectiveCapacityRatio"`
	// DesiredReplicas is the HPA desired replica count.
	DesiredReplicas int32 `json:"desiredReplicas" yaml:"desiredReplicas"`
	// CurrentReplicas is the HPA current replica count.
	CurrentReplicas int32 `json:"currentReplicas" yaml:"currentReplicas"`
	// ReadyPods is the count of pods in Ready state.
	ReadyPods int32 `json:"readyPods" yaml:"readyPods"`
	// AvailablePods is the count from the workload's availableReplicas status.
	AvailablePods int32 `json:"availablePods" yaml:"availablePods"`
	// AvgTimeToReadySeconds is the average time from pod creation to Ready condition.
	// Zero if no pods have become Ready yet.
	AvgTimeToReadySeconds int64 `json:"avgTimeToReadySeconds" yaml:"avgTimeToReadySeconds"`
	// P95TimeToReadySeconds is the p95 time from pod creation to Ready condition.
	P95TimeToReadySeconds int64 `json:"p95TimeToReadySeconds" yaml:"p95TimeToReadySeconds"`
	// MaxTimeToReadySeconds is the maximum observed time-to-ready.
	MaxTimeToReadySeconds int64 `json:"maxTimeToReadySeconds,omitempty" yaml:"maxTimeToReadySeconds,omitempty"`
	// Bottlenecks lists the detected warmup bottlenecks.
	Bottlenecks []WarmupBottleneck `json:"bottlenecks" yaml:"bottlenecks"`
	// Evidence lists human-readable evidence lines.
	Evidence []string `json:"evidence" yaml:"evidence"`
	// Impact is a human-readable description of the current effective capacity.
	Impact string `json:"impact" yaml:"impact"`
	// RecommendedActions lists actionable suggestions.
	RecommendedActions []string `json:"recommendedActions" yaml:"recommendedActions"`
	// PodDetails holds per-pod warmup status for JSON/YAML consumers.
	PodDetails []WarmupPodDetail `json:"podDetails,omitempty" yaml:"podDetails,omitempty"`
}

// WarmupBottleneck represents a single detected warmup bottleneck.
type WarmupBottleneck struct {
	// Type classifies the bottleneck: "readiness_probe", "image_pull",
	// "scheduling", "startup_probe", "container_crash", "metrics_inactive", "unknown".
	Type string `json:"type" yaml:"type"`
	// Severity is the bottleneck severity.
	Severity Severity `json:"severity" yaml:"severity"`
	// Confidence is the analysis confidence.
	Confidence Confidence `json:"confidence" yaml:"confidence"`
	// Count is how many pods are affected by this bottleneck.
	Count int32 `json:"count" yaml:"count"`
	// Message is a human-readable description.
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// WarmupPodDetail holds per-pod warmup status for structured output.
type WarmupPodDetail struct {
	// Name is the pod name.
	Name string `json:"name" yaml:"name"`
	// AgeSeconds is the pod age in seconds.
	AgeSeconds int64 `json:"ageSeconds" yaml:"ageSeconds"`
	// Ready indicates whether the pod is Ready.
	Ready bool `json:"ready" yaml:"ready"`
	// ContainerState is the primary container state: "running", "waiting", "terminated".
	ContainerState string `json:"containerState,omitempty" yaml:"containerState,omitempty"`
	// WaitingReason is the container waiting reason (e.g., "ImagePullBackOff").
	WaitingReason string `json:"waitingReason,omitempty" yaml:"waitingReason,omitempty"`
	// RestartCount is the number of container restarts.
	RestartCount int32 `json:"restartCount" yaml:"restartCount"`
	// TimeToReadySeconds is the observed time-to-Ready, or 0 if not ready yet.
	TimeToReadySeconds int64 `json:"timeToReadySeconds,omitempty" yaml:"timeToReadySeconds,omitempty"`
}

// WarmupInput aggregates all observable signals for warmup analysis.
// The cmd layer assembles this from multiple kube fetchers, keeping the core
// analysis in pkg/hpa free of Kubernetes API dependencies.
type WarmupInput struct {
	// Namespace is the Kubernetes namespace.
	Namespace string
	// DesiredReplicas is the HPA desired replica count.
	DesiredReplicas int32
	// CurrentReplicas is the HPA current replica count.
	CurrentReplicas int32
	// MinReplicas is the HPA minimum replica count.
	MinReplicas int32
	// MaxReplicas is the HPA maximum replica count.
	MaxReplicas int32
	// ScalingActive indicates whether the HPA ScalingActive condition is True.
	ScalingActive bool
	// ScalingLimited indicates whether the HPA is capped by min/max.
	ScalingLimited bool
	// TargetReadyReplicas is the ready replica count from the scale target.
	TargetReadyReplicas int32
	// TargetAvailableReplicas is the available replica count from the scale target.
	TargetAvailableReplicas int32
	// TargetDesiredReplicas is the desired replica count from the scale target.
	TargetDesiredReplicas int32
	// TotalPods is the total number of pods for the scale target.
	TotalPods int32
	// ReadyPods is the count of pods in Running/Ready state.
	ReadyPods int32
	// PodDetails holds per-pod warmup status information.
	PodDetails []WarmupPodDetail
	// UnhealthyEvents lists pod-level events with reasons indicating warmup issues.
	UnhealthyEvents []WarmupEventInfo
	// ReadinessProbePresent indicates if the pod template has a readinessProbe.
	ReadinessProbePresent bool
	// StartupProbePresent indicates if the pod template has a startupProbe.
	StartupProbePresent bool
	// ReadinessProbeMaxDelaySeconds is the maximum readiness probe delay.
	ReadinessProbeMaxDelaySeconds int32
	// StartupProbeMaxDelaySeconds is the maximum startup probe delay.
	StartupProbeMaxDelaySeconds int32
	// Now is the current time, used for age calculations.
	Now metav1.Time
}

// WarmupEventInfo holds a pod-level event relevant to warmup analysis.
type WarmupEventInfo struct {
	// Reason is the event reason (e.g., "Unhealthy", "FailedScheduling",
	// "BackOff", "ImagePullBackOff").
	Reason string `json:"reason" yaml:"reason"`
	// Count is the number of times this event occurred.
	Count int32 `json:"count" yaml:"count"`
}

// StructuredDecisionTrace holds a comprehensive, schema-versioned decision
// trace that integrates per-metric analysis, tolerance/stabilization effects,
// and the winning metric determination into a single exportable document.
// Populated when --decision-trace-format is set.
type StructuredDecisionTrace struct {
	// SchemaVersion is the version of the structured trace schema.
	SchemaVersion string `json:"schemaVersion" yaml:"schemaVersion"`
	// Namespace is the HPA namespace.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA name.
	Name string `json:"name" yaml:"name"`
	// CurrentReplicas is the current replica count from HPA status.
	CurrentReplicas int32 `json:"currentReplicas" yaml:"currentReplicas"`
	// VisibleDesiredReplicas is the desired replica count from HPA status.
	VisibleDesiredReplicas int32 `json:"visibleDesiredReplicas" yaml:"visibleDesiredReplicas"`
	// EstimatedRawDesired is the estimated raw desired count before clamping.
	EstimatedRawDesired int32 `json:"estimatedRawDesiredReplicas,omitempty" yaml:"estimatedRawDesiredReplicas,omitempty"`
	// MinReplicas is the HPA minimum replica count.
	MinReplicas int32 `json:"minReplicas" yaml:"minReplicas"`
	// MaxReplicas is the HPA maximum replica count.
	MaxReplicas int32 `json:"maxReplicas" yaml:"maxReplicas"`
	// Metrics holds the per-metric trace entries with full evaluation.
	Metrics []StructuredMetricTrace `json:"metrics,omitempty" yaml:"metrics,omitempty"`
	// WinnerMetric is the name of the metric that drove the scaling decision.
	WinnerMetric string `json:"winnerMetric,omitempty" yaml:"winnerMetric,omitempty"`
	// WinnerConfidence is the confidence level of the winner determination.
	WinnerConfidence Confidence `json:"winnerConfidence,omitempty" yaml:"winnerConfidence,omitempty"`
	// LimitClamp describes whether the desired was clamped by min/max.
	LimitClamp string `json:"limitClamp,omitempty" yaml:"limitClamp,omitempty"`
	// ToleranceEffect describes whether tolerance suppressed scaling.
	ToleranceEffect *ToleranceTrace `json:"toleranceEffect,omitempty" yaml:"toleranceEffect,omitempty"`
	// StabilizationEffect describes whether stabilization delayed the decision.
	StabilizationEffect *StabilizationTrace `json:"stabilizationEffect,omitempty" yaml:"stabilizationEffect,omitempty"`
	// DecisionPath lists the ordered evaluation steps that produced the result.
	DecisionPath []DecisionStep `json:"decisionPath,omitempty" yaml:"decisionPath,omitempty"`
	// Summary is a human-readable one-line explanation.
	Summary string `json:"summary" yaml:"summary"`
	// Confidence is the overall confidence of the trace.
	Confidence Confidence `json:"confidence" yaml:"confidence"`
}

// StructuredMetricTrace holds per-metric analysis within the structured decision trace.
type StructuredMetricTrace struct {
	// Name is the metric display name.
	Name string `json:"name" yaml:"name"`
	// Type is the metric source type.
	Type string `json:"type" yaml:"type"`
	// Current is the current metric value as a string.
	Current string `json:"current,omitempty" yaml:"current,omitempty"`
	// Target is the target metric value as a string.
	Target string `json:"target,omitempty" yaml:"target,omitempty"`
	// Ratio is the current/target ratio.
	Ratio *float64 `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	// DistanceFromTarget is |ratio - 1.0|.
	DistanceFromTarget float64 `json:"distanceFromTarget,omitempty" yaml:"distanceFromTarget,omitempty"`
	// DesiredDirection is the desired scaling direction: "up", "down", or "none".
	DesiredDirection string `json:"desiredDirection" yaml:"desiredDirection"`
	// WithinTolerance indicates whether the metric is within the tolerance band.
	WithinTolerance bool `json:"withinTolerance,omitempty" yaml:"withinTolerance,omitempty"`
	// EstimatedDesiredReplicas is the raw desired replica count from this metric alone.
	EstimatedDesiredReplicas *int32 `json:"estimatedDesiredReplicas,omitempty" yaml:"estimatedDesiredReplicas,omitempty"`
	// Formula describes the computation used to derive the desired count.
	Formula string `json:"formula,omitempty" yaml:"formula,omitempty"`
	// Confidence is the confidence level for this metric's estimation.
	Confidence Confidence `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}

// ToleranceTrace describes tolerance impact on the decision within the structured trace.
type ToleranceTrace struct {
	// DefaultTolerance is the Kubernetes default tolerance (0.1).
	DefaultTolerance float64 `json:"defaultTolerance" yaml:"defaultTolerance"`
	// ConfiguredTolerance is the explicitly configured tolerance, if any.
	ConfiguredTolerance *float64 `json:"configuredTolerance,omitempty" yaml:"configuredTolerance,omitempty"`
	// EffectiveTolerance is the tolerance value used for evaluation.
	EffectiveTolerance float64 `json:"effectiveTolerance" yaml:"effectiveTolerance"`
	// SuppressedMetrics lists metrics whose scaling was suppressed by tolerance.
	SuppressedMetrics []string `json:"suppressedMetrics,omitempty" yaml:"suppressedMetrics,omitempty"`
	// Note is a human-readable explanation.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// StabilizationTrace describes stabilization window impact on the decision.
type StabilizationTrace struct {
	// WindowSeconds is the configured stabilization window duration.
	WindowSeconds int32 `json:"windowSeconds,omitempty" yaml:"windowSeconds,omitempty"`
	// Direction is the direction of stabilization: "scaleDown" or "scaleUp".
	Direction string `json:"direction,omitempty" yaml:"direction,omitempty"`
	// RemainingSeconds estimates seconds remaining in the window.
	RemainingSeconds *int64 `json:"remainingSeconds,omitempty" yaml:"remainingSeconds,omitempty"`
	// SuppressedDirection indicates which direction was suppressed.
	SuppressedDirection string `json:"suppressedDirection,omitempty" yaml:"suppressedDirection,omitempty"`
	// Note is a human-readable explanation.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// DecisionStep represents a single step in the HPA decision evaluation path.
type DecisionStep struct {
	// Step is the step number in the evaluation sequence.
	Step int `json:"step" yaml:"step"`
	// Description describes what was evaluated at this step.
	Description string `json:"description" yaml:"description"`
	// Result is the outcome of this evaluation step.
	Result string `json:"result" yaml:"result"`
	// Impact describes how this step affected the final decision.
	Impact string `json:"impact,omitempty" yaml:"impact,omitempty"`
	// Confidence is the confidence level for this step.
	Confidence Confidence `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}

// FlappingPreventionReport holds the result of flapping prevention analysis
// with what-if simulations for different stabilization window values.
type FlappingPreventionReport struct {
	// CurrentWindow is the current stabilization window in seconds.
	CurrentWindow int32 `json:"currentWindow" yaml:"currentWindow"`
	// CurrentDirectionFlips is the number of direction changes observed.
	CurrentDirectionFlips int `json:"currentDirectionFlips" yaml:"currentDirectionFlips"`
	// ObservationWindow is the time range analyzed.
	ObservationWindow string `json:"observationWindow" yaml:"observationWindow"`
	// Recommendations holds the what-if simulation results for different window values.
	Recommendations []FlappingSimulation `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	// Summary is a human-readable summary of the analysis.
	Summary string `json:"summary" yaml:"summary"`
}

// FlappingSimulation holds a single what-if simulation result for a specific
// stabilization window value.
type FlappingSimulation struct {
	// WindowSeconds is the simulated stabilization window duration.
	WindowSeconds int32 `json:"windowSeconds" yaml:"windowSeconds"`
	// EstimatedFlapReduction is the estimated percentage reduction in flapping.
	EstimatedFlapReduction float64 `json:"estimatedFlapReduction" yaml:"estimatedFlapReduction"`
	// EstimatedDirectionFlips is the estimated number of direction flips at this window.
	EstimatedDirectionFlips int `json:"estimatedDirectionFlips" yaml:"estimatedDirectionFlips"`
	// Rationale explains why this window value would reduce flapping.
	Rationale string `json:"rationale" yaml:"rationale"`
	// Patch is the JSON merge patch to apply this window value.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Confidence is the confidence level for this estimate.
	Confidence string `json:"confidence" yaml:"confidence"`
}

// FlappingDiagnosis holds the result of event-based flapping detection with
// root-cause analysis. Unlike FlappingPreventionReport which simulates window
// changes, FlappingDiagnosis identifies *why* flapping occurs and produces
// actionable recommendations with patches.
type FlappingDiagnosis struct {
	// Detected indicates whether flapping was observed.
	Detected bool `json:"detected" yaml:"detected"`
	// Severity classifies the flapping: "LOW", "MEDIUM", "HIGH", "CRITICAL".
	Severity string `json:"severity" yaml:"severity"`
	// Pattern describes the oscillation pattern (e.g. "up-down-up in 3 minutes").
	Pattern string `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	// FlipCount is the number of direction changes observed.
	FlipCount int `json:"flipCount" yaml:"flipCount"`
	// WindowSeconds is the time span of the observed flapping.
	WindowSeconds int `json:"windowSeconds" yaml:"windowSeconds"`
	// EstimatedCauses lists the likely root causes of the flapping.
	EstimatedCauses []FlappingCause `json:"estimatedCauses,omitempty" yaml:"estimatedCauses,omitempty"`
	// Recommendations lists actionable suggestions to stop flapping.
	Recommendations []FlappingFix `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	// EventTTLLimitation warns about the Event TTL constraint.
	EventTTLLimitation string `json:"eventTtlLimitation,omitempty" yaml:"eventTtlLimitation,omitempty"`
}

// FlappingCause describes a likely root cause of HPA replica flapping.
type FlappingCause struct {
	// Type categorizes the cause: "tight-target", "short-stabilization-window",
	// "missing-scaledown-policy".
	Type string `json:"type" yaml:"type"`
	// Description explains why this cause contributes to flapping.
	Description string `json:"description" yaml:"description"`
	// Confidence is the confidence level: "high", "medium", "low".
	Confidence string `json:"confidence" yaml:"confidence"`
}

// FlappingFix describes an actionable recommendation to stop HPA flapping.
type FlappingFix struct {
	// Action describes what to do.
	Action string `json:"action" yaml:"action"`
	// Patch is an optional JSON merge patch to apply the fix.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Rationale explains why this fix helps.
	Rationale string `json:"rationale" yaml:"rationale"`
}

// AnomalyType identifies the kind of anomaly detected in health score history.
type AnomalyType string

const (
	// AnomalySuddenDegradation indicates a rapid health score drop.
	AnomalySuddenDegradation AnomalyType = "sudden-degradation"
	// AnomalyStuckState indicates the health score has not changed for an extended period.
	AnomalyStuckState AnomalyType = "stuck-state"
	// AnomalyOscillationEscalation indicates increasing oscillation in health scores.
	AnomalyOscillationEscalation AnomalyType = "oscillation-escalation"
)

// AnomalyDetection holds a single anomaly detected in health score history.
type AnomalyDetection struct {
	// Timestamp is when the anomaly was detected.
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`
	// Type is the anomaly type.
	Type AnomalyType `json:"type" yaml:"type"`
	// Severity is the severity: "critical", "warning", or "info".
	Severity string `json:"severity" yaml:"severity"`
	// ScoreBefore is the health score before the anomaly.
	ScoreBefore int `json:"scoreBefore" yaml:"scoreBefore"`
	// ScoreAfter is the health score after the anomaly.
	ScoreAfter int `json:"scoreAfter" yaml:"scoreAfter"`
	// Duration describes how long the anomaly condition persisted.
	Duration string `json:"duration,omitempty" yaml:"duration,omitempty"`
	// CauseEstimate is the estimated root cause of the anomaly.
	CauseEstimate string `json:"causeEstimate,omitempty" yaml:"causeEstimate,omitempty"`
	// Remediation suggests actions to address the anomaly.
	Remediation string `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

// ---------------------------------------------------------------------------
// Rollout Report types (rollout command)
// ---------------------------------------------------------------------------

// RolloutReport holds the complete rollout-aware HPA diagnostics for a
// single HPA. It detects rollout-related risks that can make HPA behavior
// confusing or incorrect during rolling updates.
type RolloutReport struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA resource name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// RolloutInProgress indicates whether a rollout is currently in progress.
	RolloutInProgress bool `json:"rolloutInProgress" yaml:"rolloutInProgress"`
	// NewPodsReady is the count of new pods that are ready vs total new pods.
	NewPodsReady string `json:"newPodsReady,omitempty" yaml:"newPodsReady,omitempty"`
	// Summary is a one-line overall assessment.
	Summary string `json:"summary" yaml:"summary"`
	// Checks lists individual rollout-aware check results.
	Checks []RolloutCheck `json:"checks" yaml:"checks"`
	// Risks lists detected risks during rollout.
	Risks []RolloutRisk `json:"risks,omitempty" yaml:"risks,omitempty"`
	// Recommendation is the overall recommendation text.
	Recommendation string `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	// NextActions lists concrete actions to take.
	NextActions []string `json:"nextActions,omitempty" yaml:"nextActions,omitempty"`
}

// RolloutCheck is a single rollout-aware diagnostic check result.
type RolloutCheck struct {
	// Pass is true when the check succeeds.
	Pass bool `json:"pass" yaml:"pass"`
	// Category is the check category: "probe", "metric", "readiness", "container".
	Category string `json:"category" yaml:"category"`
	// Message describes the check outcome.
	Message string `json:"message" yaml:"message"`
}

// RolloutRisk represents a detected risk during rollout that may affect HPA.
type RolloutRisk struct {
	// Severity is the risk severity: "high", "medium", "low".
	Severity string `json:"severity" yaml:"severity"`
	// Category is the risk category.
	Category string `json:"category" yaml:"category"`
	// Message describes the risk.
	Message string `json:"message" yaml:"message"`
	// Detail provides additional context.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// RolloutInput aggregates all observable signals for rollout-aware HPA
// diagnostics. The cmd layer assembles this from Kubernetes API calls.
type RolloutInput struct {
	// Namespace is the Kubernetes namespace.
	Namespace string
	// HPAName is the HPA resource name.
	HPAName string
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string
	// RolloutInProgress indicates whether a rollout is currently in progress.
	RolloutInProgress bool
	// UpdatedReplicas is the count of pods running the updated revision.
	UpdatedReplicas int32
	// ReadyReplicas is the count of ready pods.
	ReadyReplicas int32
	// DesiredReplicas is the desired replica count from the workload.
	DesiredReplicas int32
	// HasStartupProbe indicates whether the pod template has a startupProbe.
	HasStartupProbe bool
	// HasReadinessProbe indicates whether the pod template has a readinessProbe.
	HasReadinessProbe bool
	// ReadinessInitialDelaySeconds is the readinessProbe initialDelaySeconds.
	ReadinessInitialDelaySeconds int32
	// HPAContainerMetrics lists container names referenced by HPA
	// ContainerResource metrics.
	HPAContainerMetrics []string
	// TemplateContainerNames lists container names from the current pod template.
	TemplateContainerNames []string
	// NewReplicaSetContainerNames lists container names from the new ReplicaSet.
	NewReplicaSetContainerNames []string
	// PodIssues lists pod-level issues detected during rollout.
	PodIssues []string
}

// ---------------------------------------------------------------------------
// GitOps Review types (gitops review command)
// ---------------------------------------------------------------------------

// GitOpsReview holds the result of reviewing HPA manifest changes for
// risky modifications in a PR or GitOps diff.
type GitOpsReview struct {
	// Files lists the files that were reviewed.
	Files []GitOpsReviewFile `json:"files,omitempty" yaml:"files,omitempty"`
	// Summary is a one-line overall assessment.
	Summary string `json:"summary" yaml:"summary"`
	// RiskLevel is the overall risk level: "high", "medium", "low", "none".
	RiskLevel string `json:"riskLevel" yaml:"riskLevel"`
	// Findings lists all detected risky changes.
	Findings []GitOpsReviewFinding `json:"findings,omitempty" yaml:"findings,omitempty"`
	// Recommendation is the overall recommendation text.
	Recommendation string `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
}

// GitOpsReviewFile holds review results for a single file.
type GitOpsReviewFile struct {
	// Path is the file path.
	Path string `json:"path" yaml:"path"`
	// HPAName is the HPA name from the manifest.
	HPAName string `json:"hpaName,omitempty" yaml:"hpaName,omitempty"`
	// Findings lists findings for this file.
	Findings []GitOpsReviewFinding `json:"findings,omitempty" yaml:"findings,omitempty"`
}

// GitOpsReviewFinding represents a single risky HPA manifest change.
type GitOpsReviewFinding struct {
	// Severity is the finding severity: "high", "medium", "low".
	Severity string `json:"severity" yaml:"severity"`
	// Category is the finding category (e.g. "maxReplicas", "stabilization",
	// "target", "behavior", "metric").
	Category string `json:"category" yaml:"category"`
	// Message describes the finding.
	Message string `json:"message" yaml:"message"`
	// Detail provides additional context.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// GitOpsReviewInput holds the before and after HPA manifests for diff review.
type GitOpsReviewInput struct {
	// Before is the base (old) HPA manifest.
	Before *autoscalingv2.HorizontalPodAutoscaler
	// After is the proposed (new) HPA manifest.
	After *autoscalingv2.HorizontalPodAutoscaler
	// FilePath is the path to the manifest file.
	FilePath string
}

// ---------------------------------------------------------------------------
// Autoscaler Map types (autoscaler-map command)
// ---------------------------------------------------------------------------

// AutoscalerMap holds the complete HPA-to-Node Autoscaler relationship
// visualization for a single HPA.
type AutoscalerMap struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// HPAName is the HPA resource name.
	HPAName string `json:"hpaName" yaml:"hpaName"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// CurrentReplicas is the current replica count.
	CurrentReplicas int32 `json:"currentReplicas" yaml:"currentReplicas"`
	// DesiredReplicas is the desired replica count from HPA.
	DesiredReplicas int32 `json:"desiredReplicas" yaml:"desiredReplicas"`
	// MaxReplicas is the HPA maxReplicas.
	MaxReplicas int32 `json:"maxReplicas" yaml:"maxReplicas"`
	// Summary is a one-line overall assessment.
	Summary string `json:"summary" yaml:"summary"`
	// Layers describes the HPA -> Deployment -> Pods -> Nodes -> Autoscaler layers.
	Layers []AutoscalerMapLayer `json:"layers" yaml:"layers"`
	// Blockers lists detected blockers in the autoscaling chain.
	Blockers []AutoscalerMapBlocker `json:"blockers,omitempty" yaml:"blockers,omitempty"`
	// Recommendation is the overall recommendation text.
	Recommendation string `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	// NextActions lists concrete actions to take.
	NextActions []string `json:"nextActions,omitempty" yaml:"nextActions,omitempty"`
	// Risk is the overall risk level: "high", "medium", "low", or "none".
	Risk string `json:"risk" yaml:"risk"`
	// NextChecks lists kubectl verification commands for detected resources.
	NextChecks []string `json:"nextChecks,omitempty" yaml:"nextChecks,omitempty"`
}

// AutoscalerMapLayer describes one layer in the autoscaling chain.
type AutoscalerMapLayer struct {
	// Name is the layer name: "hpa", "workload", "pods", "nodes", "autoscaler", "external-scaler", "constraints".
	Name string `json:"name" yaml:"name"`
	// Resource is the resource identifier at this layer.
	Resource string `json:"resource" yaml:"resource"`
	// Status is the status summary at this layer.
	Status string `json:"status" yaml:"status"`
	// Details provides additional information about this layer.
	Details []string `json:"details,omitempty" yaml:"details,omitempty"`
	// Healthy indicates whether this layer is functioning correctly.
	Healthy bool `json:"healthy" yaml:"healthy"`
}

// AutoscalerMapBlocker represents a detected blocker in the autoscaling chain.
type AutoscalerMapBlocker struct {
	// Layer is the layer where the blocker was detected.
	Layer string `json:"layer" yaml:"layer"`
	// Severity is the blocker severity: "high", "medium", "low".
	Severity string `json:"severity" yaml:"severity"`
	// Message describes the blocker.
	Message string `json:"message" yaml:"message"`
	// Detail provides additional context.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// AutoscalerMapInput aggregates all observable signals for autoscaler map
// analysis. The cmd layer assembles this from Kubernetes API calls.
type AutoscalerMapInput struct {
	// Namespace is the Kubernetes namespace.
	Namespace string
	// HPAName is the HPA resource name.
	HPAName string
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string
	// CurrentReplicas is the current replica count from HPA status.
	CurrentReplicas int32
	// DesiredReplicas is the desired replica count from HPA status.
	DesiredReplicas int32
	// MaxReplicas is the HPA maxReplicas.
	MaxReplicas int32
	// WorkloadReadyReplicas is the ready replica count from the workload.
	WorkloadReadyReplicas int32
	// WorkloadDesiredReplicas is the desired replica count from the workload.
	WorkloadDesiredReplicas int32
	// PodSummary holds pod-level summary information.
	PodSummary AutoscalerMapPodSummary
	// NodeSummary holds node-level summary information.
	NodeSummary AutoscalerMapNodeSummary
	// ClusterAutoscaler indicates whether Cluster Autoscaler is detected.
	ClusterAutoscaler bool
	// Karpenter indicates whether Karpenter is detected.
	Karpenter bool
	// PendingPods lists pending pods for the scale target.
	PendingPods []PendingPodInfo
	// ScalingActive indicates whether the HPA ScalingActive condition is True.
	ScalingActive bool
	// KEDAInfo holds KEDA ScaledObject information if detected.
	KEDAInfo *AutoscalerMapKEDAInfo
	// VPAInfo holds VPA conflict information if detected.
	VPAInfo *AutoscalerMapVPAInfo
	// PDBs lists PodDisruptionBudgets in the namespace affecting the scale target.
	PDBs []AutoscalerMapPDB
	// Quotas lists ResourceQuotas near their limits in the namespace.
	Quotas []AutoscalerMapQuota
}

// AutoscalerMapKEDAInfo holds KEDA ScaledObject information for the autoscaler map.
type AutoscalerMapKEDAInfo struct {
	// ScaledObjectName is the name of the KEDA ScaledObject.
	ScaledObjectName string `json:"scaledObjectName" yaml:"scaledObjectName"`
	// TriggerCount is the number of triggers configured.
	TriggerCount int `json:"triggerCount" yaml:"triggerCount"`
	// Active indicates whether the ScaledObject is active.
	Active bool `json:"active" yaml:"active"`
}

// AutoscalerMapVPAInfo holds VPA conflict information for the autoscaler map.
type AutoscalerMapVPAInfo struct {
	// VPAName is the name of the conflicting VPA.
	VPAName string `json:"vpaName" yaml:"vpaName"`
	// TargetRef is the VPA target reference.
	TargetRef string `json:"targetRef" yaml:"targetRef"`
	// UpdateMode is the VPA update mode.
	UpdateMode string `json:"updateMode" yaml:"updateMode"`
	// ControlledResources lists the resource types controlled by VPA.
	ControlledResources []string `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	// ConflictResources lists the resource types that conflict with HPA.
	ConflictResources []string `json:"conflictResources,omitempty" yaml:"conflictResources,omitempty"`
}

// AutoscalerMapPDB represents a PodDisruptionBudget relevant to the autoscaler map.
type AutoscalerMapPDB struct {
	// Name is the PDB name.
	Name string `json:"name" yaml:"name"`
	// MinAvailable is the minAvailable setting if set.
	MinAvailable string `json:"minAvailable,omitempty" yaml:"minAvailable,omitempty"`
	// MaxUnavailable is the maxUnavailable setting if set.
	MaxUnavailable string `json:"maxUnavailable,omitempty" yaml:"maxUnavailable,omitempty"`
}

// AutoscalerMapQuota represents a ResourceQuota near its limit.
type AutoscalerMapQuota struct {
	// Name is the quota name.
	Name string `json:"name" yaml:"name"`
	// Resource is the resource type (e.g. "limits.cpu").
	Resource string `json:"resource" yaml:"resource"`
	// Used is the current usage.
	Used string `json:"used" yaml:"used"`
	// Hard is the hard limit.
	Hard string `json:"hard" yaml:"hard"`
	// Ratio is the usage ratio (0.0 to 1.0+).
	Ratio float64 `json:"ratio" yaml:"ratio"`
}

// AutoscalerMapPodSummary holds pod-level summary information.
type AutoscalerMapPodSummary struct {
	// Total is the total number of pods.
	Total int32 `json:"total" yaml:"total"`
	// Running is the count of running pods.
	Running int32 `json:"running" yaml:"running"`
	// Pending is the count of pending pods.
	Pending int32 `json:"pending" yaml:"pending"`
	// Ready is the count of ready pods.
	Ready int32 `json:"ready" yaml:"ready"`
}

// AutoscalerMapNodeSummary holds node-level summary information.
type AutoscalerMapNodeSummary struct {
	// TotalNodes is the total number of nodes.
	TotalNodes int32 `json:"totalNodes" yaml:"totalNodes"`
	// AllocatableCPU is the total allocatable CPU across all nodes.
	AllocatableCPU string `json:"allocatableCpu,omitempty" yaml:"allocatableCpu,omitempty"`
	// AllocatableMemory is the total allocatable memory across all nodes.
	AllocatableMemory string `json:"allocatableMemory,omitempty" yaml:"allocatableMemory,omitempty"`
	// TaintedNodes is the count of tainted nodes.
	TaintedNodes int32 `json:"taintedNodes,omitempty" yaml:"taintedNodes,omitempty"`
	// MatchingNodePools lists node pools that match the workload's scheduling constraints.
	MatchingNodePools []string `json:"matchingNodePools,omitempty" yaml:"matchingNodePools,omitempty"`
	// PodCPURequest is the CPU request per pod.
	PodCPURequest string `json:"podCpuRequest,omitempty" yaml:"podCpuRequest,omitempty"`
	// PodMemoryRequest is the memory request per pod.
	PodMemoryRequest string `json:"podMemoryRequest,omitempty" yaml:"podMemoryRequest,omitempty"`
}

// ---------------------------------------------------------------------------
// Support Bundle types (support-bundle command)
// ---------------------------------------------------------------------------

// SupportBundleMetadata holds metadata about what's included in the bundle.
type SupportBundleMetadata struct {
	// KEDADetected indicates whether KEDA was detected in the cluster.
	KEDADetected bool `json:"kedaDetected" yaml:"kedaDetected"`
	// VPADetected indicates whether VPA was detected in the cluster.
	VPADetected bool `json:"vpaDetected" yaml:"vpaDetected"`
	// KEDAScaledObject is the KEDA ScaledObject YAML if KEDA is detected.
	KEDAScaledObject string `json:"kedaScaledObject,omitempty" yaml:"kedaScaledObject,omitempty"`
	// VPARecommendation is the VPA recommendation YAML if VPA is detected.
	VPARecommendation string `json:"vpaRecommendation,omitempty" yaml:"vpaRecommendation,omitempty"`
}
