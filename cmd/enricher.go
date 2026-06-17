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
	// Run executes the enrichment step. A non-nil error is recorded into
	// report.Analysis.Warnings by the pipeline runner. enrichSimulations is the
	// only step whose error aborts the whole pipeline (to preserve prior
	// behavior); see abortOnErrorEnrichers.
	Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error
}

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

// abortOnErrorEnrichers is the set of enricher Names whose error aborts the
// entire buildStatusReport (returning the error to the caller), preserving the
// pre-refactor behavior where enrichSimulations' error short-circuited.
var abortOnErrorEnrichers = map[string]struct{}{
	"simulations": {},
}

// runEnrichers executes each enabled enricher in order. When an enricher
// returns an error, the error is recorded into report.Analysis.Warnings. If
// the enricher's name is in abortOnErrorEnrichers, the error is also returned
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
			if _, abort := abortOnErrorEnrichers[e.Name()]; abort {
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
	enabled             func() bool
	decisionTrace       bool
	decisionTraceFormat string
}

func newDecisionTracesEnricher(opts *options) Enricher {
	return &decisionTracesEnricher{
		enabled:             func() bool { return opts.features.decisionTrace || opts.decisionTraceFormat != "" },
		decisionTrace:       opts.features.decisionTrace,
		decisionTraceFormat: opts.decisionTraceFormat,
	}
}

func (*decisionTracesEnricher) Name() string    { return "decision-traces" }
func (e *decisionTracesEnricher) Enabled() bool { return e.enabled() }
func (e *decisionTracesEnricher) Run(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichDecisionTraces(hpa, report, e.decisionTrace, e.decisionTraceFormat)
	return nil
}

type eventsEnricher struct {
	enabled    func() bool
	eventLimit int
}

func newEventsEnricher(opts *options) Enricher {
	return &eventsEnricher{
		enabled:    func() bool { return opts.events.enabled || opts.features.flappingAdvisor },
		eventLimit: opts.events.limit,
	}
}

func (*eventsEnricher) Name() string    { return "events" }
func (e *eventsEnricher) Enabled() bool { return e.enabled() }
func (e *eventsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichEvents(ctx, p.Client, hpa, report, e.eventLimit)
	return nil
}

type reportEnricher struct {
	healthWeights hpaanalysis.HealthWeights
}

func newReportEnricher(opts *options) Enricher {
	return &reportEnricher{healthWeights: opts.healthWeights}
}

func (*reportEnricher) Name() string  { return "report" }
func (*reportEnricher) Enabled() bool { return true }
func (e *reportEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichReport(ctx, p.EC, hpa, report, e.healthWeights)
	return nil
}

type metricsDiagnosticsEnricher struct {
	enabled func() bool
}

func newMetricsDiagnosticsEnricher(opts *options) Enricher {
	return &metricsDiagnosticsEnricher{
		enabled: func() bool { return opts.features.diagnoseMetrics },
	}
}

func (*metricsDiagnosticsEnricher) Name() string    { return "metrics-diagnostics" }
func (e *metricsDiagnosticsEnricher) Enabled() bool { return e.enabled() }
func (*metricsDiagnosticsEnricher) Run(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricsDiagnostics(hpa, report)
	return nil
}

type metricFreshnessEnricher struct {
	enabled func() bool
}

func newMetricFreshnessEnricher(opts *options) Enricher {
	return &metricFreshnessEnricher{
		enabled: func() bool { return opts.features.metricsFreshness },
	}
}

func (*metricFreshnessEnricher) Name() string    { return "metric-freshness" }
func (e *metricFreshnessEnricher) Enabled() bool { return e.enabled() }
func (e *metricFreshnessEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricFreshnessReport(ctx, p.Client, hpa, report)
	return nil
}

type resourceCheckEnricher struct {
	enabled func() bool
}

func newResourceCheckEnricher(opts *options) Enricher {
	return &resourceCheckEnricher{
		enabled: func() bool { return opts.features.checkResources },
	}
}

func (*resourceCheckEnricher) Name() string    { return "resource-check" }
func (e *resourceCheckEnricher) Enabled() bool { return e.enabled() }
func (e *resourceCheckEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichResourceCheck(ctx, p.Client, hpa, report)
	return nil
}

type targetReplicaObservationsEnricher struct{}

func newTargetReplicaObservationsEnricher(*options) Enricher {
	return &targetReplicaObservationsEnricher{}
}

func (*targetReplicaObservationsEnricher) Name() string  { return "target-replica-observations" }
func (*targetReplicaObservationsEnricher) Enabled() bool { return true }
func (*targetReplicaObservationsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichTargetReplicaObservations(ctx, p.Client, hpa, report)
	return nil
}

type podAnalysisEnricher struct {
	enabled func() bool
}

func newPodAnalysisEnricher(opts *options) Enricher {
	return &podAnalysisEnricher{
		enabled: func() bool { return opts.features.explainPods },
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
		enabled: func() bool { return len(opts.simulate) > 0 || len(opts.simulateMetric) > 0 },
		cfg: SimulationConfig{
			Overrides:       opts.simulate,
			MetricOverrides: opts.simulateMetric,
			DurationSeconds: opts.simulateDuration,
			HealthWeights:   opts.healthWeights,
			Debug:           opts.debug,
		},
	}
}

func (*simulationsEnricher) Name() string    { return "simulations" }
func (e *simulationsEnricher) Enabled() bool { return e.enabled() }
func (e *simulationsEnricher) Run(ctx context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	return enrichSimulations(ctx, hpa, report, e.cfg)
}

type capacityAnalysisEnricher struct {
	enabled          func() bool
	capacityContext  bool
	capacityHeadroom bool
	readinessImpact  bool
	scalePath        bool
}

func newCapacityAnalysisEnricher(opts *options) Enricher {
	return &capacityAnalysisEnricher{
		enabled: func() bool {
			return opts.features.capacityContext || opts.features.capacityHeadroom || opts.features.readinessImpact || opts.features.scalePath
		},
		capacityContext:  opts.features.capacityContext,
		capacityHeadroom: opts.features.capacityHeadroom,
		readinessImpact:  opts.features.readinessImpact,
		scalePath:        opts.features.scalePath,
	}
}

func (*capacityAnalysisEnricher) Name() string    { return "capacity-analysis" }
func (e *capacityAnalysisEnricher) Enabled() bool { return e.enabled() }
func (e *capacityAnalysisEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichCapacityAnalysis(ctx, p.Client, hpa, report, e.capacityContext, e.capacityHeadroom, e.readinessImpact, e.scalePath)
	return nil
}

type rolloutAndBlockersEnricher struct {
	enabled          func() bool
	rollout          bool
	rolloutImpact    bool
	capacityDeep     bool
	scaleoutBlockers bool
}

func newRolloutAndBlockersEnricher(opts *options) Enricher {
	return &rolloutAndBlockersEnricher{
		enabled: func() bool {
			return opts.features.rollout || opts.features.rolloutImpact || opts.features.capacityDeep || opts.features.scaleoutBlockers
		},
		rollout:          opts.features.rollout,
		rolloutImpact:    opts.features.rolloutImpact,
		capacityDeep:     opts.features.capacityDeep,
		scaleoutBlockers: opts.features.scaleoutBlockers,
	}
}

func (*rolloutAndBlockersEnricher) Name() string    { return "rollout-and-blockers" }
func (e *rolloutAndBlockersEnricher) Enabled() bool { return e.enabled() }
func (e *rolloutAndBlockersEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichRolloutAndBlockers(ctx, p.Client, hpa, report, e.rollout, e.rolloutImpact, e.capacityDeep, e.scaleoutBlockers)
	return nil
}

type controllerProfileEnricher struct {
	enabled       func() bool
	assumeProfile string
	profileFile   string
}

func newControllerProfileEnricher(opts *options) Enricher {
	return &controllerProfileEnricher{
		enabled: func() bool {
			return opts.features.controllerProfile || opts.assumeProfile != "" || opts.controllerProfileFile != ""
		},
		assumeProfile: opts.assumeProfile,
		profileFile:   opts.controllerProfileFile,
	}
}

func (*controllerProfileEnricher) Name() string    { return "controller-profile" }
func (e *controllerProfileEnricher) Enabled() bool { return e.enabled() }
func (e *controllerProfileEnricher) Run(ctx context.Context, p *PipelineContext, _ *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	report.Analysis.ControllerProfile = buildControllerProfile(ctx, p.Client, e.assumeProfile, e.profileFile)
	return nil
}

type capacityPlanEnricher struct {
	enabled   func() bool
	targetMax int32
}

func newCapacityPlanEnricher(opts *options) Enricher {
	return &capacityPlanEnricher{
		enabled:   func() bool { return opts.features.capacityPlan },
		targetMax: opts.targetMax,
	}
}

func (*capacityPlanEnricher) Name() string    { return "capacity-plan" }
func (e *capacityPlanEnricher) Enabled() bool { return e.enabled() }
func (e *capacityPlanEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichCapacityPlan(ctx, p.Client, hpa, report, e.targetMax)
	return nil
}

type gitOpsConflictEnricher struct {
	enabled      func() bool
	manifestPath string
}

func newGitOpsConflictEnricher(opts *options) Enricher {
	return &gitOpsConflictEnricher{
		enabled:      func() bool { return opts.features.gitopsCheck || opts.manifestPath != "" },
		manifestPath: opts.manifestPath,
	}
}

func (*gitOpsConflictEnricher) Name() string    { return "gitops-conflict" }
func (e *gitOpsConflictEnricher) Enabled() bool { return e.enabled() }
func (e *gitOpsConflictEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichGitOpsConflict(ctx, p.Client, hpa, report, e.manifestPath)
	return nil
}

type metricContractAndAdapterEnricher struct {
	enabled            func() bool
	metricContract     bool
	adapterDiagnostics bool
}

func newMetricContractAndAdapterEnricher(opts *options) Enricher {
	return &metricContractAndAdapterEnricher{
		enabled:            func() bool { return opts.features.metricContract || opts.features.adapterDiagnostics },
		metricContract:     opts.features.metricContract,
		adapterDiagnostics: opts.features.adapterDiagnostics,
	}
}

func (*metricContractAndAdapterEnricher) Name() string    { return "metric-contract-and-adapter" }
func (e *metricContractAndAdapterEnricher) Enabled() bool { return e.enabled() }
func (e *metricContractAndAdapterEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricContractAndAdapter(ctx, p.Client, hpa, report, e.metricContract, e.adapterDiagnostics)
	return nil
}

type churnAndFlappingEnricher struct {
	enabled         func() bool
	churnDetect     bool
	eventsEnabled   bool
	flappingAdvisor bool
	healthWeights   hpaanalysis.HealthWeights
}

func newChurnAndFlappingEnricher(opts *options) Enricher {
	return &churnAndFlappingEnricher{
		enabled:         func() bool { return opts.features.churnDetect || opts.features.flappingAdvisor },
		churnDetect:     opts.features.churnDetect,
		eventsEnabled:   opts.events.enabled,
		flappingAdvisor: opts.features.flappingAdvisor,
		healthWeights:   opts.healthWeights,
	}
}

func (*churnAndFlappingEnricher) Name() string    { return "churn-and-flapping" }
func (e *churnAndFlappingEnricher) Enabled() bool { return e.enabled() }
func (e *churnAndFlappingEnricher) Run(ctx context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichChurnAndFlapping(ctx, hpa, report, e.churnDetect, e.eventsEnabled, e.flappingAdvisor, e.healthWeights)
	return nil
}

type vpaAdvisoryEnricher struct{}

func newVPAAdvisoryEnricher(*options) Enricher { return &vpaAdvisoryEnricher{} }

func (*vpaAdvisoryEnricher) Name() string  { return "vpa-advisory" }
func (*vpaAdvisoryEnricher) Enabled() bool { return true }
func (*vpaAdvisoryEnricher) Run(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichVPAAdvisory(hpa, report)
	return nil
}

type metricHintsEnricher struct {
	enabled func() bool
}

func newMetricHintsEnricher(opts *options) Enricher {
	return &metricHintsEnricher{
		enabled: func() bool { return opts.features.metricHints },
	}
}

func (*metricHintsEnricher) Name() string    { return "metric-hints" }
func (e *metricHintsEnricher) Enabled() bool { return e.enabled() }
func (e *metricHintsEnricher) Run(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricHints(hpa, report)
	return nil
}

type advisorsEnricher struct {
	enabled          func() bool
	containerAdvisor bool
	behaviorAdvisor  bool
}

func newAdvisorsEnricher(opts *options) Enricher {
	return &advisorsEnricher{
		enabled:          func() bool { return opts.features.containerAdvisor || opts.features.behaviorAdvisor },
		containerAdvisor: opts.features.containerAdvisor,
		behaviorAdvisor:  opts.features.behaviorAdvisor,
	}
}

func (*advisorsEnricher) Name() string    { return "advisors" }
func (e *advisorsEnricher) Enabled() bool { return e.enabled() }
func (e *advisorsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichAdvisors(ctx, p.Client, hpa, report, e.containerAdvisor, e.behaviorAdvisor)
	return nil
}
