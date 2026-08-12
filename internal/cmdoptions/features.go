package cmdoptions

// Features groups enrichment and analysis boolean toggles for the status
// workflow. Fields stay flat because named composite literals are part of the
// internal test and command contract; AnalysisPlan provides the grouped,
// immutable execution view.
type Features struct {
	Interpret, NoInterpret, Explain, Suggest, Fix, Recommend, HiddenFactors, ContextForAI               bool
	Deep, NoEnrich, HPAOnly                                                                             bool
	DiagnoseMetrics, MetricsFreshness, MetricContract, AdapterDiagnostics, MetricHints                  bool
	CheckResources, ExplainPods                                                                         bool
	CapacityContext, CapacityHeadroom, CapacityDeep, CapacityPlan, ScalePath, NodeAutoscaler, Karpenter bool
	Rollout, RolloutImpact, ReadinessImpact, ScaleoutBlockers                                           bool
	ControllerProfile, DecisionTrace                                                                    bool
	GitOpsCheck                                                                                         bool
	ChurnDetect, FlappingAdvisor, TrendAnomaly, ContainerAdvisor, BehaviorAdvisor                       bool
}

// AnalysisPlan is the immutable, domain-grouped execution plan derived from
// mutable CLI feature flags at the command boundary.
type AnalysisPlan struct {
	Presentation PresentationPlan
	Depth        DepthPlan
	Metrics      MetricsPlan
	Workload     WorkloadPlan
	Capacity     CapacityPlan
	Rollout      RolloutPlan
	Decision     DecisionPlan
	Advisors     AdvisorPlan
}
type PresentationPlan struct{ Interpret, Explain, Suggest, Fix, Recommend, HiddenFactors, ContextForAI bool }
type DepthPlan struct{ Deep, NoEnrich, HPAOnly bool }
type MetricsPlan struct{ Diagnose, Freshness, Contract, AdapterDiagnostics, Hints bool }
type WorkloadPlan struct{ CheckResources, ExplainPods bool }
type CapacityPlan struct{ Context, Headroom, Deep, Plan, ScalePath, NodeAutoscaler, Karpenter bool }
type RolloutPlan struct{ Rollout, Impact, ReadinessImpact, ScaleoutBlockers bool }
type DecisionPlan struct{ ControllerProfile, Trace, GitOpsCheck bool }
type AdvisorPlan struct{ Churn, Flapping, TrendAnomaly, Container, Behavior bool }

func (f Features) Plan() AnalysisPlan {
	return AnalysisPlan{
		Presentation: PresentationPlan{f.Interpret && !f.NoInterpret, f.Explain, f.Suggest, f.Fix, f.Recommend, f.HiddenFactors, f.ContextForAI},
		Depth:        DepthPlan{f.Deep, f.NoEnrich, f.HPAOnly},
		Metrics:      MetricsPlan{f.DiagnoseMetrics, f.MetricsFreshness, f.MetricContract, f.AdapterDiagnostics, f.MetricHints},
		Workload:     WorkloadPlan{f.CheckResources, f.ExplainPods},
		Capacity:     CapacityPlan{f.CapacityContext, f.CapacityHeadroom, f.CapacityDeep, f.CapacityPlan, f.ScalePath, f.NodeAutoscaler, f.Karpenter},
		Rollout:      RolloutPlan{f.Rollout, f.RolloutImpact, f.ReadinessImpact, f.ScaleoutBlockers},
		Decision:     DecisionPlan{f.ControllerProfile, f.DecisionTrace, f.GitOpsCheck},
		Advisors:     AdvisorPlan{f.ChurnDetect, f.FlappingAdvisor, f.TrendAnomaly, f.ContainerAdvisor, f.BehaviorAdvisor},
	}
}
