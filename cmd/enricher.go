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
// executed in declaration order (see statusEnrichers) and each one may decide
// whether it is enabled for the current options and then either mutate the
// report in place or return an error.
type Enricher interface {
	// Name is a short, stable identifier used in warning messages.
	Name() string
	// Enabled reports whether this step should run for the given options.
	Enabled(opts *options) bool
	// Run executes the enrichment step. A non-nil error is recorded into
	// report.Analysis.Warnings by the pipeline runner. enrichSimulations is the
	// only step whose error aborts the whole pipeline (to preserve prior
	// behavior); see abortOnErrorEnrichers.
	Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error
}

// statusEnrichers is the ordered list of enrichment steps executed by
// buildStatusReport. The order matches the original sequential calls exactly
// because several enrichers depend on fields populated by earlier steps:
//   - enrichReport (KEDA/VPA) must run before enrichVPAAdvisory.
//   - enrichMetricFreshnessReport, enrichMetricContractAndAdapter and
//     enrichEvents must run before enrichMetricHints.
//   - enrichAdvisors must run before FinalizeAnalysis and the health snapshot.
//
// Do not reorder without re-reading buildStatusReport's dependency comments.
var statusEnrichers = []Enricher{
	decisionTracesEnricher{},
	eventsEnricher{},
	reportEnricher{},
	metricsDiagnosticsEnricher{},
	metricFreshnessEnricher{},
	resourceCheckEnricher{},
	targetReplicaObservationsEnricher{},
	podAnalysisEnricher{},
	simulationsEnricher{},
	capacityAnalysisEnricher{},
	rolloutAndBlockersEnricher{},
	controllerProfileEnricher{},
	capacityPlanEnricher{},
	gitOpsConflictEnricher{},
	metricContractAndAdapterEnricher{},
	churnAndFlappingEnricher{},
	vpaAdvisoryEnricher{},
	metricHintsEnricher{},
	advisorsEnricher{},
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
func runEnrichers(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	for _, e := range statusEnrichers {
		if !e.Enabled(p.Opts) {
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
// enrichXxx signatures are intentionally unchanged: the adapter just extracts
// the needed fields from PipelineContext and forwards them. Enrichers that
// unconditionally run (no feature flag) return true from Enabled.

type decisionTracesEnricher struct{}

func (decisionTracesEnricher) Name() string { return "decision-traces" }
func (decisionTracesEnricher) Enabled(opts *options) bool {
	return opts.decisionTrace || opts.decisionTraceFormat != ""
}
func (decisionTracesEnricher) Run(_ context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichDecisionTraces(p.Opts, hpa, report)
	return nil
}

type eventsEnricher struct{}

func (eventsEnricher) Name() string               { return "events" }
func (eventsEnricher) Enabled(opts *options) bool { return opts.events.enabled || opts.flappingAdvisor }
func (eventsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichEvents(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type reportEnricher struct{}

func (reportEnricher) Name() string          { return "report" }
func (reportEnricher) Enabled(*options) bool { return true }
func (reportEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichReport(ctx, p.EC, hpa, report, p.Opts.healthWeights)
	return nil
}

type metricsDiagnosticsEnricher struct{}

func (metricsDiagnosticsEnricher) Name() string               { return "metrics-diagnostics" }
func (metricsDiagnosticsEnricher) Enabled(opts *options) bool { return opts.diagnoseMetrics }
func (metricsDiagnosticsEnricher) Run(_ context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricsDiagnostics(p.Opts, hpa, report)
	return nil
}

type metricFreshnessEnricher struct{}

func (metricFreshnessEnricher) Name() string               { return "metric-freshness" }
func (metricFreshnessEnricher) Enabled(opts *options) bool { return opts.metricsFreshness }
func (metricFreshnessEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricFreshnessReport(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type resourceCheckEnricher struct{}

func (resourceCheckEnricher) Name() string               { return "resource-check" }
func (resourceCheckEnricher) Enabled(opts *options) bool { return opts.checkResources }
func (resourceCheckEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichResourceCheck(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type targetReplicaObservationsEnricher struct{}

func (targetReplicaObservationsEnricher) Name() string          { return "target-replica-observations" }
func (targetReplicaObservationsEnricher) Enabled(*options) bool { return true }
func (targetReplicaObservationsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichTargetReplicaObservations(ctx, p.Client, hpa, report)
	return nil
}

type podAnalysisEnricher struct{}

func (podAnalysisEnricher) Name() string               { return "pod-analysis" }
func (podAnalysisEnricher) Enabled(opts *options) bool { return opts.explainPods }
func (podAnalysisEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichPodAnalysis(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type simulationsEnricher struct{}

func (simulationsEnricher) Name() string { return "simulations" }
func (simulationsEnricher) Enabled(opts *options) bool {
	return len(opts.simulate) > 0 || len(opts.simulateMetric) > 0
}
func (simulationsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	return enrichSimulations(ctx, p.Opts, hpa, report)
}

type capacityAnalysisEnricher struct{}

func (capacityAnalysisEnricher) Name() string { return "capacity-analysis" }
func (capacityAnalysisEnricher) Enabled(opts *options) bool {
	return opts.capacityContext || opts.capacityHeadroom || opts.readinessImpact || opts.scalePath
}
func (capacityAnalysisEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichCapacityAnalysis(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type rolloutAndBlockersEnricher struct{}

func (rolloutAndBlockersEnricher) Name() string { return "rollout-and-blockers" }
func (rolloutAndBlockersEnricher) Enabled(opts *options) bool {
	return opts.rollout || opts.rolloutImpact || opts.capacityDeep || opts.scaleoutBlockers
}
func (rolloutAndBlockersEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichRolloutAndBlockers(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type controllerProfileEnricher struct{}

func (controllerProfileEnricher) Name() string { return "controller-profile" }
func (controllerProfileEnricher) Enabled(opts *options) bool {
	return opts.controllerProfile || opts.assumeProfile != "" || opts.controllerProfileFile != ""
}
func (controllerProfileEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichControllerProfile(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type capacityPlanEnricher struct{}

func (capacityPlanEnricher) Name() string               { return "capacity-plan" }
func (capacityPlanEnricher) Enabled(opts *options) bool { return opts.capacityPlan }
func (capacityPlanEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichCapacityPlan(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type gitOpsConflictEnricher struct{}

func (gitOpsConflictEnricher) Name() string { return "gitops-conflict" }
func (gitOpsConflictEnricher) Enabled(opts *options) bool {
	return opts.gitopsCheck || opts.manifestPath != ""
}
func (gitOpsConflictEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichGitOpsConflict(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type metricContractAndAdapterEnricher struct{}

func (metricContractAndAdapterEnricher) Name() string { return "metric-contract-and-adapter" }
func (metricContractAndAdapterEnricher) Enabled(opts *options) bool {
	return opts.metricContract || opts.adapterDiagnostics
}
func (metricContractAndAdapterEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricContractAndAdapter(ctx, p.Opts, p.Client, hpa, report)
	return nil
}

type churnAndFlappingEnricher struct{}

func (churnAndFlappingEnricher) Name() string { return "churn-and-flapping" }
func (churnAndFlappingEnricher) Enabled(opts *options) bool {
	return opts.churnDetect || opts.flappingAdvisor
}
func (churnAndFlappingEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichChurnAndFlapping(ctx, p.Opts, hpa, report)
	return nil
}

type vpaAdvisoryEnricher struct{}

func (vpaAdvisoryEnricher) Name() string          { return "vpa-advisory" }
func (vpaAdvisoryEnricher) Enabled(*options) bool { return true }
func (vpaAdvisoryEnricher) Run(_ context.Context, _ *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichVPAAdvisory(hpa, report)
	return nil
}

type metricHintsEnricher struct{}

func (metricHintsEnricher) Name() string               { return "metric-hints" }
func (metricHintsEnricher) Enabled(opts *options) bool { return opts.metricHints }
func (metricHintsEnricher) Run(_ context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichMetricHints(p.Opts, hpa, report)
	return nil
}

type advisorsEnricher struct{}

func (advisorsEnricher) Name() string { return "advisors" }
func (advisorsEnricher) Enabled(opts *options) bool {
	return opts.containerAdvisor || opts.behaviorAdvisor
}
func (advisorsEnricher) Run(ctx context.Context, p *PipelineContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	enrichAdvisors(ctx, p.Opts, p.Client, hpa, report)
	return nil
}
