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
// needs at construction time (see newXxxEnricher) and forwards them as plain
// parameters to the enrichXxx functions. This keeps the enrichment pipeline
// independent of the options God Object.
type PipelineContext struct {
	Client *kube.Client
	EC     *enrichmentContext
}

// Enricher is one step of the status-report enrichment pipeline. Enrichers are
// executed in declaration order (see buildStatusEnrichers) and each one may
// decide whether it is enabled for the current options and then either mutate
// the report in place or return an error.
//
// Neither Enabled nor Run depends on *options: each adapter captures the
// options fields it needs inside its newXxxEnricher constructor. This keeps
// the interface testable without standing up a full options struct and is the
// foundation for splitting options into per-feature config subgroups.
type Enricher interface {
	// Name is a short, stable identifier used in warning messages.
	Name() string
	// Enabled reports whether this step should run. The predicate is captured
	// from options at construction; callers pass nothing here.
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

// defaultAbort is embedded by enricher adapters whose errors should be
// recorded as warnings rather than aborting the whole pipeline. Only adapters
// that need to short-circuit (simulations) override AbortOnError.
type defaultAbort struct{}

func (defaultAbort) AbortOnError() bool { return false }

// buildStatusEnrichers constructs the ordered list of enrichment steps for the
// given options. Each adapter captures the options fields it needs for both its
// Enabled() predicate and Run() body at this point, so the returned slice is
// bound to opts.
//
// The order matches the original sequential calls exactly because several
// enrichers depend on fields populated by earlier steps:
//   - enrichReport (KEDA/VPA) must run before enrichVPAAdvisory.
//   - enrichMetricFreshnessReport, enrichMetricContractAndAdapter and
//     enrichEvents must run before enrichMetricHints.
//   - enrichAdvisors must run before FinalizeAnalysis and the health snapshot.
//
// Do not reorder without re-reading buildStatusReport's dependency comments.
func buildStatusEnrichers(opts *options) []Enricher {
	return []Enricher{
		newDecisionTracesEnricher(opts),
		newEventsEnricher(opts),
		newReportEnricher(opts),
		newMetricsDiagnosticsEnricher(opts),
		newMetricFreshnessEnricher(opts),
		newResourceCheckEnricher(opts),
		newTargetReplicaObservationsEnricher(opts),
		newPodAnalysisEnricher(opts),
		newSimulationsEnricher(opts),
		newCapacityAnalysisEnricher(opts),
		newRolloutAndBlockersEnricher(opts),
		newControllerProfileEnricher(opts),
		newCapacityPlanEnricher(opts),
		newGitOpsConflictEnricher(opts),
		newMetricContractAndAdapterEnricher(opts),
		newChurnAndFlappingEnricher(opts),
		newVPAAdvisoryEnricher(opts),
		newMetricHintsEnricher(opts),
		newAdvisorsEnricher(opts),
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

// --- Adapter types ---
//
// Each adapter captures the options fields it needs (both for the Enabled()
// predicate and for the Run() body) at construction time. Run forwards the
// captured values plus the PipelineContext (Client/EC) to the enrichXxx
// function. No adapter touches *options after construction.

type decisionTracesEnricher struct {
	defaultAbort
	enabled             func() bool
	decisionTrace       bool
	decisionTraceFormat string
}

func newDecisionTracesEnricher(opts *options) Enricher {
	return &decisionTracesEnricher{
		enabled:             func() bool { return opts.DecisionTrace || opts.DecisionTraceFormat != "" },
		decisionTrace:       opts.DecisionTrace,
		decisionTraceFormat: opts.DecisionTraceFormat,
	}
}

func (*decisionTracesEnricher) Name() string    { return "decision-traces" }
func (e *decisionTracesEnricher) Enabled() bool { return e.enabled() }
func (e *decisionTracesEnricher) Run(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichDecisionTraces(hpa, report, e.decisionTrace, e.decisionTraceFormat)
	return nil
}

type eventsEnricher struct {
	defaultAbort
	enabled    func() bool
	eventLimit int
}

func newEventsEnricher(opts *options) Enricher {
	return &eventsEnricher{
		enabled:    func() bool { return opts.Events.Enabled || opts.FlappingAdvisor },
		eventLimit: opts.Events.Limit,
	}
}

func (*eventsEnricher) Name() string    { return "events" }
func (e *eventsEnricher) Enabled() bool { return e.enabled() }
func (e *eventsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichEvents(ctx, p.Client, hpa, report, e.eventLimit)
	return nil
}

type reportEnricher struct {
	defaultAbort
	healthWeights hpaanalysis.HealthWeights
}

func newReportEnricher(opts *options) Enricher {
	return &reportEnricher{healthWeights: opts.HealthWeights}
}

func (*reportEnricher) Name() string  { return "report" }
func (*reportEnricher) Enabled() bool { return true }
func (e *reportEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichReport(ctx, p.EC, hpa, report, e.healthWeights)
	return nil
}

type metricsDiagnosticsEnricher struct {
	defaultAbort
	enabled func() bool
}

func newMetricsDiagnosticsEnricher(opts *options) Enricher {
	return &metricsDiagnosticsEnricher{
		enabled: func() bool { return opts.DiagnoseMetrics },
	}
}

func (*metricsDiagnosticsEnricher) Name() string    { return "metrics-diagnostics" }
func (e *metricsDiagnosticsEnricher) Enabled() bool { return e.enabled() }
func (*metricsDiagnosticsEnricher) Run(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricsDiagnostics(hpa, report)
	return nil
}

type metricFreshnessEnricher struct {
	defaultAbort
	enabled func() bool
}

func newMetricFreshnessEnricher(opts *options) Enricher {
	return &metricFreshnessEnricher{
		enabled: func() bool { return opts.MetricsFreshness },
	}
}

func (*metricFreshnessEnricher) Name() string    { return "metric-freshness" }
func (e *metricFreshnessEnricher) Enabled() bool { return e.enabled() }
func (e *metricFreshnessEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricFreshnessReport(ctx, p.Client, hpa, report)
	return nil
}

type resourceCheckEnricher struct {
	defaultAbort
	enabled func() bool
}

func newResourceCheckEnricher(opts *options) Enricher {
	return &resourceCheckEnricher{
		enabled: func() bool { return opts.CheckResources },
	}
}

func (*resourceCheckEnricher) Name() string    { return "resource-check" }
func (e *resourceCheckEnricher) Enabled() bool { return e.enabled() }
func (e *resourceCheckEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichResourceCheck(ctx, p.Client, hpa, report)
	return nil
}

type targetReplicaObservationsEnricher struct {
	defaultAbort
	enabled func() bool
}

// newTargetReplicaObservationsEnricher gates the scale-target pod read behind
// the depth-tier flags that actually need it. A plain `status` no longer reads
// Pods/Deployments, which keeps status fast and usable under restricted RBAC
// where those reads may be denied. The enricher is enabled when any of its
// consumers is on:
//   - --explain / --interpret / --suggest: the explanation references
//     not-ready / pending target pods;
//   - --explain-pods / --check-resources: workload-level inspection;
//   - --scale-path / --capacity-* / --rollout* / --readiness-impact: the
//     capacity and rollout enrichers read report.Analysis.TargetReplicas;
//   - --deep: the one-flag depth tier pulls in all of the above.
func newTargetReplicaObservationsEnricher(opts *options) Enricher {
	return &targetReplicaObservationsEnricher{
		enabled: func() bool {
			return opts.Explain || opts.Interpret || opts.Suggest ||
				opts.ExplainPods || opts.CheckResources ||
				opts.ScalePath || opts.CapacityContext || opts.CapacityHeadroom ||
				opts.CapacityDeep || opts.Rollout || opts.RolloutImpact ||
				opts.ReadinessImpact || opts.Deep
		},
	}
}

func (*targetReplicaObservationsEnricher) Name() string    { return "target-replica-observations" }
func (e *targetReplicaObservationsEnricher) Enabled() bool { return e.enabled() }
func (*targetReplicaObservationsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichTargetReplicaObservations(ctx, p.Client, hpa, report)
	return nil
}

type podAnalysisEnricher struct {
	defaultAbort
	enabled func() bool
}

func newPodAnalysisEnricher(opts *options) Enricher {
	return &podAnalysisEnricher{
		enabled: func() bool { return opts.ExplainPods },
	}
}

func (*podAnalysisEnricher) Name() string    { return "pod-analysis" }
func (e *podAnalysisEnricher) Enabled() bool { return e.enabled() }
func (e *podAnalysisEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichPodAnalysis(ctx, p.Client, hpa, report)
	return nil
}

type simulationsEnricher struct {
	enabled func() bool
	cfg     SimulationConfig
}

func newSimulationsEnricher(opts *options) Enricher {
	return &simulationsEnricher{
		enabled: func() bool { return len(opts.Simulate) > 0 || len(opts.SimulateMetric) > 0 },
		cfg: SimulationConfig{
			Overrides:       opts.Simulate,
			MetricOverrides: opts.SimulateMetric,
			DurationSeconds: opts.SimulateDuration,
			HealthWeights:   opts.HealthWeights,
			Debug:           opts.Debug,
		},
	}
}

func (*simulationsEnricher) Name() string    { return "simulations" }
func (e *simulationsEnricher) Enabled() bool { return e.enabled() }

// AbortOnError preserves the historical short-circuit behavior where a
// simulation error aborts the whole status report instead of being recorded
// as a best-effort warning.
func (*simulationsEnricher) AbortOnError() bool { return true }
func (e *simulationsEnricher) Run(ctx context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	return enrichSimulations(ctx, hpa, report, e.cfg)
}

type capacityAnalysisEnricher struct {
	defaultAbort
	enabled          func() bool
	capacityContext  bool
	capacityHeadroom bool
	readinessImpact  bool
	scalePath        bool
}

func newCapacityAnalysisEnricher(opts *options) Enricher {
	return &capacityAnalysisEnricher{
		enabled: func() bool {
			return opts.CapacityContext || opts.CapacityHeadroom || opts.ReadinessImpact || opts.ScalePath
		},
		capacityContext:  opts.CapacityContext,
		capacityHeadroom: opts.CapacityHeadroom,
		readinessImpact:  opts.ReadinessImpact,
		scalePath:        opts.ScalePath,
	}
}

func (*capacityAnalysisEnricher) Name() string    { return "capacity-analysis" }
func (e *capacityAnalysisEnricher) Enabled() bool { return e.enabled() }
func (e *capacityAnalysisEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichCapacityAnalysis(ctx, p.Client, hpa, report, CapacityAnalysisConfig{
		CapacityContext:  e.capacityContext,
		CapacityHeadroom: e.capacityHeadroom,
		ReadinessImpact:  e.readinessImpact,
		ScalePath:        e.scalePath,
	})
	return nil
}

type rolloutAndBlockersEnricher struct {
	defaultAbort
	enabled          func() bool
	rollout          bool
	rolloutImpact    bool
	capacityDeep     bool
	scaleoutBlockers bool
}

func newRolloutAndBlockersEnricher(opts *options) Enricher {
	return &rolloutAndBlockersEnricher{
		enabled: func() bool {
			return opts.Rollout || opts.RolloutImpact || opts.CapacityDeep || opts.ScaleoutBlockers
		},
		rollout:          opts.Rollout,
		rolloutImpact:    opts.RolloutImpact,
		capacityDeep:     opts.CapacityDeep,
		scaleoutBlockers: opts.ScaleoutBlockers,
	}
}

func (*rolloutAndBlockersEnricher) Name() string    { return "rollout-and-blockers" }
func (e *rolloutAndBlockersEnricher) Enabled() bool { return e.enabled() }
func (e *rolloutAndBlockersEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichRolloutAndBlockers(ctx, p.Client, hpa, report, RolloutAndBlockersConfig{
		Rollout:          e.rollout,
		RolloutImpact:    e.rolloutImpact,
		CapacityDeep:     e.capacityDeep,
		ScaleoutBlockers: e.scaleoutBlockers,
	})
	return nil
}

type controllerProfileEnricher struct {
	defaultAbort
	enabled       func() bool
	assumeProfile string
	profileFile   string
}

func newControllerProfileEnricher(opts *options) Enricher {
	return &controllerProfileEnricher{
		enabled: func() bool {
			return opts.ControllerProfile || opts.AssumeProfile != "" || opts.ControllerProfileFile != ""
		},
		assumeProfile: opts.AssumeProfile,
		profileFile:   opts.ControllerProfileFile,
	}
}

func (*controllerProfileEnricher) Name() string    { return "controller-profile" }
func (e *controllerProfileEnricher) Enabled() bool { return e.enabled() }
func (e *controllerProfileEnricher) Run(ctx context.Context, p *PipelineContext, _ *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	report.Analysis.ControllerProfile = buildControllerProfile(ctx, p.Client, e.assumeProfile, e.profileFile)
	return nil
}

type capacityPlanEnricher struct {
	defaultAbort
	enabled   func() bool
	targetMax int32
}

func newCapacityPlanEnricher(opts *options) Enricher {
	return &capacityPlanEnricher{
		enabled:   func() bool { return opts.CapacityPlan },
		targetMax: opts.TargetMax,
	}
}

func (*capacityPlanEnricher) Name() string    { return "capacity-plan" }
func (e *capacityPlanEnricher) Enabled() bool { return e.enabled() }
func (e *capacityPlanEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichCapacityPlan(ctx, p.Client, hpa, report, e.targetMax)
	return nil
}

type gitOpsConflictEnricher struct {
	defaultAbort
	enabled      func() bool
	manifestPath string
}

func newGitOpsConflictEnricher(opts *options) Enricher {
	return &gitOpsConflictEnricher{
		enabled:      func() bool { return opts.GitOpsCheck || opts.ManifestPath != "" },
		manifestPath: opts.ManifestPath,
	}
}

func (*gitOpsConflictEnricher) Name() string    { return "gitops-conflict" }
func (e *gitOpsConflictEnricher) Enabled() bool { return e.enabled() }
func (e *gitOpsConflictEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichGitOpsConflict(ctx, p.Client, hpa, report, e.manifestPath)
	return nil
}

type metricContractAndAdapterEnricher struct {
	defaultAbort
	enabled            func() bool
	metricContract     bool
	adapterDiagnostics bool
}

func newMetricContractAndAdapterEnricher(opts *options) Enricher {
	return &metricContractAndAdapterEnricher{
		enabled:            func() bool { return opts.MetricContract || opts.AdapterDiagnostics },
		metricContract:     opts.MetricContract,
		adapterDiagnostics: opts.AdapterDiagnostics,
	}
}

func (*metricContractAndAdapterEnricher) Name() string    { return "metric-contract-and-adapter" }
func (e *metricContractAndAdapterEnricher) Enabled() bool { return e.enabled() }
func (e *metricContractAndAdapterEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricContractAndAdapter(ctx, p.Client, hpa, report, MetricContractConfig{
		MetricContract:     e.metricContract,
		AdapterDiagnostics: e.adapterDiagnostics,
	})
	return nil
}

type churnAndFlappingEnricher struct {
	defaultAbort
	enabled         func() bool
	churnDetect     bool
	eventsEnabled   bool
	flappingAdvisor bool
	healthWeights   hpaanalysis.HealthWeights
}

func newChurnAndFlappingEnricher(opts *options) Enricher {
	return &churnAndFlappingEnricher{
		enabled:         func() bool { return opts.ChurnDetect || opts.FlappingAdvisor },
		churnDetect:     opts.ChurnDetect,
		eventsEnabled:   opts.Events.Enabled,
		flappingAdvisor: opts.FlappingAdvisor,
		healthWeights:   opts.HealthWeights,
	}
}

func (*churnAndFlappingEnricher) Name() string    { return "churn-and-flapping" }
func (e *churnAndFlappingEnricher) Enabled() bool { return e.enabled() }
func (e *churnAndFlappingEnricher) Run(ctx context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichChurnAndFlapping(ctx, hpa, report, ChurnAndFlappingConfig{
		ChurnDetect:     e.churnDetect,
		EventsEnabled:   e.eventsEnabled,
		FlappingAdvisor: e.flappingAdvisor,
		HealthWeights:   e.healthWeights,
	})
	return nil
}

type vpaAdvisoryEnricher struct{ defaultAbort }

func newVPAAdvisoryEnricher(*options) Enricher { return &vpaAdvisoryEnricher{} }

func (*vpaAdvisoryEnricher) Name() string  { return "vpa-advisory" }
func (*vpaAdvisoryEnricher) Enabled() bool { return true }
func (*vpaAdvisoryEnricher) Run(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichVPAAdvisory(hpa, report)
	return nil
}

type metricHintsEnricher struct {
	defaultAbort
	enabled func() bool
}

func newMetricHintsEnricher(opts *options) Enricher {
	return &metricHintsEnricher{
		enabled: func() bool { return opts.MetricHints },
	}
}

func (*metricHintsEnricher) Name() string    { return "metric-hints" }
func (e *metricHintsEnricher) Enabled() bool { return e.enabled() }
func (e *metricHintsEnricher) Run(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricHints(hpa, report)
	return nil
}

type advisorsEnricher struct {
	defaultAbort
	enabled          func() bool
	containerAdvisor bool
	behaviorAdvisor  bool
}

func newAdvisorsEnricher(opts *options) Enricher {
	return &advisorsEnricher{
		enabled:          func() bool { return opts.ContainerAdvisor || opts.BehaviorAdvisor },
		containerAdvisor: opts.ContainerAdvisor,
		behaviorAdvisor:  opts.BehaviorAdvisor,
	}
}

func (*advisorsEnricher) Name() string    { return "advisors" }
func (e *advisorsEnricher) Enabled() bool { return e.enabled() }
func (e *advisorsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichAdvisors(ctx, p.Client, hpa, report, AdvisorsConfig{
		ContainerAdvisor: e.containerAdvisor,
		BehaviorAdvisor:  e.behaviorAdvisor,
	})
	return nil
}
