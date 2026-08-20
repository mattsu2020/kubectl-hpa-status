package hpa

import (
	"encoding/json"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/behavioradvisor"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/containeradvisor"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/flapping"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/gitops"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/readiness"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/simulate"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/warmup"
)

// This file is the v1 wire decoupling for the Analysis storage flip tracked
// in docs/analysis-storage-flip.md. FlatAnalysis is the explicit flat v1
// projection: today Analysis.Flat() reaches the groups through Grouped(),
// so the inverse mapping (groups -> flat) is written and tested before the
// storage changes; when the grouped views become the primary in-memory
// storage, Flat() and the v1 output path below stay untouched and the v1
// bytes stay identical.
//
// Field policy: FlatAnalysis mirrors the exported fields of Analysis in the
// exact declaration order with the exact tags, so encoding/json and
// sigs.k8s.io/yaml (which marshals via JSON) emit identical keys in identical
// order. schema_v1_test.go enforces the mirror by reflection in both
// directions; do not edit one struct without the other.

// FlatAnalysis is the flat v1 wire shape of Analysis. It carries no behavior;
// construct it with Analysis.Flat().
type FlatAnalysis struct {
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
	// which branch of SummarizeDirection produced Summary.
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
	KEDAInfo *keda.Analysis `json:"keda,omitempty" yaml:"keda,omitempty"`
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
	DecisionSignals []DecisionSignal `json:"decisionSignals,omitempty" yaml:"decisionSignals,omitempty"`
	// StabilizationWindowSeconds is the configured scale-down stabilization window.
	StabilizationWindowSeconds *int32 `json:"stabilizationWindowSeconds,omitempty" yaml:"stabilizationWindowSeconds,omitempty"`
	// StabilizationSource indicates which behavior direction caused stabilization.
	StabilizationSource string `json:"stabilizationSource,omitempty" yaml:"stabilizationSource,omitempty"`
	// StabilizationConfidence is the confidence label for stabilization estimates.
	StabilizationConfidence string `json:"stabilizationConfidence,omitempty" yaml:"stabilizationConfidence,omitempty"`
	// MetricsDiagnostics holds per-metric health check results for the metrics pipeline.
	MetricsDiagnostics *MetricsPipelineDiagnostics `json:"metricsDiagnostics,omitempty" yaml:"metricsDiagnostics,omitempty"`
	// MetricFreshnessEntries holds per-metric freshness analysis results.
	MetricFreshnessEntries []MetricFreshness `json:"metricFreshness,omitempty" yaml:"metricFreshness,omitempty"`
	// ResourceCheck holds warnings about resource request/limit consistency with HPA targets.
	ResourceCheck *ResourceCheckResult `json:"resourceCheck,omitempty" yaml:"resourceCheck,omitempty"`
	// PodAnalysis holds per-pod readiness and resource analysis for the scale target.
	PodAnalysis *PodAnalysis `json:"podAnalysis,omitempty" yaml:"podAnalysis,omitempty"`
	// MetricDecisionTrace holds a comprehensive per-metric analysis explaining
	// which metric drove the HPA scaling decision and why.
	MetricDecisionTrace *MetricDecisionTrace `json:"metricDecisionTrace,omitempty" yaml:"metricDecisionTrace,omitempty"`
	// DecisionTrace holds the human-oriented step-by-step HPA decision trace.
	DecisionTrace *DecisionTrace `json:"decisionTrace,omitempty" yaml:"decisionTrace,omitempty"`
	// FlappingSimulation holds what-if analysis results from --simulate.
	FlappingSimulation *simulate.SimulationResult `json:"simulation,omitempty" yaml:"simulation,omitempty"`
	// CapacityContext holds infrastructure capacity analysis for the scale target.
	CapacityContext *CapacityContext `json:"capacityContext,omitempty" yaml:"capacityContext,omitempty"`
	// CapacityHeadroom estimates whether the cluster can absorb additional pods.
	CapacityHeadroom *CapacityHeadroom `json:"capacityHeadroom,omitempty" yaml:"capacityHeadroom,omitempty"`
	// ReadinessImpact explains how not-yet-ready pods and missing PodMetrics may
	// affect HPA CPU/resource decisions.
	ReadinessImpact *readiness.Impact `json:"readinessImpact,omitempty" yaml:"readinessImpact,omitempty"`
	// ScalePath explains the visible path from HPA desired replicas to scheduled pods.
	ScalePath *ScalePath `json:"scalePath,omitempty" yaml:"scalePath,omitempty"`
	// RolloutDiagnosis holds Deployment/StatefulSet rollout context for HPA diagnosis.
	RolloutDiagnosis *RolloutDiagnosis `json:"rolloutDiagnosis,omitempty" yaml:"rolloutDiagnosis,omitempty"`
	// ControllerProfile holds cluster-wide HPA controller timing assumptions.
	ControllerProfile *ControllerProfile `json:"controllerProfile,omitempty" yaml:"controllerProfile,omitempty"`
	// BlockerReport holds scale-out blocker analysis for the HPA scale target.
	BlockerReport *blocker.Report `json:"blockerReport,omitempty" yaml:"blockerReport,omitempty"`
	// CapacityPlan holds a pre-flight capacity check result.
	CapacityPlan *CapacityPlan `json:"capacityPlan,omitempty" yaml:"capacityPlan,omitempty"`
	// EnrichmentStatus holds KEDA/VPA enrichment skip reasons for diagnostic output.
	EnrichmentStatus *EnrichmentStatus `json:"enrichmentStatus,omitempty" yaml:"enrichmentStatus,omitempty"`
	// MetricContract holds metrics contract validation results.
	MetricContract *MetricContractReport `json:"metricContract,omitempty" yaml:"metricContract,omitempty"`
	// GitOpsConflict holds GitOps manifest conflict detection results.
	GitOpsConflict *gitops.Conflict `json:"gitopsConflict,omitempty" yaml:"gitopsConflict,omitempty"`
	// ChurnAnalysis holds the thrashing/churn detection result for the HPA timeline.
	ChurnAnalysis *churn.ChurnAnalysis `json:"churnAnalysis,omitempty" yaml:"churnAnalysis,omitempty"`
	// VPAAdvisory holds the VPA-HPA coexistence advisory result.
	VPAAdvisory *vpa.Advisory `json:"vpaAdvisory,omitempty" yaml:"vpaAdvisory,omitempty"`
	// MetricHints holds troubleshooting hints for custom/external metrics.
	MetricHints *MetricHintsReport `json:"metricHints,omitempty" yaml:"metricHints,omitempty"`
	// WarmupAnalysis holds the warmup analysis result.
	WarmupAnalysis *warmup.Analysis `json:"warmupAnalysis,omitempty" yaml:"warmupAnalysis,omitempty"`
	// ContainerAdvisor holds the ContainerResource advisor result.
	ContainerAdvisor *containeradvisor.Result `json:"containerAdvisor,omitempty" yaml:"containerAdvisor,omitempty"`
	// BehaviorAdvisor holds the behavior tuning advisor result.
	BehaviorAdvisor *behavioradvisor.Result `json:"behaviorAdvisor,omitempty" yaml:"behaviorAdvisor,omitempty"`
	// HealthTrend holds the health score trend analysis over time.
	HealthTrend *healthtrend.Result `json:"healthTrend,omitempty" yaml:"healthTrend,omitempty"`
	// StructuredDecisionTrace holds the comprehensive structured decision trace.
	StructuredDecisionTrace *StructuredDecisionTrace `json:"structuredDecisionTrace,omitempty" yaml:"structuredDecisionTrace,omitempty"`
	// FlappingPrevention holds the flapping prevention advisor result.
	FlappingPrevention *flapping.PreventionReport `json:"flappingPrevention,omitempty" yaml:"flappingPrevention,omitempty"`
	// FlappingDiagnosis holds event-based flapping detection with root-cause analysis.
	FlappingDiagnosis *flapping.Diagnosis `json:"flappingDiagnosis,omitempty" yaml:"flappingDiagnosis,omitempty"`
	// AdapterDiagnostics holds custom/external metrics adapter diagnostics.
	AdapterDiagnostics *AdapterDiagnosticsReport `json:"adapterDiagnostics,omitempty" yaml:"adapterDiagnostics,omitempty"`
	// Assumptions documents inferred/estimated values the analysis relies on.
	Assumptions []Assumption `json:"assumptions,omitempty" yaml:"assumptions,omitempty"`
	// Warnings records enrichment-pipeline errors and notable skip reasons.
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// Flat projects the analysis onto the flat v1 wire shape by inverting the
// grouped views. Building it through Grouped() (rather than reading the flat
// fields directly) means the storage flip does not change this function: it
// is the inverse mapping the flip depends on, exercised here first.
//
// The one view divergence is CreationTimestamp: MetaView carries the
// RFC3339 string the v2 wire needs, so Flat parses it back. The v1 wire
// itself is second-precision RFC3339, so the round trip is faithful.
func (a *Analysis) Flat() FlatAnalysis {
	if a == nil {
		return FlatAnalysis{}
	}
	g := a.Grouped()
	return FlatAnalysis{
		Namespace:                  g.Meta.Namespace,
		Name:                       g.Meta.Name,
		Target:                     g.Meta.Target,
		Current:                    g.Replicas.Current,
		Desired:                    g.Replicas.Desired,
		Min:                        g.Replicas.Min,
		Max:                        g.Replicas.Max,
		Health:                     g.Decision.Health,
		HealthScore:                g.Decision.HealthScore,
		HealthResult:               g.Decision.HealthResult,
		HiddenFactors:              g.Lifecycle.HiddenFactors,
		Summary:                    g.Decision.Summary,
		SummaryKey:                 g.Decision.SummaryKey,
		Conditions:                 g.Conditions.Conditions,
		Metrics:                    g.Metrics.Metrics,
		Behavior:                   g.Conditions.Behavior,
		Actions:                    g.Actions.Actions,
		Suggestions:                g.Actions.Suggestions,
		Interpretation:             g.Actions.Interpretation,
		KEDAInfo:                   g.Controllers.KEDAInfo,
		VPAConflict:                g.Advisory.VPAConflict,
		TargetReplicas:             g.Replicas.TargetReplicas,
		Debug:                      g.Lifecycle.Debug,
		ImpactMetric:               g.Decision.ImpactMetric,
		CreationTimestamp:          parseMetaTimestamp(g.Meta.CreationTimestamp),
		StaleStatus:                g.Lifecycle.StaleStatus,
		StabilizationRemaining:     g.Conditions.StabilizationRemaining,
		ScaleToZero:                g.ScaleToZero.ScaleToZero,
		StructuredInterpretation:   g.Actions.StructuredInterpretation,
		StructuredActions:          g.Actions.StructuredActions,
		DecisionSignals:            g.Decision.DecisionSignals,
		StabilizationWindowSeconds: g.Conditions.StabilizationWindowSeconds,
		StabilizationSource:        g.Conditions.StabilizationSource,
		StabilizationConfidence:    g.Conditions.StabilizationConfidence,
		MetricsDiagnostics:         g.Metrics.MetricsDiagnostics,
		MetricFreshnessEntries:     g.Metrics.MetricFreshness,
		ResourceCheck:              g.Capacity.ResourceCheck,
		PodAnalysis:                g.Capacity.PodAnalysis,
		MetricDecisionTrace:        g.Decision.MetricDecisionTrace,
		DecisionTrace:              g.Decision.DecisionTrace,
		FlappingSimulation:         g.Stability.FlappingSimulation,
		CapacityContext:            g.Capacity.CapacityContext,
		CapacityHeadroom:           g.Capacity.CapacityHeadroom,
		ReadinessImpact:            g.Capacity.ReadinessImpact,
		ScalePath:                  g.Capacity.ScalePath,
		RolloutDiagnosis:           g.Controllers.RolloutDiagnosis,
		ControllerProfile:          g.Controllers.ControllerProfile,
		BlockerReport:              g.Blockers.BlockerReport,
		CapacityPlan:               g.Capacity.CapacityPlan,
		EnrichmentStatus:           g.Lifecycle.EnrichmentStatus,
		MetricContract:             g.Metrics.MetricContract,
		GitOpsConflict:             g.Blockers.GitOpsConflict,
		ChurnAnalysis:              g.Stability.ChurnAnalysis,
		VPAAdvisory:                g.Advisory.VPAAdvisory,
		MetricHints:                g.Metrics.MetricHints,
		WarmupAnalysis:             g.ScaleToZero.WarmupAnalysis,
		ContainerAdvisor:           g.Advisory.ContainerAdvisor,
		BehaviorAdvisor:            g.Advisory.BehaviorAdvisor,
		HealthTrend:                g.Lifecycle.HealthTrend,
		StructuredDecisionTrace:    g.Decision.StructuredDecisionTrace,
		FlappingPrevention:         g.Stability.FlappingPrevention,
		FlappingDiagnosis:          g.Stability.FlappingDiagnosis,
		AdapterDiagnostics:         g.Metrics.AdapterDiagnostics,
		Assumptions:                g.Actions.Assumptions,
		Warnings:                   g.Actions.Warnings,
	}
}

// parseMetaTimestamp converts MetaView's RFC3339 string back to metav1.Time.
// An empty or malformed string yields the zero time, matching the omitted
// wire field.
func parseMetaTimestamp(s string) metav1.Time {
	if s == "" {
		return metav1.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return metav1.Time{}
	}
	return metav1.NewTime(t)
}

// MarshalJSON serializes Analysis in the flat v1 wire shape. Routing every
// JSON/YAML consumer (sigs.k8s.io/yaml marshals via JSON) through the
// projection is what lets the storage flip change Analysis's in-memory
// layout without touching a single byte of v1 output: the shape comes from
// FlatAnalysis, not from Analysis's own fields.
func (a Analysis) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Flat())
}

// UnmarshalJSON decodes the flat v1 wire shape into group storage, so
// consumers that decode v1 JSON into Analysis keep working across the
// storage flip.
func (a *Analysis) UnmarshalJSON(data []byte) error {
	var f FlatAnalysis
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	*a = *NewAnalysis(f)
	return nil
}

// StatusReportV1 is the flat v1 wire envelope for a single-HPA status output.
// It mirrors StatusReport's tags so the emitted bytes are identical to the
// historical Analysis-based output.
type StatusReportV1 struct {
	APIVersion string       `json:"apiVersion" yaml:"apiVersion"`
	Analysis   FlatAnalysis `json:"analysis" yaml:"analysis"`
	Events     []Event      `json:"events,omitempty" yaml:"events,omitempty"`
}

// StatusBatchV1 is the flat v1 wire envelope for multi-HPA status output.
type StatusBatchV1 struct {
	APIVersion string              `json:"apiVersion" yaml:"apiVersion"`
	Items      []StatusBatchItemV1 `json:"items" yaml:"items"`
}

// StatusBatchItemV1 mirrors StatusBatchItem while carrying a flat report.
type StatusBatchItemV1 struct {
	Namespace string            `json:"namespace" yaml:"namespace"`
	Name      string            `json:"name" yaml:"name"`
	Status    StatusBatchStatus `json:"status" yaml:"status"`
	Error     string            `json:"error,omitempty" yaml:"error,omitempty"`
	Report    *StatusReportV1   `json:"report,omitempty" yaml:"report,omitempty"`
}

// ProjectStatusReportV1 projects a report onto the flat v1 wire envelope.
func ProjectStatusReportV1(report StatusReport) StatusReportV1 {
	return StatusReportV1{
		APIVersion: report.APIVersion,
		Analysis:   report.Analysis.Flat(),
		Events:     cloneEvents(report.Events),
	}
}

// ProjectStatusReportsV1 preserves input order while projecting reports.
func ProjectStatusReportsV1(reports []StatusReport) []StatusReportV1 {
	if len(reports) == 0 {
		return nil
	}
	projected := make([]StatusReportV1, len(reports))
	for i := range reports {
		projected[i] = ProjectStatusReportV1(reports[i])
	}
	return projected
}

// ProjectStatusBatchV1 projects a multi-HPA batch onto the flat v1 envelope.
func ProjectStatusBatchV1(batch StatusBatch) StatusBatchV1 {
	items := make([]StatusBatchItemV1, 0, len(batch.Items))
	for _, item := range batch.Items {
		projected := StatusBatchItemV1{
			Namespace: item.Namespace,
			Name:      item.Name,
			Status:    item.Status,
			Error:     item.Error,
		}
		if item.Report != nil {
			report := ProjectStatusReportV1(*item.Report)
			projected.Report = &report
		}
		items = append(items, projected)
	}
	return StatusBatchV1{
		APIVersion: batch.APIVersion,
		Items:      items,
	}
}
