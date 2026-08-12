package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/behavioradvisor"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/containeradvisor"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/flapping"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/gitops"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/readiness"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/warmup"
)

// This file provides additive, read-only "group views" over the flat Analysis
// struct. The flat fields and their JSON tags remain the default v1 contract;
// ProjectStatusReportV2 uses these views for the opt-in nested v2 contract.
// Keeping projection separate from storage lets both schemas share one
// analysis implementation until the v1 wire shape can be retired in a future
// major version.
//
// Each view is a plain value struct (no methods, no mutation) returned by a
// method on *Analysis. The views are snapshots: they copy scalar/struct values
// and share pointer/slice backing arrays (read-only by convention). Callers
// must not mutate the returned views' slice/map fields.
//
// The groups match the documented v2 schema. When the flat fields are
// eventually removed, the migration is to make these views the primary storage
// and delete the flat fields in one breaking change.

// MetaView groups HPA identity fields: namespace, name, target, creation time.
type MetaView struct {
	Namespace         string `json:"namespace" yaml:"namespace"`
	Name              string `json:"name" yaml:"name"`
	Target            string `json:"target" yaml:"target"`
	CreationTimestamp string `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"` // RFC3339 of a.CreationTimestamp
}

// Meta returns the identity group view.
func (a *Analysis) Meta() MetaView {
	ts := ""
	if !a.CreationTimestamp.IsZero() {
		ts = a.CreationTimestamp.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return MetaView{
		Namespace:         a.Namespace,
		Name:              a.Name,
		Target:            a.Target,
		CreationTimestamp: ts,
	}
}

// ReplicasView groups the core scaling envelope.
type ReplicasView struct {
	Current        int32              `json:"currentReplicas" yaml:"currentReplicas"`
	Desired        int32              `json:"desiredReplicas" yaml:"desiredReplicas"`
	Min            int32              `json:"minReplicas" yaml:"minReplicas"`
	Max            int32              `json:"maxReplicas" yaml:"maxReplicas"`
	TargetReplicas *TargetReplicaInfo `json:"targetReplicas,omitempty" yaml:"targetReplicas,omitempty"`
}

// Replicas returns the core scaling-envelope group view.
func (a *Analysis) Replicas() ReplicasView {
	return ReplicasView{
		Current:        a.Current,
		Desired:        a.Desired,
		Min:            a.Min,
		Max:            a.Max,
		TargetReplicas: a.TargetReplicas,
	}
}

// DecisionView groups the "why this replica count" signals.
type DecisionView struct {
	Health                  string                   `json:"health" yaml:"health"`
	HealthScore             int                      `json:"healthScore" yaml:"healthScore"`
	HealthResult            *HealthResult            `json:"healthResult,omitempty" yaml:"healthResult,omitempty"`
	Summary                 string                   `json:"summary" yaml:"summary"`
	SummaryKey              string                   `json:"summaryKey,omitempty" yaml:"summaryKey,omitempty"`
	ImpactMetric            *MetricImpactGuess       `json:"impactMetric,omitempty" yaml:"impactMetric,omitempty"`
	DecisionTrace           *DecisionTrace           `json:"decisionTrace,omitempty" yaml:"decisionTrace,omitempty"`
	MetricDecisionTrace     *MetricDecisionTrace     `json:"metricDecisionTrace,omitempty" yaml:"metricDecisionTrace,omitempty"`
	StructuredDecisionTrace *StructuredDecisionTrace `json:"structuredDecisionTrace,omitempty" yaml:"structuredDecisionTrace,omitempty"`
	DecisionSignals         []DecisionSignal         `json:"decisionSignals,omitempty" yaml:"decisionSignals,omitempty"`
}

// Decision returns the decision/health group view.
func (a *Analysis) Decision() DecisionView {
	return DecisionView{
		Health:                  a.Health,
		HealthScore:             a.HealthScore,
		HealthResult:            a.HealthResult,
		Summary:                 a.Summary,
		SummaryKey:              a.SummaryKey,
		ImpactMetric:            a.ImpactMetric,
		DecisionTrace:           a.DecisionTrace,
		MetricDecisionTrace:     a.MetricDecisionTrace,
		StructuredDecisionTrace: a.StructuredDecisionTrace,
		DecisionSignals:         a.DecisionSignals,
	}
}

// MetricsView groups the metric-pipeline health signals.
type MetricsView struct {
	Metrics            []Metric                    `json:"metrics,omitempty" yaml:"metrics,omitempty"`
	MetricsDiagnostics *MetricsPipelineDiagnostics `json:"metricsDiagnostics,omitempty" yaml:"metricsDiagnostics,omitempty"`
	MetricFreshness    []MetricFreshness           `json:"metricFreshness,omitempty" yaml:"metricFreshness,omitempty"`
	MetricContract     *MetricContractReport       `json:"metricContract,omitempty" yaml:"metricContract,omitempty"`
	MetricHints        *MetricHintsReport          `json:"metricHints,omitempty" yaml:"metricHints,omitempty"`
	AdapterDiagnostics *AdapterDiagnosticsReport   `json:"adapterDiagnostics,omitempty" yaml:"adapterDiagnostics,omitempty"`
}

// MetricsGroup returns the metric-pipeline group view. The method name avoids
// a collision with the existing Metrics field.
func (a *Analysis) MetricsGroup() MetricsView {
	return MetricsView{
		Metrics:            a.Metrics,
		MetricsDiagnostics: a.MetricsDiagnostics,
		MetricFreshness:    a.MetricFreshnessEntries,
		MetricContract:     a.MetricContract,
		MetricHints:        a.MetricHints,
		AdapterDiagnostics: a.AdapterDiagnostics,
	}
}

// ConditionsView groups HPA controller conditions and behavior configuration.
type ConditionsView struct {
	Conditions                 []Condition    `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	Behavior                   []BehaviorRule `json:"behavior,omitempty" yaml:"behavior,omitempty"`
	StabilizationWindowSeconds *int32         `json:"stabilizationWindowSeconds,omitempty" yaml:"stabilizationWindowSeconds,omitempty"`
	StabilizationSource        string         `json:"stabilizationSource,omitempty" yaml:"stabilizationSource,omitempty"`
	StabilizationConfidence    string         `json:"stabilizationConfidence,omitempty" yaml:"stabilizationConfidence,omitempty"`
	StabilizationRemaining     *int64         `json:"stabilizationRemaining,omitempty" yaml:"stabilizationRemaining,omitempty"`
}

// ConditionsGroup returns the conditions/behavior group view.
func (a *Analysis) ConditionsGroup() ConditionsView {
	return ConditionsView{
		Conditions:                 a.Conditions,
		Behavior:                   a.Behavior,
		StabilizationWindowSeconds: a.StabilizationWindowSeconds,
		StabilizationSource:        a.StabilizationSource,
		StabilizationConfidence:    a.StabilizationConfidence,
		StabilizationRemaining:     a.StabilizationRemaining,
	}
}

// ActionsView groups the recommendation/explainability output.
type ActionsView struct {
	Actions                  []string            `json:"recommendedActions,omitempty" yaml:"recommendedActions,omitempty"`
	Suggestions              []Suggestion        `json:"suggestions,omitempty" yaml:"suggestions,omitempty"`
	StructuredActions        []StructuredMessage `json:"structuredActions,omitempty" yaml:"structuredActions,omitempty"`
	StructuredInterpretation []StructuredMessage `json:"structuredInterpretation,omitempty" yaml:"structuredInterpretation,omitempty"`
	Interpretation           []string            `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	Assumptions              []Assumption        `json:"assumptions,omitempty" yaml:"assumptions,omitempty"`
	Warnings                 []string            `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// ActionsGroup returns the recommendations/explainability group view.
func (a *Analysis) ActionsGroup() ActionsView {
	return ActionsView{
		Actions:                  a.Actions,
		Suggestions:              a.Suggestions,
		StructuredActions:        a.StructuredActions,
		StructuredInterpretation: a.StructuredInterpretation,
		Interpretation:           a.Interpretation,
		Assumptions:              a.Assumptions,
		Warnings:                 a.Warnings,
	}
}

// LifecycleView groups freshness/trend/telemetry signals.
type LifecycleView struct {
	StaleStatus      *StaleStatusInfo       `json:"staleStatus,omitempty" yaml:"staleStatus,omitempty"`
	HealthTrend      *healthtrend.Result    `json:"healthTrend,omitempty" yaml:"healthTrend,omitempty"`
	Debug            []string               `json:"debug,omitempty" yaml:"debug,omitempty"`
	HiddenFactors    []HiddenDecisionFactor `json:"hiddenFactors,omitempty" yaml:"hiddenFactors,omitempty"`
	EnrichmentStatus *EnrichmentStatus      `json:"enrichmentStatus,omitempty" yaml:"enrichmentStatus,omitempty"`
}

// Lifecycle returns the freshness/trend group view.
func (a *Analysis) Lifecycle() LifecycleView {
	return LifecycleView{
		StaleStatus:      a.StaleStatus,
		HealthTrend:      a.HealthTrend,
		Debug:            a.Debug,
		HiddenFactors:    a.HiddenFactors,
		EnrichmentStatus: a.EnrichmentStatus,
	}
}

// CapacityView groups scheduling and cluster capacity signals.
type CapacityView struct {
	CapacityContext  *CapacityContext     `json:"capacityContext,omitempty" yaml:"capacityContext,omitempty"`
	CapacityHeadroom *CapacityHeadroom    `json:"capacityHeadroom,omitempty" yaml:"capacityHeadroom,omitempty"`
	CapacityPlan     *CapacityPlan        `json:"capacityPlan,omitempty" yaml:"capacityPlan,omitempty"`
	ResourceCheck    *ResourceCheckResult `json:"resourceCheck,omitempty" yaml:"resourceCheck,omitempty"`
	PodAnalysis      *PodAnalysis         `json:"podAnalysis,omitempty" yaml:"podAnalysis,omitempty"`
	ScalePath        *ScalePath           `json:"scalePath,omitempty" yaml:"scalePath,omitempty"`
	ReadinessImpact  *readiness.Impact    `json:"readinessImpact,omitempty" yaml:"readinessImpact,omitempty"`
}

// Capacity returns the scheduling/capacity group view.
func (a *Analysis) Capacity() CapacityView {
	return CapacityView{
		CapacityContext:  a.CapacityContext,
		CapacityHeadroom: a.CapacityHeadroom,
		CapacityPlan:     a.CapacityPlan,
		ResourceCheck:    a.ResourceCheck,
		PodAnalysis:      a.PodAnalysis,
		ScalePath:        a.ScalePath,
		ReadinessImpact:  a.ReadinessImpact,
	}
}

// ScaleToZeroView groups scale-to-zero and cold-start/warmup signals.
type ScaleToZeroView struct {
	ScaleToZero    *ScaleToZeroInfo `json:"scaleToZero,omitempty" yaml:"scaleToZero,omitempty"`
	WarmupAnalysis *warmup.Analysis `json:"warmupAnalysis,omitempty" yaml:"warmupAnalysis,omitempty"`
}

// ScaleToZeroGroup returns the scale-to-zero/warmup group view. The method
// name avoids a collision with the existing ScaleToZero field.
func (a *Analysis) ScaleToZeroGroup() ScaleToZeroView {
	return ScaleToZeroView{
		ScaleToZero:    a.ScaleToZero,
		WarmupAnalysis: a.WarmupAnalysis,
	}
}

// StabilityView groups flapping and churn diagnosis signals.
type StabilityView struct {
	FlappingSimulation *SimulationResult          `json:"simulation,omitempty" yaml:"simulation,omitempty"`
	FlappingPrevention *flapping.PreventionReport `json:"flappingPrevention,omitempty" yaml:"flappingPrevention,omitempty"`
	FlappingDiagnosis  *flapping.Diagnosis        `json:"flappingDiagnosis,omitempty" yaml:"flappingDiagnosis,omitempty"`
	ChurnAnalysis      *churn.ChurnAnalysis       `json:"churnAnalysis,omitempty" yaml:"churnAnalysis,omitempty"`
}

// Stability returns the flapping/churn group view.
func (a *Analysis) Stability() StabilityView {
	return StabilityView{
		FlappingSimulation: a.FlappingSimulation,
		FlappingPrevention: a.FlappingPrevention,
		FlappingDiagnosis:  a.FlappingDiagnosis,
		ChurnAnalysis:      a.ChurnAnalysis,
	}
}

// AdvisoryView groups VPA and container/behavior tuning advice.
type AdvisoryView struct {
	VPAConflict      *vpa.ConflictInfo        `json:"vpaConflict,omitempty" yaml:"vpaConflict,omitempty"`
	VPAAdvisory      *vpa.Advisory            `json:"vpaAdvisory,omitempty" yaml:"vpaAdvisory,omitempty"`
	ContainerAdvisor *containeradvisor.Result `json:"containerAdvisor,omitempty" yaml:"containerAdvisor,omitempty"`
	BehaviorAdvisor  *behavioradvisor.Result  `json:"behaviorAdvisor,omitempty" yaml:"behaviorAdvisor,omitempty"`
}

// Advisory returns the VPA/container/behavior advisory group view.
func (a *Analysis) Advisory() AdvisoryView {
	return AdvisoryView{
		VPAConflict:      a.VPAConflict,
		VPAAdvisory:      a.VPAAdvisory,
		ContainerAdvisor: a.ContainerAdvisor,
		BehaviorAdvisor:  a.BehaviorAdvisor,
	}
}

// ControllersView groups external controller integrations.
type ControllersView struct {
	KEDAInfo          *keda.Analysis     `json:"keda,omitempty" yaml:"keda,omitempty"`
	RolloutDiagnosis  *RolloutDiagnosis  `json:"rolloutDiagnosis,omitempty" yaml:"rolloutDiagnosis,omitempty"`
	ControllerProfile *ControllerProfile `json:"controllerProfile,omitempty" yaml:"controllerProfile,omitempty"`
}

// Controllers returns the external-controller group view.
func (a *Analysis) Controllers() ControllersView {
	return ControllersView{
		KEDAInfo:          a.KEDAInfo,
		RolloutDiagnosis:  a.RolloutDiagnosis,
		ControllerProfile: a.ControllerProfile,
	}
}

// BlockersView groups apply-time gating signals.
type BlockersView struct {
	BlockerReport  *blocker.Report  `json:"blockerReport,omitempty" yaml:"blockerReport,omitempty"`
	GitOpsConflict *gitops.Conflict `json:"gitOpsConflict,omitempty" yaml:"gitOpsConflict,omitempty"`
}

// Blockers returns the apply-time gating group view.
func (a *Analysis) Blockers() BlockersView {
	return BlockersView{
		BlockerReport:  a.BlockerReport,
		GitOpsConflict: a.GitOpsConflict,
	}
}

// GroupedAnalysis is the nested representation used by the v2 output schema.
// The existing flat Analysis remains the v1 wire contract; serializers can
// consume this value without learning the flat field layout.
type GroupedAnalysis struct {
	Meta        MetaView        `json:"meta" yaml:"meta"`
	Replicas    ReplicasView    `json:"replicas" yaml:"replicas"`
	Decision    DecisionView    `json:"decision" yaml:"decision"`
	Metrics     MetricsView     `json:"metrics" yaml:"metrics"`
	Conditions  ConditionsView  `json:"conditions" yaml:"conditions"`
	Capacity    CapacityView    `json:"capacity" yaml:"capacity"`
	ScaleToZero ScaleToZeroView `json:"scaleToZero" yaml:"scaleToZero"`
	Stability   StabilityView   `json:"stability" yaml:"stability"`
	Advisory    AdvisoryView    `json:"advisory" yaml:"advisory"`
	Controllers ControllersView `json:"controllers" yaml:"controllers"`
	Blockers    BlockersView    `json:"blockers" yaml:"blockers"`
	Actions     ActionsView     `json:"actions" yaml:"actions"`
	Lifecycle   LifecycleView   `json:"lifecycle" yaml:"lifecycle"`
}

// Grouped returns all v2 groups in one stable value. It is intentionally
// additive and does not change v1 JSON/YAML serialization.
func (a *Analysis) Grouped() GroupedAnalysis {
	if a == nil {
		return GroupedAnalysis{}
	}
	return GroupedAnalysis{
		Meta:        a.Meta(),
		Replicas:    a.Replicas(),
		Decision:    a.Decision(),
		Metrics:     a.MetricsGroup(),
		Conditions:  a.ConditionsGroup(),
		Capacity:    a.Capacity(),
		ScaleToZero: a.ScaleToZeroGroup(),
		Stability:   a.Stability(),
		Advisory:    a.Advisory(),
		Controllers: a.Controllers(),
		Blockers:    a.Blockers(),
		Actions:     a.ActionsGroup(),
		Lifecycle:   a.Lifecycle(),
	}
}
