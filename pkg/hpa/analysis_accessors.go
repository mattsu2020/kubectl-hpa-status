package hpa

import (
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

// This file is the flat compatibility surface over the grouped storage (the
// storage flip tracked in docs/analysis-storage-flip.md). The accessors keep
// the historical field names — a.Current() / a.SetCurrent(v) replace the
// retired Current field — so the v1 semantics survive unchanged while the
// in-memory layout is the 13 grouped views. The v1 wire does not depend on
// any of this: serialization goes through FlatAnalysis (Analysis.Flat /
// MarshalJSON).
//
// Getters use pointer receivers so reads do not copy the (large) struct;
// call sites need an addressable Analysis, which the compiler enforces.
// The one rename is Metrics: it would collide with the MetricsGroup view
// method naming family, but since no view method is named Metrics the
// historical name is kept.

// NewAnalysis builds an Analysis from the flat v1 field values. It is the
// inverse of Analysis.Flat() and the migration path for code that used to
// construct Analysis with a composite literal: NewAnalysis(FlatAnalysis{...}).
// NewAnalysis(fillDeepFixture).Flat() == fixture is property-tested in
// schema_v1_test.go.
func NewAnalysis(f FlatAnalysis) *Analysis {
	a := &Analysis{
		creationTimestamp: f.CreationTimestamp,
		meta: MetaView{
			Namespace: f.Namespace,
			Name:      f.Name,
			Target:    f.Target,
		},
		replicas: ReplicasView{
			Current:        f.Current,
			Desired:        f.Desired,
			Min:            f.Min,
			Max:            f.Max,
			TargetReplicas: f.TargetReplicas,
		},
		decision: DecisionView{
			Health:                  f.Health,
			HealthScore:             f.HealthScore,
			HealthResult:            f.HealthResult,
			Summary:                 f.Summary,
			SummaryKey:              f.SummaryKey,
			ImpactMetric:            f.ImpactMetric,
			DecisionTrace:           f.DecisionTrace,
			MetricDecisionTrace:     f.MetricDecisionTrace,
			StructuredDecisionTrace: f.StructuredDecisionTrace,
			DecisionSignals:         f.DecisionSignals,
		},
		metrics: MetricsView{
			Metrics:            f.Metrics,
			MetricsDiagnostics: f.MetricsDiagnostics,
			MetricFreshness:    f.MetricFreshnessEntries,
			MetricContract:     f.MetricContract,
			MetricHints:        f.MetricHints,
			AdapterDiagnostics: f.AdapterDiagnostics,
		},
		conditions: ConditionsView{
			Conditions:                 f.Conditions,
			Behavior:                   f.Behavior,
			StabilizationWindowSeconds: f.StabilizationWindowSeconds,
			StabilizationSource:        f.StabilizationSource,
			StabilizationConfidence:    f.StabilizationConfidence,
			StabilizationRemaining:     f.StabilizationRemaining,
		},
		capacity: CapacityView{
			CapacityContext:  f.CapacityContext,
			CapacityHeadroom: f.CapacityHeadroom,
			CapacityPlan:     f.CapacityPlan,
			ResourceCheck:    f.ResourceCheck,
			PodAnalysis:      f.PodAnalysis,
			ScalePath:        f.ScalePath,
			ReadinessImpact:  f.ReadinessImpact,
		},
		scaleToZero: ScaleToZeroView{
			ScaleToZero:    f.ScaleToZero,
			WarmupAnalysis: f.WarmupAnalysis,
		},
		stability: StabilityView{
			FlappingSimulation: f.FlappingSimulation,
			FlappingPrevention: f.FlappingPrevention,
			FlappingDiagnosis:  f.FlappingDiagnosis,
			ChurnAnalysis:      f.ChurnAnalysis,
		},
		advisory: AdvisoryView{
			VPAConflict:      f.VPAConflict,
			VPAAdvisory:      f.VPAAdvisory,
			ContainerAdvisor: f.ContainerAdvisor,
			BehaviorAdvisor:  f.BehaviorAdvisor,
		},
		controllers: ControllersView{
			KEDAInfo:          f.KEDAInfo,
			RolloutDiagnosis:  f.RolloutDiagnosis,
			ControllerProfile: f.ControllerProfile,
		},
		blockers: BlockersView{
			BlockerReport:  f.BlockerReport,
			GitOpsConflict: f.GitOpsConflict,
		},
		actions: ActionsView{
			Actions:                  f.Actions,
			Suggestions:              f.Suggestions,
			StructuredActions:        f.StructuredActions,
			StructuredInterpretation: f.StructuredInterpretation,
			Interpretation:           f.Interpretation,
			Assumptions:              f.Assumptions,
			Warnings:                 f.Warnings,
		},
		lifecycle: LifecycleView{
			StaleStatus:      f.StaleStatus,
			HealthTrend:      f.HealthTrend,
			Debug:            f.Debug,
			HiddenFactors:    f.HiddenFactors,
			EnrichmentStatus: f.EnrichmentStatus,
		},
	}
	return a
}

// --- meta ---

// Namespace is the Kubernetes namespace of the HPA.
func (a *Analysis) Namespace() string { return a.meta.Namespace }

// SetNamespace sets the HPA namespace.
func (a *Analysis) SetNamespace(v string) { a.meta.Namespace = v }

// Name is the HPA resource name.
func (a *Analysis) Name() string { return a.meta.Name }

// SetName sets the HPA resource name.
func (a *Analysis) SetName(v string) { a.meta.Name = v }

// Target is the scaleTargetRef in "Kind/Name" format.
func (a *Analysis) Target() string { return a.meta.Target }

// SetTarget sets the scaleTargetRef label.
func (a *Analysis) SetTarget(v string) { a.meta.Target = v }

// CreationTimestamp is the HPA creation time.
func (a *Analysis) CreationTimestamp() metav1.Time { return a.creationTimestamp }

// SetCreationTimestamp sets the HPA creation time.
func (a *Analysis) SetCreationTimestamp(v metav1.Time) { a.creationTimestamp = v }

// --- replicas ---

// Current is the current replica count from HPA status.
func (a *Analysis) Current() int32 { return a.replicas.Current }

// SetCurrent sets the current replica count.
func (a *Analysis) SetCurrent(v int32) { a.replicas.Current = v }

// Desired is the desired replica count from HPA status.
func (a *Analysis) Desired() int32 { return a.replicas.Desired }

// SetDesired sets the desired replica count.
func (a *Analysis) SetDesired(v int32) { a.replicas.Desired = v }

// Min is the minimum replica count (defaults to 1 if spec.minReplicas is nil).
func (a *Analysis) Min() int32 { return a.replicas.Min }

// SetMin sets the minimum replica count.
func (a *Analysis) SetMin(v int32) { a.replicas.Min = v }

// Max is the maximum replica count from spec.maxReplicas.
func (a *Analysis) Max() int32 { return a.replicas.Max }

// SetMax sets the maximum replica count.
func (a *Analysis) SetMax(v int32) { a.replicas.Max = v }

// TargetReplicas holds replica status from the scale target resource.
func (a *Analysis) TargetReplicas() *TargetReplicaInfo { return a.replicas.TargetReplicas }

// SetTargetReplicas sets the scale-target replica status.
func (a *Analysis) SetTargetReplicas(v *TargetReplicaInfo) { a.replicas.TargetReplicas = v }

// --- decision ---

// Health is the health state: "OK", "ERROR", "LIMITED", or "STABILIZED".
func (a *Analysis) Health() string { return a.decision.Health }

// SetHealth sets the health state.
func (a *Analysis) SetHealth(v string) { a.decision.Health = v }

// HealthScore is the numeric health score from 0 (worst) to 100 (best).
func (a *Analysis) HealthScore() int { return a.decision.HealthScore }

// SetHealthScore sets the numeric health score.
func (a *Analysis) SetHealthScore(v int) { a.decision.HealthScore = v }

// HealthResult holds the typed health state, score, and individual penalty
// signals. Populated when --debug is enabled or for JSON/YAML output.
func (a *Analysis) HealthResult() *HealthResult { return a.decision.HealthResult }

// SetHealthResult sets the typed health result.
func (a *Analysis) SetHealthResult(v *HealthResult) { a.decision.HealthResult = v }

// Summary is a one-line direction summary of the HPA scaling state.
func (a *Analysis) Summary() string { return a.decision.Summary }

// SetSummary sets the direction summary line.
func (a *Analysis) SetSummary(v string) { a.decision.Summary = v }

// SummaryKey is the stable i18n key (e.g. "dir_scale_up") that identifies
// which branch of SummarizeDirection produced Summary.
func (a *Analysis) SummaryKey() string { return a.decision.SummaryKey }

// SetSummaryKey sets the direction summary key.
func (a *Analysis) SetSummaryKey(v string) { a.decision.SummaryKey = v }

// ImpactMetric estimates which metric has the largest scaling impact.
func (a *Analysis) ImpactMetric() *MetricImpactGuess { return a.decision.ImpactMetric }

// SetImpactMetric sets the impact-metric estimate.
func (a *Analysis) SetImpactMetric(v *MetricImpactGuess) { a.decision.ImpactMetric = v }

// DecisionTrace holds the human-oriented step-by-step HPA decision trace.
func (a *Analysis) DecisionTrace() *DecisionTrace { return a.decision.DecisionTrace }

// SetDecisionTrace sets the human-oriented decision trace.
func (a *Analysis) SetDecisionTrace(v *DecisionTrace) { a.decision.DecisionTrace = v }

// MetricDecisionTrace holds the per-metric analysis explaining which metric
// drove the HPA scaling decision and why.
func (a *Analysis) MetricDecisionTrace() *MetricDecisionTrace { return a.decision.MetricDecisionTrace }

// SetMetricDecisionTrace sets the per-metric decision trace.
func (a *Analysis) SetMetricDecisionTrace(v *MetricDecisionTrace) { a.decision.MetricDecisionTrace = v }

// StructuredDecisionTrace holds the comprehensive structured decision trace.
func (a *Analysis) StructuredDecisionTrace() *StructuredDecisionTrace {
	return a.decision.StructuredDecisionTrace
}

// SetStructuredDecisionTrace sets the structured decision trace.
func (a *Analysis) SetStructuredDecisionTrace(v *StructuredDecisionTrace) {
	a.decision.StructuredDecisionTrace = v
}

// DecisionSignals holds future-proof scaling decision data for KEP-6111
// compatibility.
func (a *Analysis) DecisionSignals() []DecisionSignal { return a.decision.DecisionSignals }

// SetDecisionSignals sets the decision signals.
func (a *Analysis) SetDecisionSignals(v []DecisionSignal) { a.decision.DecisionSignals = v }

// --- metrics ---

// Metrics lists formatted metric data for each current metric.
func (a *Analysis) Metrics() []Metric { return a.metrics.Metrics }

// SetMetrics sets the formatted metric list.
func (a *Analysis) SetMetrics(v []Metric) { a.metrics.Metrics = v }

// MetricsDiagnostics holds per-metric health check results for the metrics
// pipeline.
func (a *Analysis) MetricsDiagnostics() *MetricsPipelineDiagnostics {
	return a.metrics.MetricsDiagnostics
}

// SetMetricsDiagnostics sets the metrics pipeline diagnostics.
func (a *Analysis) SetMetricsDiagnostics(v *MetricsPipelineDiagnostics) {
	a.metrics.MetricsDiagnostics = v
}

// MetricFreshnessEntries holds per-metric freshness analysis results.
func (a *Analysis) MetricFreshnessEntries() []MetricFreshness { return a.metrics.MetricFreshness }

// SetMetricFreshnessEntries sets the metric freshness entries.
func (a *Analysis) SetMetricFreshnessEntries(v []MetricFreshness) { a.metrics.MetricFreshness = v }

// MetricContract holds metrics contract validation results.
func (a *Analysis) MetricContract() *MetricContractReport { return a.metrics.MetricContract }

// SetMetricContract sets the metrics contract report.
func (a *Analysis) SetMetricContract(v *MetricContractReport) { a.metrics.MetricContract = v }

// MetricHints holds troubleshooting hints for custom/external metrics.
func (a *Analysis) MetricHints() *MetricHintsReport { return a.metrics.MetricHints }

// SetMetricHints sets the metric hints report.
func (a *Analysis) SetMetricHints(v *MetricHintsReport) { a.metrics.MetricHints = v }

// AdapterDiagnostics holds custom/external metrics adapter diagnostics.
func (a *Analysis) AdapterDiagnostics() *AdapterDiagnosticsReport {
	return a.metrics.AdapterDiagnostics
}

// SetAdapterDiagnostics sets the adapter diagnostics report.
func (a *Analysis) SetAdapterDiagnostics(v *AdapterDiagnosticsReport) {
	a.metrics.AdapterDiagnostics = v
}

// --- conditions ---

// Conditions lists the HPA conditions sorted by priority.
func (a *Analysis) Conditions() []Condition { return a.conditions.Conditions }

// SetConditions sets the condition list.
func (a *Analysis) SetConditions(v []Condition) { a.conditions.Conditions = v }

// Behavior lists the scale-up and scale-down behavior rules, if configured.
func (a *Analysis) Behavior() []BehaviorRule { return a.conditions.Behavior }

// SetBehavior sets the behavior rule list.
func (a *Analysis) SetBehavior(v []BehaviorRule) { a.conditions.Behavior = v }

// StabilizationWindowSeconds is the configured scale-down stabilization window.
func (a *Analysis) StabilizationWindowSeconds() *int32 {
	return a.conditions.StabilizationWindowSeconds
}

// SetStabilizationWindowSeconds sets the stabilization window.
func (a *Analysis) SetStabilizationWindowSeconds(v *int32) {
	a.conditions.StabilizationWindowSeconds = v
}

// StabilizationSource indicates which behavior direction caused
// stabilization: "scaleDown" or "scaleUp".
func (a *Analysis) StabilizationSource() string { return a.conditions.StabilizationSource }

// SetStabilizationSource sets the stabilization source.
func (a *Analysis) SetStabilizationSource(v string) { a.conditions.StabilizationSource = v }

// StabilizationConfidence is the confidence label for stabilization
// estimates.
func (a *Analysis) StabilizationConfidence() string { return a.conditions.StabilizationConfidence }

// SetStabilizationConfidence sets the stabilization confidence label.
func (a *Analysis) SetStabilizationConfidence(v string) { a.conditions.StabilizationConfidence = v }

// StabilizationRemaining estimates seconds remaining in the scale-down
// stabilization window.
func (a *Analysis) StabilizationRemaining() *int64 { return a.conditions.StabilizationRemaining }

// SetStabilizationRemaining sets the remaining stabilization seconds.
func (a *Analysis) SetStabilizationRemaining(v *int64) { a.conditions.StabilizationRemaining = v }

// --- actions ---

// Actions lists recommended action strings for the operator.
func (a *Analysis) Actions() []string { return a.actions.Actions }

// SetActions sets the recommended action list.
func (a *Analysis) SetActions(v []string) { a.actions.Actions = v }

// Suggestions lists patch suggestions with safety metadata.
func (a *Analysis) Suggestions() []Suggestion { return a.actions.Suggestions }

// SetSuggestions sets the suggestion list.
func (a *Analysis) SetSuggestions(v []Suggestion) { a.actions.Suggestions = v }

// StructuredActions provides machine-readable action entries.
func (a *Analysis) StructuredActions() []StructuredMessage { return a.actions.StructuredActions }

// SetStructuredActions sets the structured action entries.
func (a *Analysis) SetStructuredActions(v []StructuredMessage) { a.actions.StructuredActions = v }

// StructuredInterpretation provides machine-readable interpretation entries.
func (a *Analysis) StructuredInterpretation() []StructuredMessage {
	return a.actions.StructuredInterpretation
}

// SetStructuredInterpretation sets the structured interpretation entries.
func (a *Analysis) SetStructuredInterpretation(v []StructuredMessage) {
	a.actions.StructuredInterpretation = v
}

// Interpretation lists detailed interpretation lines with confidence labels.
func (a *Analysis) Interpretation() []string { return a.actions.Interpretation }

// SetInterpretation sets the interpretation lines.
func (a *Analysis) SetInterpretation(v []string) { a.actions.Interpretation = v }

// Assumptions documents inferred/estimated values the analysis relies on.
func (a *Analysis) Assumptions() []Assumption { return a.actions.Assumptions }

// SetAssumptions sets the assumption list.
func (a *Analysis) SetAssumptions(v []Assumption) { a.actions.Assumptions = v }

// Warnings records enrichment-pipeline errors and notable skip reasons.
func (a *Analysis) Warnings() []string { return a.actions.Warnings }

// SetWarnings sets the warning list.
func (a *Analysis) SetWarnings(v []string) { a.actions.Warnings = v }

// --- lifecycle ---

// StaleStatus indicates observedGeneration lag, if detected.
func (a *Analysis) StaleStatus() *StaleStatusInfo { return a.lifecycle.StaleStatus }

// SetStaleStatus sets the stale-status info.
func (a *Analysis) SetStaleStatus(v *StaleStatusInfo) { a.lifecycle.StaleStatus = v }

// HealthTrend holds the health score trend analysis over time.
func (a *Analysis) HealthTrend() *healthtrend.Result { return a.lifecycle.HealthTrend }

// SetHealthTrend sets the health trend result.
func (a *Analysis) SetHealthTrend(v *healthtrend.Result) { a.lifecycle.HealthTrend = v }

// Debug lists verbose debug lines, populated when the debug option is enabled.
func (a *Analysis) Debug() []string { return a.lifecycle.Debug }

// SetDebug sets the debug lines.
func (a *Analysis) SetDebug(v []string) { a.lifecycle.Debug = v }

// HiddenFactors lists HPA decision factors that influence the controller but
// are only partially visible through public status fields.
func (a *Analysis) HiddenFactors() []HiddenDecisionFactor { return a.lifecycle.HiddenFactors }

// SetHiddenFactors sets the hidden decision factors.
func (a *Analysis) SetHiddenFactors(v []HiddenDecisionFactor) { a.lifecycle.HiddenFactors = v }

// EnrichmentStatus holds KEDA/VPA enrichment skip reasons for diagnostic
// output. This is the canonical status model; treat the serialized shape as a
// public contract: add fields additively and never rename existing keys.
func (a *Analysis) EnrichmentStatus() *EnrichmentStatus { return a.lifecycle.EnrichmentStatus }

// SetEnrichmentStatus sets the enrichment status.
func (a *Analysis) SetEnrichmentStatus(v *EnrichmentStatus) { a.lifecycle.EnrichmentStatus = v }

// --- capacity ---

// CapacityContext holds infrastructure capacity analysis for the scale target.
func (a *Analysis) CapacityContext() *CapacityContext { return a.capacity.CapacityContext }

// SetCapacityContext sets the capacity context.
func (a *Analysis) SetCapacityContext(v *CapacityContext) { a.capacity.CapacityContext = v }

// CapacityHeadroom estimates whether the cluster can absorb additional pods
// up to maxReplicas.
func (a *Analysis) CapacityHeadroom() *CapacityHeadroom { return a.capacity.CapacityHeadroom }

// SetCapacityHeadroom sets the capacity headroom.
func (a *Analysis) SetCapacityHeadroom(v *CapacityHeadroom) { a.capacity.CapacityHeadroom = v }

// CapacityPlan holds a pre-flight capacity check result, diagnosing whether
// it is safe to raise maxReplicas.
func (a *Analysis) CapacityPlan() *CapacityPlan { return a.capacity.CapacityPlan }

// SetCapacityPlan sets the capacity plan.
func (a *Analysis) SetCapacityPlan(v *CapacityPlan) { a.capacity.CapacityPlan = v }

// ResourceCheck holds warnings about resource request/limit consistency with
// HPA targets.
func (a *Analysis) ResourceCheck() *ResourceCheckResult { return a.capacity.ResourceCheck }

// SetResourceCheck sets the resource check result.
func (a *Analysis) SetResourceCheck(v *ResourceCheckResult) { a.capacity.ResourceCheck = v }

// PodAnalysis holds per-pod readiness and resource analysis for the scale
// target.
func (a *Analysis) PodAnalysis() *PodAnalysis { return a.capacity.PodAnalysis }

// SetPodAnalysis sets the pod analysis.
func (a *Analysis) SetPodAnalysis(v *PodAnalysis) { a.capacity.PodAnalysis = v }

// ScalePath explains the visible path from HPA desired replicas to scheduled
// pods.
func (a *Analysis) ScalePath() *ScalePath { return a.capacity.ScalePath }

// SetScalePath sets the scale path.
func (a *Analysis) SetScalePath(v *ScalePath) { a.capacity.ScalePath = v }

// ReadinessImpact explains how not-yet-ready pods and missing PodMetrics may
// affect HPA CPU/resource decisions.
func (a *Analysis) ReadinessImpact() *readiness.Impact { return a.capacity.ReadinessImpact }

// SetReadinessImpact sets the readiness impact.
func (a *Analysis) SetReadinessImpact(v *readiness.Impact) { a.capacity.ReadinessImpact = v }

// --- scale to zero ---

// ScaleToZero holds scale-to-zero information, populated when minReplicas=0.
func (a *Analysis) ScaleToZero() *ScaleToZeroInfo { return a.scaleToZero.ScaleToZero }

// SetScaleToZero sets the scale-to-zero info.
func (a *Analysis) SetScaleToZero(v *ScaleToZeroInfo) { a.scaleToZero.ScaleToZero = v }

// WarmupAnalysis holds the warmup analysis result, diagnosing why pods are
// not yet ready after HPA scales out.
func (a *Analysis) WarmupAnalysis() *warmup.Analysis { return a.scaleToZero.WarmupAnalysis }

// SetWarmupAnalysis sets the warmup analysis.
func (a *Analysis) SetWarmupAnalysis(v *warmup.Analysis) { a.scaleToZero.WarmupAnalysis = v }

// --- stability ---

// FlappingSimulation holds what-if analysis results from --simulate.
func (a *Analysis) FlappingSimulation() *simulate.SimulationResult {
	return a.stability.FlappingSimulation
}

// SetFlappingSimulation sets the simulation result.
func (a *Analysis) SetFlappingSimulation(v *simulate.SimulationResult) {
	a.stability.FlappingSimulation = v
}

// FlappingPrevention holds the flapping prevention advisor result with
// what-if simulations for different stabilization window values.
func (a *Analysis) FlappingPrevention() *flapping.PreventionReport {
	return a.stability.FlappingPrevention
}

// SetFlappingPrevention sets the flapping prevention report.
func (a *Analysis) SetFlappingPrevention(v *flapping.PreventionReport) {
	a.stability.FlappingPrevention = v
}

// FlappingDiagnosis holds event-based flapping detection with root-cause
// analysis.
func (a *Analysis) FlappingDiagnosis() *flapping.Diagnosis { return a.stability.FlappingDiagnosis }

// SetFlappingDiagnosis sets the flapping diagnosis.
func (a *Analysis) SetFlappingDiagnosis(v *flapping.Diagnosis) { a.stability.FlappingDiagnosis = v }

// ChurnAnalysis holds the thrashing/churn detection result for the HPA
// timeline.
func (a *Analysis) ChurnAnalysis() *churn.ChurnAnalysis { return a.stability.ChurnAnalysis }

// SetChurnAnalysis sets the churn analysis.
func (a *Analysis) SetChurnAnalysis(v *churn.ChurnAnalysis) { a.stability.ChurnAnalysis = v }

// --- advisory ---

// VPAConflict holds VPA conflict detection results, populated when --vpa is
// enabled.
func (a *Analysis) VPAConflict() *vpa.ConflictInfo { return a.advisory.VPAConflict }

// SetVPAConflict sets the VPA conflict info.
func (a *Analysis) SetVPAConflict(v *vpa.ConflictInfo) { a.advisory.VPAConflict = v }

// VPAAdvisory holds the VPA-HPA coexistence advisory result.
func (a *Analysis) VPAAdvisory() *vpa.Advisory { return a.advisory.VPAAdvisory }

// SetVPAAdvisory sets the VPA advisory.
func (a *Analysis) SetVPAAdvisory(v *vpa.Advisory) { a.advisory.VPAAdvisory = v }

// ContainerAdvisor holds the ContainerResource advisor result.
func (a *Analysis) ContainerAdvisor() *containeradvisor.Result { return a.advisory.ContainerAdvisor }

// SetContainerAdvisor sets the container advisor result.
func (a *Analysis) SetContainerAdvisor(v *containeradvisor.Result) { a.advisory.ContainerAdvisor = v }

// BehaviorAdvisor holds the behavior tuning advisor result.
func (a *Analysis) BehaviorAdvisor() *behavioradvisor.Result { return a.advisory.BehaviorAdvisor }

// SetBehaviorAdvisor sets the behavior advisor result.
func (a *Analysis) SetBehaviorAdvisor(v *behavioradvisor.Result) { a.advisory.BehaviorAdvisor = v }

// --- controllers ---

// KEDAInfo holds KEDA-specific analysis, populated when --keda is enabled.
func (a *Analysis) KEDAInfo() *keda.Analysis { return a.controllers.KEDAInfo }

// SetKEDAInfo sets the KEDA analysis.
func (a *Analysis) SetKEDAInfo(v *keda.Analysis) { a.controllers.KEDAInfo = v }

// RolloutDiagnosis holds Deployment/StatefulSet rollout context for HPA
// diagnosis.
func (a *Analysis) RolloutDiagnosis() *RolloutDiagnosis { return a.controllers.RolloutDiagnosis }

// SetRolloutDiagnosis sets the rollout diagnosis.
func (a *Analysis) SetRolloutDiagnosis(v *RolloutDiagnosis) { a.controllers.RolloutDiagnosis = v }

// ControllerProfile holds cluster-wide HPA controller timing assumptions.
func (a *Analysis) ControllerProfile() *ControllerProfile { return a.controllers.ControllerProfile }

// SetControllerProfile sets the controller profile.
func (a *Analysis) SetControllerProfile(v *ControllerProfile) { a.controllers.ControllerProfile = v }

// --- blockers ---

// BlockerReport holds scale-out blocker analysis for the HPA scale target.
func (a *Analysis) BlockerReport() *blocker.Report { return a.blockers.BlockerReport }

// SetBlockerReport sets the blocker report.
func (a *Analysis) SetBlockerReport(v *blocker.Report) { a.blockers.BlockerReport = v }

// GitOpsConflict holds GitOps manifest conflict detection results.
func (a *Analysis) GitOpsConflict() *gitops.Conflict { return a.blockers.GitOpsConflict }

// SetGitOpsConflict sets the GitOps conflict.
func (a *Analysis) SetGitOpsConflict(v *gitops.Conflict) { a.blockers.GitOpsConflict = v }
