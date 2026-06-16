package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// PipelineContext bundles the shared dependencies enrichers need so each
// Enricher.Run implementation can pull out exactly what it requires without
// bloating the Enricher interface signature.
type PipelineContext struct {
	Client *kube.Client
	EC     *enrichmentContext
	Opts   *options
}

// Enricher is one step of the status-report enrichment pipeline. Enrichers are
// executed in declaration order (see buildStatusEnrichers) and each one may
// decide whether it is enabled for the current options and then either mutate
// the report in place or return an error.
//
// The Enabled method takes no arguments by design: each adapter captures the
// options it needs at construction time (see newXxxEnricher), so the Enricher
// contract no longer depends on the *options type. This keeps the interface
// testable without standing up a full options struct and is the first step
// toward splitting the options God Object into per-feature config subgroups.
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
// given options. Each adapter captures the options fields it reads for its
// Enabled() predicate at this point, so the returned slice is bound to opts.
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
// Each adapter is a thin wrapper around the existing enrichXxx function. The
// adapter captures the options it needs for its Enabled() predicate at
// construction time (newXxxEnricher) and forwards PipelineContext fields to
// the enrichXxx function in Run(). Enrichers that unconditionally run capture
// a constant-true predicate.

type decisionTracesEnricher struct {
	enabled func() bool
}

func newDecisionTracesEnricher(opts *options) Enricher {
	return &decisionTracesEnricher{
		enabled: func() bool { return opts.decisionTrace || opts.decisionTraceFormat != "" },
	}
}

func (*decisionTracesEnricher) Name() string    { return "decision-traces" }
func (e *decisionTracesEnricher) Enabled() bool { return e.enabled() }
func (*decisionTracesEnricher) Run(_ context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichDecisionTraces(p.Opts, hpa, report)
	return nil
}

type eventsEnricher struct {
	enabled func() bool
}

func newEventsEnricher(opts *options) Enricher {
	return &eventsEnricher{
		enabled: func() bool { return opts.events.enabled || opts.flappingAdvisor },
	}
}

func (*eventsEnricher) Name() string    { return "events" }
func (e *eventsEnricher) Enabled() bool { return e.enabled() }
func (*eventsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichEvents(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type reportEnricher struct{}

func newReportEnricher(*options) Enricher { return &reportEnricher{} }

func (*reportEnricher) Name() string  { return "report" }
func (*reportEnricher) Enabled() bool { return true }
func (*reportEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichReport(ctx, p.EC, hpa, report, p.Opts.healthWeights)
	return nil
}

type metricsDiagnosticsEnricher struct {
	enabled func() bool
}

func newMetricsDiagnosticsEnricher(opts *options) Enricher {
	return &metricsDiagnosticsEnricher{
		enabled: func() bool { return opts.diagnoseMetrics },
	}
}

func (*metricsDiagnosticsEnricher) Name() string    { return "metrics-diagnostics" }
func (e *metricsDiagnosticsEnricher) Enabled() bool { return e.enabled() }
func (*metricsDiagnosticsEnricher) Run(_ context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricsDiagnostics(p.Opts, hpa, report)
	return nil
}

type metricFreshnessEnricher struct {
	enabled func() bool
}

func newMetricFreshnessEnricher(opts *options) Enricher {
	return &metricFreshnessEnricher{
		enabled: func() bool { return opts.metricsFreshness },
	}
}

func (*metricFreshnessEnricher) Name() string    { return "metric-freshness" }
func (e *metricFreshnessEnricher) Enabled() bool { return e.enabled() }
func (*metricFreshnessEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricFreshnessReport(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type resourceCheckEnricher struct {
	enabled func() bool
}

func newResourceCheckEnricher(opts *options) Enricher {
	return &resourceCheckEnricher{
		enabled: func() bool { return opts.checkResources },
	}
}

func (*resourceCheckEnricher) Name() string    { return "resource-check" }
func (e *resourceCheckEnricher) Enabled() bool { return e.enabled() }
func (*resourceCheckEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichResourceCheck(ctx, p.Opts, p.Client, hpa, report)
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
		enabled: func() bool { return opts.explainPods },
	}
}

func (*podAnalysisEnricher) Name() string    { return "pod-analysis" }
func (e *podAnalysisEnricher) Enabled() bool { return e.enabled() }
func (*podAnalysisEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichPodAnalysis(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type simulationsEnricher struct {
	enabled func() bool
}

func newSimulationsEnricher(opts *options) Enricher {
	return &simulationsEnricher{
		enabled: func() bool { return len(opts.simulate) > 0 || len(opts.simulateMetric) > 0 },
	}
}

func (*simulationsEnricher) Name() string    { return "simulations" }
func (e *simulationsEnricher) Enabled() bool { return e.enabled() }
func (*simulationsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	return enrichSimulations(ctx, p.Opts, hpa, report)
}

type capacityAnalysisEnricher struct {
	enabled func() bool
}

func newCapacityAnalysisEnricher(opts *options) Enricher {
	return &capacityAnalysisEnricher{
		enabled: func() bool {
			return opts.capacityContext || opts.capacityHeadroom || opts.readinessImpact || opts.scalePath
		},
	}
}

func (*capacityAnalysisEnricher) Name() string    { return "capacity-analysis" }
func (e *capacityAnalysisEnricher) Enabled() bool { return e.enabled() }
func (*capacityAnalysisEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichCapacityAnalysis(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type rolloutAndBlockersEnricher struct {
	enabled func() bool
}

func newRolloutAndBlockersEnricher(opts *options) Enricher {
	return &rolloutAndBlockersEnricher{
		enabled: func() bool {
			return opts.rollout || opts.rolloutImpact || opts.capacityDeep || opts.scaleoutBlockers
		},
	}
}

func (*rolloutAndBlockersEnricher) Name() string    { return "rollout-and-blockers" }
func (e *rolloutAndBlockersEnricher) Enabled() bool { return e.enabled() }
func (*rolloutAndBlockersEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichRolloutAndBlockers(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type controllerProfileEnricher struct {
	enabled func() bool
}

func newControllerProfileEnricher(opts *options) Enricher {
	return &controllerProfileEnricher{
		enabled: func() bool {
			return opts.controllerProfile || opts.assumeProfile != "" || opts.controllerProfileFile != ""
		},
	}
}

func (*controllerProfileEnricher) Name() string    { return "controller-profile" }
func (e *controllerProfileEnricher) Enabled() bool { return e.enabled() }
func (*controllerProfileEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichControllerProfile(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type capacityPlanEnricher struct {
	enabled func() bool
}

func newCapacityPlanEnricher(opts *options) Enricher {
	return &capacityPlanEnricher{
		enabled: func() bool { return opts.capacityPlan },
	}
}

func (*capacityPlanEnricher) Name() string    { return "capacity-plan" }
func (e *capacityPlanEnricher) Enabled() bool { return e.enabled() }
func (*capacityPlanEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichCapacityPlan(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type gitOpsConflictEnricher struct {
	enabled func() bool
}

func newGitOpsConflictEnricher(opts *options) Enricher {
	return &gitOpsConflictEnricher{
		enabled: func() bool { return opts.gitopsCheck || opts.manifestPath != "" },
	}
}

func (*gitOpsConflictEnricher) Name() string    { return "gitops-conflict" }
func (e *gitOpsConflictEnricher) Enabled() bool { return e.enabled() }
func (*gitOpsConflictEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichGitOpsConflict(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type metricContractAndAdapterEnricher struct {
	enabled func() bool
}

func newMetricContractAndAdapterEnricher(opts *options) Enricher {
	return &metricContractAndAdapterEnricher{
		enabled: func() bool { return opts.metricContract || opts.adapterDiagnostics },
	}
}

func (*metricContractAndAdapterEnricher) Name() string    { return "metric-contract-and-adapter" }
func (e *metricContractAndAdapterEnricher) Enabled() bool { return e.enabled() }
func (*metricContractAndAdapterEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricContractAndAdapter(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type churnAndFlappingEnricher struct {
	enabled func() bool
}

func newChurnAndFlappingEnricher(opts *options) Enricher {
	return &churnAndFlappingEnricher{
		enabled: func() bool { return opts.churnDetect || opts.flappingAdvisor },
	}
}

func (*churnAndFlappingEnricher) Name() string    { return "churn-and-flapping" }
func (e *churnAndFlappingEnricher) Enabled() bool { return e.enabled() }
func (*churnAndFlappingEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichChurnAndFlapping(ctx, p.Opts, hpa, report)
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
		enabled: func() bool { return opts.metricHints },
	}
}

func (*metricHintsEnricher) Name() string    { return "metric-hints" }
func (e *metricHintsEnricher) Enabled() bool { return e.enabled() }
func (*metricHintsEnricher) Run(_ context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricHints(p.Opts, hpa, report)
	return nil
}

type advisorsEnricher struct {
	enabled func() bool
}

func newAdvisorsEnricher(opts *options) Enricher {
	return &advisorsEnricher{
		enabled: func() bool { return opts.containerAdvisor || opts.behaviorAdvisor },
	}
}

func (*advisorsEnricher) Name() string    { return "advisors" }
func (e *advisorsEnricher) Enabled() bool { return e.enabled() }
func (*advisorsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichAdvisors(ctx, p.Opts, p.Client, hpa, report)
	return nil
}
