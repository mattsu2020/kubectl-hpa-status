package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// PipelineContext bundles the shared dependencies enrichers need. Opts is
// intentionally absent: each adapter captures the concrete option values it
// needs at construction time (see buildStatusEnrichers) and forwards them as
// plain parameters to the enrichXxx functions. This keeps the enrichment
// pipeline independent of the options God Object.
type PipelineContext struct {
	Client *kube.Client
	EC     *enrichmentContext
}

// Enricher is one step of the status-report enrichment pipeline. Enrichers are
// executed in declaration order (see buildStatusEnrichers) and each one may
// decide whether it is enabled for the current options and then either mutate
// the report in place or return an error.
type Enricher interface {
	// Name is a short, stable identifier used in warning messages.
	Name() string
	// Enabled reports whether this step should run.
	Enabled() bool
	// AbortOnError reports whether a non-nil error from Run should abort the
	// whole pipeline (returning the error to the caller) instead of being
	// recorded as a warning. Most enrichers return false; enrichSimulations
	// returns true to preserve its historical short-circuit behavior.
	AbortOnError() bool
	// Run executes the enrichment step. A non-nil error is recorded into
	// report.Analysis.Warnings by the pipeline runner unless AbortOnError
	// returns true, in which case the error is propagated immediately.
	Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error
}

// genericEnricher is the single adapter implementation backing every step in
// the pipeline. Each step supplies its name, an enabled predicate, and a run
// closure that has already captured the concrete option values it needs. This
// replaces the previous one-type-per-step boilerplate (~19 hand-written struct
// types) while preserving type safety: the closures capture typed values, so
// there is no interface{} / type-assertion escape hatch.
type genericEnricher struct {
	name         string
	enabled      func() bool
	run          func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error
	abortOnError bool
}

func (e *genericEnricher) Name() string       { return e.name }
func (e *genericEnricher) Enabled() bool      { return e.enabled() }
func (e *genericEnricher) AbortOnError() bool { return e.abortOnError }
func (e *genericEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	return e.run(ctx, p, hpa, report)
}

// enricherSpec is the declarative table entry each step registers. The run
// closure captures the option values it needs; the pipeline runner does not
// touch *options after buildStatusEnrichers returns.
type enricherSpec struct {
	name         string
	enabled      func() bool
	run          func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error
	abortOnError bool
}

// buildStatusEnrichers constructs the ordered list of enrichment steps for the
// given options. Steps are grouped into named dependency phases (see
// statusEnricherPhases); flattening phases in order preserves the historical
// sequential semantics.
//
// Cross-phase dependencies (do not reorder phases or steps without review):
//   - report (KEDA/VPA, phase core) must run before vpa-advisory (phase advisors)
//   - metric-freshness, metric-contract-and-adapter, and events must run before
//     metric-hints (phase advisors)
//   - advisors must run before FinalizeAnalysis / health snapshot (caller side)
func buildStatusEnrichers(opts *options) []Enricher {
	return materializeEnrichers(statusEnricherPhases(opts))
}

// statusEnricherPhases returns the ordered dependency buckets that make up the
// status enrichment pipeline. Each phase is a contiguous block of specs; the
// relative order of phases and of steps within a phase is part of the public
// pipeline contract (tests pin the flattened name sequence).
func statusEnricherPhases(opts *options) [][]enricherSpec {
	return [][]enricherSpec{
		// phase core: decision traces, events, and base KEDA/VPA report
		enricherPhaseCore(opts),
		// phase metricsPods: metrics pipeline, resources, pods, simulations
		enricherPhaseMetricsPods(opts),
		// phase capacity: capacity, rollout, blockers, controller profile, plans
		enricherPhaseCapacity(opts),
		// phase advisors: gitops, contracts, churn/flapping, VPA advice, hints, advisors
		enricherPhaseAdvisors(opts),
	}
}

// materializeEnrichers flattens phase specs into Enricher adapters in order.
func materializeEnrichers(phases [][]enricherSpec) []Enricher {
	var n int
	for _, phase := range phases {
		n += len(phase)
	}
	enrichers := make([]Enricher, 0, n)
	for _, phase := range phases {
		for _, s := range phase {
			enrichers = append(enrichers, &genericEnricher{
				name:         s.name,
				enabled:      s.enabled,
				run:          s.run,
				abortOnError: s.abortOnError,
			})
		}
	}
	return enrichers
}

// statusEnricherNames is the canonical flattened order of enricher names.
// Used by tests to pin phase composition without depending on option gates.
var statusEnricherNames = []string{
	// core
	"decision-traces",
	"events",
	"report",
	// metricsPods
	"metrics-diagnostics",
	"metric-freshness",
	"resource-check",
	"target-replica-observations",
	"pod-analysis",
	"simulations",
	// capacity
	"capacity-analysis",
	"rollout-and-blockers",
	"controller-profile",
	"capacity-plan",
	// advisors
	"gitops-conflict",
	"metric-contract-and-adapter",
	"churn-and-flapping",
	"vpa-advisory",
	"metric-hints",
	"advisors",
}

func enricherPhaseCore(opts *options) []enricherSpec {
	return []enricherSpec{
		{
			name:    "decision-traces",
			enabled: func() bool { return opts.DecisionTrace || opts.DecisionTraceFormat != "" },
			run: func(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichDecisionTraces(hpa, report, opts.DecisionTrace, opts.DecisionTraceFormat)
				return nil
			},
		},
		{
			name:    "events",
			enabled: func() bool { return opts.Events.Enabled || opts.FlappingAdvisor },
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichEvents(ctx, p.Client, hpa, report, opts.Events.Limit)
				return nil
			},
		},
		{
			name:    "report",
			enabled: func() bool { return true },
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichReport(ctx, p.EC, hpa, report, opts.HealthWeights)
				return nil
			},
		},
	}
}

func enricherPhaseMetricsPods(opts *options) []enricherSpec {
	return []enricherSpec{
		{
			name:    "metrics-diagnostics",
			enabled: func() bool { return opts.DiagnoseMetrics },
			run: func(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichMetricsDiagnostics(hpa, report)
				return nil
			},
		},
		{
			name:    "metric-freshness",
			enabled: func() bool { return opts.MetricsFreshness },
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichMetricFreshnessReport(ctx, p.Client, hpa, report)
				return nil
			},
		},
		{
			name:    "resource-check",
			enabled: func() bool { return opts.CheckResources },
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichResourceCheck(ctx, p.Client, hpa, report)
				return nil
			},
		},
		{
			// Gated behind the depth-tier flags that actually need it: a plain
			// `status` no longer reads Pods/Deployments, which keeps status fast
			// and usable under restricted RBAC where those reads may be denied.
			name: "target-replica-observations",
			enabled: func() bool {
				return opts.Explain || opts.Interpret || opts.Suggest ||
					opts.ExplainPods || opts.CheckResources ||
					opts.ScalePath || opts.CapacityContext || opts.CapacityHeadroom ||
					opts.CapacityDeep || opts.Rollout || opts.RolloutImpact ||
					opts.ReadinessImpact || opts.Deep
			},
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichTargetReplicaObservations(ctx, p.Client, hpa, report)
				return nil
			},
		},
		{
			name:    "pod-analysis",
			enabled: func() bool { return opts.ExplainPods },
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichPodAnalysis(ctx, p.Client, hpa, report)
				return nil
			},
		},
		{
			name:    "simulations",
			enabled: func() bool { return len(opts.Simulate) > 0 || len(opts.SimulateMetric) > 0 },
			// AbortOnError preserves the historical short-circuit behavior where
			// a simulation error aborts the whole status report instead of being
			// recorded as a best-effort warning.
			abortOnError: true,
			run: func(ctx context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				return enrichSimulations(ctx, hpa, report, SimulationConfig{
					Overrides:       opts.Simulate,
					MetricOverrides: opts.SimulateMetric,
					DurationSeconds: opts.SimulateDuration,
					HealthWeights:   opts.HealthWeights,
					Debug:           opts.Debug,
				})
			},
		},
	}
}

func enricherPhaseCapacity(opts *options) []enricherSpec {
	return []enricherSpec{
		{
			name: "capacity-analysis",
			enabled: func() bool {
				return opts.CapacityContext || opts.CapacityHeadroom || opts.ReadinessImpact || opts.ScalePath
			},
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichCapacityAnalysis(ctx, p.Client, hpa, report, CapacityAnalysisConfig{
					CapacityContext:  opts.CapacityContext,
					CapacityHeadroom: opts.CapacityHeadroom,
					ReadinessImpact:  opts.ReadinessImpact,
					ScalePath:        opts.ScalePath,
				})
				return nil
			},
		},
		{
			name: "rollout-and-blockers",
			enabled: func() bool {
				return opts.Rollout || opts.RolloutImpact || opts.CapacityDeep || opts.ScaleoutBlockers
			},
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichRolloutAndBlockers(ctx, p.Client, hpa, report, RolloutAndBlockersConfig{
					Rollout:          opts.Rollout,
					RolloutImpact:    opts.RolloutImpact,
					CapacityDeep:     opts.CapacityDeep,
					ScaleoutBlockers: opts.ScaleoutBlockers,
				})
				return nil
			},
		},
		{
			name: "controller-profile",
			enabled: func() bool {
				return opts.ControllerProfile || opts.AssumeProfile != "" || opts.ControllerProfileFile != ""
			},
			run: func(ctx context.Context, p *PipelineContext, _ *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				report.Analysis.ControllerProfile = buildControllerProfile(ctx, p.Client, opts.AssumeProfile, opts.ControllerProfileFile)
				return nil
			},
		},
		{
			name:    "capacity-plan",
			enabled: func() bool { return opts.CapacityPlan },
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichCapacityPlan(ctx, p.Client, hpa, report, opts.TargetMax)
				return nil
			},
		},
	}
}

func enricherPhaseAdvisors(opts *options) []enricherSpec {
	return []enricherSpec{
		{
			name:    "gitops-conflict",
			enabled: func() bool { return opts.GitOpsCheck || opts.ManifestPath != "" },
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichGitOpsConflict(ctx, p.Client, hpa, report, opts.ManifestPath)
				return nil
			},
		},
		{
			name:    "metric-contract-and-adapter",
			enabled: func() bool { return opts.MetricContract || opts.AdapterDiagnostics },
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichMetricContractAndAdapter(ctx, p.Client, hpa, report, MetricContractConfig{
					MetricContract:     opts.MetricContract,
					AdapterDiagnostics: opts.AdapterDiagnostics,
				})
				return nil
			},
		},
		{
			name:    "churn-and-flapping",
			enabled: func() bool { return opts.ChurnDetect || opts.FlappingAdvisor },
			run: func(ctx context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichChurnAndFlapping(ctx, hpa, report, ChurnAndFlappingConfig{
					ChurnDetect:     opts.ChurnDetect,
					EventsEnabled:   opts.Events.Enabled,
					FlappingAdvisor: opts.FlappingAdvisor,
					HealthWeights:   opts.HealthWeights,
				})
				return nil
			},
		},
		{
			name:    "vpa-advisory",
			enabled: func() bool { return true },
			run: func(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichVPAAdvisory(hpa, report)
				return nil
			},
		},
		{
			name:    "metric-hints",
			enabled: func() bool { return opts.MetricHints },
			run: func(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichMetricHints(hpa, report)
				return nil
			},
		},
		{
			name:    "advisors",
			enabled: func() bool { return opts.ContainerAdvisor || opts.BehaviorAdvisor },
			run: func(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
				enrichAdvisors(ctx, p.Client, hpa, report, AdvisorsConfig{
					ContainerAdvisor: opts.ContainerAdvisor,
					BehaviorAdvisor:  opts.BehaviorAdvisor,
				})
				return nil
			},
		},
	}
}

// runEnrichers executes each enabled enricher in order. When an enricher
// returns an error, the error is recorded into report.Analysis.Warnings. If
// the enricher's AbortOnError reports true, the error is also returned
// immediately so the caller can abort (matching the historical behavior for
// enrichSimulations).
func runEnrichers(ctx context.Context, enrichers []Enricher, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	for _, e := range enrichers {
		if !e.Enabled() {
			continue
		}
		if err := e.Run(ctx, p, hpa, report); err != nil {
			report.Analysis.Warnings = append(report.Analysis.Warnings,
				fmt.Sprintf("enrichment %q failed: %v", e.Name(), err))
			if e.AbortOnError() {
				return err
			}
		}
	}
	return nil
}
