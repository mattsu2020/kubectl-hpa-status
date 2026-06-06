// Package hpa provides HPA analysis, health scoring, metric formatting,
// and diagnostic interpretation for HorizontalPodAutoscaler resources.
package hpa

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const limitation = "[confidence: high] This plugin uses existing HPA status, conditions, metrics, and events. It does not expose internal controller calculations."

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
	// MetricsDiagnostics holds per-metric health check results for the metrics pipeline.
	MetricsDiagnostics *MetricsPipelineDiagnostics `json:"metricsDiagnostics,omitempty" yaml:"metricsDiagnostics,omitempty"`
	// ResourceCheck holds warnings about resource request/limit consistency with HPA targets.
	ResourceCheck *ResourceCheckResult `json:"resourceCheck,omitempty" yaml:"resourceCheck,omitempty"`
	// PodAnalysis holds per-pod readiness and resource analysis for the scale target.
	PodAnalysis *PodAnalysis `json:"podAnalysis,omitempty" yaml:"podAnalysis,omitempty"`
	// MetricDecisionTrace holds a comprehensive per-metric analysis explaining
	// which metric drove the HPA scaling decision and why. Populated when
	// multiple current metrics are present.
	MetricDecisionTrace *MetricDecisionTrace `json:"metricDecisionTrace,omitempty" yaml:"metricDecisionTrace,omitempty"`
	// Simulation holds what-if analysis results from --simulate.
	Simulation *SimulationResult `json:"simulation,omitempty" yaml:"simulation,omitempty"`
	// CapacityContext holds infrastructure capacity analysis for the scale target.
	CapacityContext *CapacityContext `json:"capacityContext,omitempty" yaml:"capacityContext,omitempty"`
	// EnrichmentStatus holds KEDA/VPA enrichment skip reasons for diagnostic output.
	// Populated during enrichment to explain why data may be absent.
	EnrichmentStatus interface{} `json:"enrichmentStatus,omitempty" yaml:"enrichmentStatus,omitempty"`
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
}

// StructuredMessage provides a machine-readable representation of an
// interpretation or action line, with a reason, human message, and
// suggested next step.
type StructuredMessage struct {
	Reason     string     `json:"reason" yaml:"reason"`
	Message    string     `json:"message" yaml:"message"`
	NextStep   string     `json:"nextStep,omitempty" yaml:"nextStep,omitempty"`
	Severity   Severity   `json:"severity,omitempty" yaml:"severity,omitempty"`
	Confidence Confidence `json:"confidence,omitempty" yaml:"confidence,omitempty"`
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
	ResourceIssues []PodResourceIssue `json:"resourceIssues,omitempty" yaml:"resourceIssues,omitempty"`
	ContainerChecks []ContainerCheck   `json:"containerChecks,omitempty" yaml:"containerChecks,omitempty"`
}

// PodResourceIssue describes a pod container missing CPU or memory requests/limits.
type PodResourceIssue struct {
	Pod       string `json:"pod" yaml:"pod"`
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Category  string `json:"category" yaml:"category"` // "missing-request", "missing-limit"
}

// ContainerCheck verifies that a ContainerResource metric target container exists in pods.
type ContainerCheck struct {
	Container string `json:"container" yaml:"container"`
	Found     bool   `json:"found" yaml:"found"`
	Message   string `json:"message,omitempty" yaml:"message,omitempty"`
}

// SimulationResult holds the before/after comparison of an HPA simulation.
type SimulationResult struct {
	Parameter         string             `json:"parameter" yaml:"parameter"`
	OriginalValue     string             `json:"originalValue" yaml:"originalValue"`
	SimulatedValue    string             `json:"simulatedValue" yaml:"simulatedValue"`
	Before            SimulationState    `json:"before" yaml:"before"`
	After             SimulationState    `json:"after" yaml:"after"`
	RiskAssessment    string             `json:"riskAssessment,omitempty" yaml:"riskAssessment,omitempty"`
	Interpretation    []string           `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	MetricSimulations []MetricSimulation `json:"metricSimulations,omitempty" yaml:"metricSimulations,omitempty"`
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
}

// CapacityContext holds infrastructure capacity analysis for the HPA scale target.
type CapacityContext struct {
	PendingPods      []PendingPodInfo  `json:"pendingPods,omitempty" yaml:"pendingPods,omitempty"`
	QuotaConstraints []QuotaConstraint `json:"quotaConstraints,omitempty" yaml:"quotaConstraints,omitempty"`
	PDBInterference  []PDBInterference `json:"pdbInterference,omitempty" yaml:"pdbInterference,omitempty"`
	NodeHints        []string          `json:"nodeHints,omitempty" yaml:"nodeHints,omitempty"`
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
