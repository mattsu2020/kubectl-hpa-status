package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/gitops"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/warmup"
)

// This file provides additive, read-only "group views" over the flat Analysis
// struct, implementing the first step of the v2 grouping migration tracked in
// ROADMAP.md ("Migration strategy (additive)"). The flat fields and their JSON
// tags are unchanged, preserving the existing serialization contract. New code
// can reach related fields through these view methods so the logical grouping
// is expressed in code even before the flat fields are retired at a future
// major version.
//
// Each view is a plain value struct (no methods, no mutation) returned by a
// method on *Analysis. The views are snapshots: they copy scalar/struct values
// and share pointer/slice backing arrays (read-only by convention). Callers
// must not mutate the returned views' slice/map fields.
//
// The groups match the ROADMAP "Proposed v2 grouping" table. When the flat
// fields are eventually removed, the migration is to make these views the
// primary storage and delete the flat fields in one breaking change.

// MetaView groups HPA identity fields: namespace, name, target, creation time.
type MetaView struct {
	Namespace         string
	Name              string
	Target            string
	CreationTimestamp string // RFC3339 of a.CreationTimestamp
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
	Current        int32
	Desired        int32
	Min            int32
	Max            int32
	TargetReplicas *TargetReplicaInfo
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
	Health                  string
	HealthScore             int
	HealthResult            *HealthResult
	Summary                 string
	SummaryKey              string
	ImpactMetric            *MetricImpactGuess
	DecisionTrace           *DecisionTrace
	MetricDecisionTrace     *MetricDecisionTrace
	StructuredDecisionTrace *StructuredDecisionTrace
	DecisionSignals         []DecisionSignal
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
	Metrics            []Metric
	MetricsDiagnostics *MetricsPipelineDiagnostics
	MetricFreshness    []MetricFreshness
	MetricContract     *MetricContractReport
	MetricHints        *MetricHintsReport
	AdapterDiagnostics *AdapterDiagnosticsReport
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
	Conditions                 []Condition
	Behavior                   []BehaviorRule
	StabilizationWindowSeconds *int32
	StabilizationSource        string
	StabilizationConfidence    string
	StabilizationRemaining     *int64
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
	Actions                  []string
	Suggestions              []Suggestion
	StructuredActions        []StructuredMessage
	StructuredInterpretation []StructuredMessage
	Interpretation           []string
	Assumptions              []Assumption
	Warnings                 []string
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
	StaleStatus      *StaleStatusInfo
	HealthTrend      *HealthTrendResult
	Debug            []string
	HiddenFactors    []HiddenDecisionFactor
	EnrichmentStatus *EnrichmentStatus
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
	CapacityContext  *CapacityContext
	CapacityHeadroom *CapacityHeadroom
	CapacityPlan     *CapacityPlan
	ResourceCheck    *ResourceCheckResult
	PodAnalysis      *PodAnalysis
	ScalePath        *ScalePath
	ReadinessImpact  *ReadinessImpact
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
	ScaleToZero    *ScaleToZeroInfo
	WarmupAnalysis *warmup.Analysis
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
	FlappingSimulation *SimulationResult
	FlappingPrevention *FlappingPreventionReport
	FlappingDiagnosis  *FlappingDiagnosis
	ChurnAnalysis      *ChurnAnalysis
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
	VPAConflict      *vpa.ConflictInfo
	VPAAdvisory      *vpa.Advisory
	ContainerAdvisor *ContainerAdvisorResult
	BehaviorAdvisor  *BehaviorAdvisorResult
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
	KEDAInfo          *KEDAAnalysis
	RolloutDiagnosis  *RolloutDiagnosis
	ControllerProfile *ControllerProfile
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
	BlockerReport  *blocker.Report
	GitOpsConflict *gitops.Conflict
}

// Blockers returns the apply-time gating group view.
func (a *Analysis) Blockers() BlockersView {
	return BlockersView{
		BlockerReport:  a.BlockerReport,
		GitOpsConflict: a.GitOpsConflict,
	}
}

// GroupedAnalysis is the additive nested representation used as the migration
// boundary for the future v2 output schema. The existing flat Analysis remains
// the v1 wire contract; new serializers can consume this value without
// learning the 65-field layout.
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
