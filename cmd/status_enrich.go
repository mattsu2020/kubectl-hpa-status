package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/history"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// This file holds the status enricher functions: each one fills an optional
// section of the StatusReport.Analysis based on the user's flags. The Enricher
// adapters in enricher.go dispatch to these free functions by name; moving them
// out of status.go keeps the command wiring (runStatus*, buildReportsConcurrently,
// fetch/build helpers) in status.go and the per-feature enrichment here.
//
// None of these functions take *options: the Enricher adapters extract the
// concrete values each enricher needs at construction time and forward them as
// plain parameters. This keeps the enrichment pipeline independent of the
// options God Object (see the Enricher interface decoupling in enricher.go).

// SimulationConfig bundles the values the simulation enricher needs. It is a
// small struct rather than five loose parameters because two of them are slices
// and the set is passed through to two helper functions.
type SimulationConfig struct {
	Overrides       []string                  // opts.simulate
	MetricOverrides []string                  // opts.simulateMetric
	DurationSeconds int32                     // opts.simulateDuration
	HealthWeights   hpaanalysis.HealthWeights // opts.healthWeights
	Debug           bool                      // opts.debug
}

// CapacityAnalysisConfig bundles the capacity-enricher flags so call sites stay
// readable and positional-bool mistakes are avoided.
type CapacityAnalysisConfig struct {
	CapacityContext  bool // opts.capacityContext
	CapacityHeadroom bool // opts.capacityHeadroom
	ReadinessImpact  bool // opts.readinessImpact
	ScalePath        bool // opts.scalePath
}

// RolloutAndBlockersConfig bundles the rollout/blockers-enricher flags.
type RolloutAndBlockersConfig struct {
	Rollout          bool // opts.rollout
	RolloutImpact    bool // opts.rolloutImpact
	CapacityDeep     bool // opts.capacityDeep
	ScaleoutBlockers bool // opts.scaleoutBlockers
}

// MetricContractConfig bundles the metric-contract / adapter-diagnostics flags.
type MetricContractConfig struct {
	MetricContract     bool // opts.metricContract
	AdapterDiagnostics bool // opts.adapterDiagnostics
}

// ChurnAndFlappingConfig bundles the churn/flapping-enricher flags plus the
// health weights used to apply churn penalties.
type ChurnAndFlappingConfig struct {
	ChurnDetect     bool                      // opts.churnDetect
	EventsEnabled   bool                      // opts.events.enabled
	FlappingAdvisor bool                      // opts.flappingAdvisor
	HealthWeights   hpaanalysis.HealthWeights // opts.healthWeights
}

// AdvisorsConfig bundles the container / behavior advisor flags.
type AdvisorsConfig struct {
	ContainerAdvisor bool // opts.containerAdvisor
	BehaviorAdvisor  bool // opts.behaviorAdvisor
}

func enrichDecisionTraces(hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, decisionTrace bool, decisionTraceFormat string) {
	if decisionTrace {
		report.Analysis.DecisionTrace = hpaanalysis.BuildDecisionTrace(hpa, report.Analysis.Min)
	}
	if decisionTraceFormat != "" {
		report.Analysis.StructuredDecisionTrace = hpaanalysis.ExportStructuredDecisionTrace(hpa, report.Analysis)
	}
}

func enrichEvents(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, eventLimit int) {
	coreEvents, err := kube.FetchRecentHPAEvents(ctx, client.Interface, hpa.Namespace, hpa.Name, int64(eventLimit))
	if err != nil {
		report.Events = []hpaanalysis.Event{{Reason: "Error", Message: fmt.Sprintf("failed to list events: %v", err)}}
		return
	}
	report.Events = hpaanalysis.EventsFromCore(coreEvents)
}

func enrichMetricsDiagnostics(hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	report.Analysis.MetricsDiagnostics = hpaanalysis.DiagnoseMetricsPipeline(hpa)
}

func enrichMetricFreshnessReport(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	report.Analysis.MetricFreshnessEntries = hpaanalysis.AnalyzeMetricFreshness(hpa, report.Events)
	enrichMetricFreshness(ctx, client, hpa, report)
}

func enrichResourceCheck(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	resources, err := kube.FetchScaleTargetResources(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name)
	if err != nil {
		// The user explicitly asked for resource checks (--check-resources);
		// surface the fetch failure so it is not silently dropped.
		report.Analysis.Warnings = append(report.Analysis.Warnings,
			fmt.Sprintf("resource check unavailable: failed to read scale target resources: %v", err))
		return
	}
	if resources != nil {
		report.Analysis.ResourceCheck = hpaanalysis.CheckResourceConsistency(hpa, convertResourceRequests(resources))
	}
}

func enrichTargetReplicaObservations(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	report.Analysis.TargetReplicas = fetchTargetReplicaInfo(ctx, client, hpa)
	if report.Analysis.TargetReplicas == nil {
		return
	}
	if report.Analysis.TargetReplicas.NotReady > 0 {
		appendNotReadyTargetObservations(report)
	}
	if report.Analysis.TargetReplicas.Pending > 0 {
		appendPendingTargetObservations(report)
	}
}

func appendNotReadyTargetObservations(report *hpaanalysis.StatusReport) {
	tr := report.Analysis.TargetReplicas
	report.Analysis.Interpretation = append(report.Analysis.Interpretation,
		fmt.Sprintf("[observed] %d of %d pods on the scale target are not ready — HPA excludes not-ready pods from utilization calculations, so scaling decisions may not reflect actual workload pressure.", tr.NotReady, tr.TotalReplicas),
	)
	report.Analysis.Actions = append(report.Analysis.Actions,
		fmt.Sprintf("Investigate why %d pod(s) are not ready on the scale target; not-ready pods can cause misleading metric utilization ratios.", tr.NotReady),
	)
}

func appendPendingTargetObservations(report *hpaanalysis.StatusReport) {
	tr := report.Analysis.TargetReplicas
	report.Analysis.Interpretation = append(report.Analysis.Interpretation,
		fmt.Sprintf("[observed] %d pod(s) for the scale target are Pending; HPA may be requesting capacity that the cluster has not scheduled yet.", tr.Pending),
	)
	if tr.Unschedulable > 0 {
		report.Analysis.Interpretation = append(report.Analysis.Interpretation,
			fmt.Sprintf("[observed] %d Pending pod(s) are marked Unschedulable, which points to node capacity, taint/toleration, affinity, or quota constraints rather than HPA math.", tr.Unschedulable),
		)
		report.Analysis.Actions = append(report.Analysis.Actions,
			"Check pending Pods, node capacity, Cluster Autoscaler/Karpenter events, quotas, affinity, and taints before raising HPA bounds.",
		)
	}
}

func enrichPodAnalysis(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	report.Analysis.PodAnalysis = fetchAndAnalyzePods(ctx, client, hpa)
}

func enrichSimulations(_ context.Context, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, cfg SimulationConfig) error {
	if len(cfg.Overrides) > 0 {
		applySimulationOverrides(hpa, report, cfg)
	}
	if len(cfg.MetricOverrides) > 0 {
		return applyMetricSimulation(hpa, report, cfg)
	}
	return nil
}

func applySimulationOverrides(hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, cfg SimulationConfig) {
	overrides, simErr := parseSimulateOverrides(cfg.Overrides)
	switch {
	case simErr != nil:
		report.Analysis.Interpretation = append(report.Analysis.Interpretation,
			fmt.Sprintf("simulation error: %v", simErr))
	case cfg.DurationSeconds > 0:
		sim, simErr := hpaanalysis.SimulateExtended(hpa, overrides,
			cfg.HealthWeights,
			hpaanalysis.SimulationExtendedOptions{
				DurationSeconds: cfg.DurationSeconds,
			})
		if simErr != nil {
			report.Analysis.Interpretation = append(report.Analysis.Interpretation,
				fmt.Sprintf("simulation error: %v", simErr))
		} else {
			report.Analysis.FlappingSimulation = sim
		}
	default:
		sim, simErr := hpaanalysis.SimulateHPA(hpa, overrides, cfg.HealthWeights)
		if simErr != nil {
			report.Analysis.Interpretation = append(report.Analysis.Interpretation,
				fmt.Sprintf("simulation error: %v", simErr))
		} else {
			report.Analysis.FlappingSimulation = sim
		}
	}
}

func applyMetricSimulation(hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, cfg SimulationConfig) error {
	metricOverrides, metricErr := parseSimulateMetricOverrides(cfg.MetricOverrides)
	if metricErr != nil {
		return fmt.Errorf("parsing --simulate-metric: %w", metricErr)
	}
	sim, simErr := hpaanalysis.SimulateMetricChange(hpa, metricOverrides, cfg.HealthWeights)
	if simErr != nil {
		return fmt.Errorf("metric simulation: %w", simErr)
	}
	if report.Analysis.FlappingSimulation == nil {
		report.Analysis.FlappingSimulation = sim
	} else {
		report.Analysis.FlappingSimulation.MetricSimulations = sim.MetricSimulations
	}
	return nil
}

func enrichCapacityAnalysis(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, cfg CapacityAnalysisConfig) {
	if cfg.CapacityContext {
		report.Analysis.CapacityContext = buildCapacityContext(ctx, client, hpa)
	}
	if cfg.CapacityHeadroom {
		report.Analysis.CapacityHeadroom = buildCapacityHeadroom(ctx, client, hpa, report.Analysis.Target)
	}
	if cfg.ReadinessImpact {
		report.Analysis.ReadinessImpact = buildReadinessImpact(ctx, client, hpa)
	}
	if cfg.ScalePath {
		report.Analysis.ScalePath = buildScalePath(ctx, client, hpa)
	}
}

func enrichRolloutAndBlockers(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, cfg RolloutAndBlockersConfig) {
	if cfg.Rollout || cfg.RolloutImpact {
		report.Analysis.RolloutDiagnosis = buildRolloutDiagnosis(ctx, client, hpa)
	}
	if cfg.CapacityDeep || cfg.ScaleoutBlockers {
		report.Analysis.BlockerReport = buildBlockerReportForStatus(ctx, client, hpa, report.Analysis.Target)
	}
}

func enrichCapacityPlan(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, targetMax int32) {
	if hpa.Status.CurrentReplicas >= hpa.Spec.MaxReplicas {
		report.Analysis.CapacityPlan = buildCapacityPlanForStatus(ctx, client, hpa, report.Analysis.Target, targetMax)
	}
}

func enrichGitOpsConflict(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, manifestPath string) {
	conflict := buildGitOpsConflict(ctx, client, hpa, manifestPath)
	if conflict != nil {
		report.Analysis.GitOpsConflict = conflict
	}
}

func enrichMetricContractAndAdapter(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, cfg MetricContractConfig) {
	if cfg.MetricContract {
		input := buildMetricContractInput(ctx, client, hpa)
		report.Analysis.MetricContract = hpaanalysis.AnalyzeMetricContract(input)
	}
	if cfg.AdapterDiagnostics {
		if len(report.Analysis.MetricFreshnessEntries) == 0 {
			report.Analysis.MetricFreshnessEntries = hpaanalysis.AnalyzeMetricFreshness(hpa, report.Events)
		}
		report.Analysis.AdapterDiagnostics = hpaanalysis.DiagnoseAdapter(
			hpa, report.Analysis.MetricFreshnessEntries, report.Analysis.MetricContract)
	}
}

func enrichChurnAndFlapping(_ context.Context, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, cfg ChurnAndFlappingConfig) {
	if cfg.ChurnDetect && cfg.EventsEnabled {
		report.Analysis.ChurnAnalysis = hpaanalysis.AnalyzeChurnFromEvents(report.Events, hpa)
		if report.Analysis.ChurnAnalysis != nil {
			hpaanalysis.ApplyChurnPenalty(&report.Analysis, cfg.HealthWeights)
		}
	}
	if cfg.FlappingAdvisor {
		report.Analysis.FlappingPrevention = hpaanalysis.AnalyzeFlappingPrevention(report.Events, hpa)
	}
}

func enrichVPAAdvisory(hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if report.Analysis.VPAConflict != nil {
		report.Analysis.VPAAdvisory = hpaanalysis.AnalyzeVPAAdvisory(hpa, report.Analysis.VPAConflict)
	}
}

func enrichMetricHints(hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	report.Analysis.MetricHints = hpaanalysis.AnalyzeMetricHints(
		hpa, report.Events, report.Analysis.MetricFreshnessEntries, report.Analysis.MetricContract)
	if report.Analysis.MetricHints != nil && len(report.Analysis.MetricHints.Hints) > 0 {
		report.Analysis.MetricHints.TroubleshootingFlows = hpaanalysis.BuildTroubleshootingFlows(report.Analysis.MetricHints.Hints)
	}
}

func enrichAdvisors(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, cfg AdvisorsConfig) {
	if cfg.ContainerAdvisor {
		report.Analysis.ContainerAdvisor = buildContainerAdvisor(ctx, client, hpa)
	}
	if cfg.BehaviorAdvisor {
		report.Analysis.BehaviorAdvisor = hpaanalysis.AnalyzeBehaviorAdvisor(hpa)
	}
}

func recordHealthSnapshotAndTrend(_ context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	// History recording is an explicit opt-in side effect: without --trend we
	// do not touch the local health store, so plain `status` runs (and CI) stay
	// free of unexpected local file writes.
	if !opts.Trend {
		return
	}
	store, storeErr := history.NewHealthStore()
	if storeErr != nil {
		report.Analysis.Warnings = append(report.Analysis.Warnings, fmt.Sprintf("health trend store unavailable: %v", storeErr))
		return
	}
	snapshot := hpaanalysis.HealthSnapshot{
		Timestamp:       time.Now(),
		HealthScore:     report.Analysis.HealthScore,
		HealthState:     report.Analysis.Health,
		DesiredReplicas: report.Analysis.Desired,
		CurrentReplicas: report.Analysis.Current,
		Stabilizing:     report.Analysis.StabilizationRemaining != nil && *report.Analysis.StabilizationRemaining > 0,
	}
	if err := store.Append(hpa.Namespace, hpa.Name, snapshot); err != nil {
		report.Analysis.Warnings = append(report.Analysis.Warnings, fmt.Sprintf("health trend append failed: %v", err))
	}
	if err := store.Prune(hpa.Namespace, hpa.Name, opts.TrendRetain); err != nil {
		report.Analysis.Warnings = append(report.Analysis.Warnings, fmt.Sprintf("health trend prune failed: %v", err))
	}

	snapshots, loadErr := store.Load(hpa.Namespace, hpa.Name, opts.TrendSince)
	if loadErr == nil && len(snapshots) > 0 {
		trend := hpaanalysis.AnalyzeHealthTrend(snapshots)
		report.Analysis.HealthTrend = &trend
	}
}
