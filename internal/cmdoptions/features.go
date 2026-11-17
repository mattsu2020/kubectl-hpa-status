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

// PresentationPlan groups user-facing explanation and recommendation features.
type PresentationPlan struct{ Interpret, Explain, Suggest, Fix, Recommend, HiddenFactors, ContextForAI bool }

// DepthPlan controls the breadth of API enrichment.
type DepthPlan struct{ Deep, NoEnrich, HPAOnly bool }

// MetricsPlan groups metric diagnosis features.
type MetricsPlan struct{ Diagnose, Freshness, Contract, AdapterDiagnostics, Hints bool }

// WorkloadPlan groups scale-target workload inspection features.
type WorkloadPlan struct{ CheckResources, ExplainPods bool }

// CapacityPlan groups cluster capacity analysis features.
type CapacityPlan struct{ Context, Headroom, Deep, Plan, ScalePath, NodeAutoscaler, Karpenter bool }

// RolloutPlan groups rollout and scale-out blocker features.
type RolloutPlan struct{ Rollout, Impact, ReadinessImpact, ScaleoutBlockers bool }

// DecisionPlan groups controller decision tracing features.
type DecisionPlan struct{ ControllerProfile, Trace, GitOpsCheck bool }

// AdvisorPlan groups optional advisory analyses.
type AdvisorPlan struct{ Churn, Flapping, TrendAnomaly, Container, Behavior bool }

// Plan derives an immutable grouped analysis plan from mutable CLI flags.
func (f Features) Plan() AnalysisPlan {
	plan := AnalysisPlan{
		Presentation: PresentationPlan{
			Interpret:     f.Interpret && !f.NoInterpret,
			Explain:       f.Explain,
			Suggest:       f.Suggest,
			Fix:           f.Fix,
			Recommend:     f.Recommend,
			HiddenFactors: f.HiddenFactors,
			ContextForAI:  f.ContextForAI,
		},
		Depth: DepthPlan{
			Deep:     f.Deep,
			NoEnrich: f.NoEnrich || f.HPAOnly,
			HPAOnly:  f.HPAOnly,
		},
	}
	if plan.Depth.NoEnrich {
		return plan
	}
	plan.Metrics = MetricsPlan{
		Diagnose:           f.DiagnoseMetrics,
		Freshness:          f.MetricsFreshness,
		Contract:           f.MetricContract,
		AdapterDiagnostics: f.AdapterDiagnostics,
		Hints:              f.MetricHints,
	}
	plan.Workload = WorkloadPlan{
		CheckResources: f.CheckResources,
		ExplainPods:    f.ExplainPods,
	}
	plan.Capacity = CapacityPlan{
		Context:        f.CapacityContext,
		Headroom:       f.CapacityHeadroom,
		Deep:           f.CapacityDeep,
		Plan:           f.CapacityPlan,
		ScalePath:      f.ScalePath,
		NodeAutoscaler: f.NodeAutoscaler,
		Karpenter:      f.Karpenter,
	}
	plan.Rollout = RolloutPlan{
		Rollout:          f.Rollout,
		Impact:           f.RolloutImpact,
		ReadinessImpact:  f.ReadinessImpact,
		ScaleoutBlockers: f.ScaleoutBlockers,
	}
	plan.Decision = DecisionPlan{
		ControllerProfile: f.ControllerProfile,
		Trace:             f.DecisionTrace,
		GitOpsCheck:       f.GitOpsCheck,
	}
	plan.Advisors = AdvisorPlan{
		Churn:        f.ChurnDetect,
		Flapping:     f.FlappingAdvisor,
		TrendAnomaly: f.TrendAnomaly,
		Container:    f.ContainerAdvisor,
		Behavior:     f.BehaviorAdvisor,
	}
	return plan
}
