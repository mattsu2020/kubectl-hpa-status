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

func enrichDecisionTraces(opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if opts.decisionTrace {
		report.Analysis.DecisionTrace = hpaanalysis.BuildDecisionTrace(hpa, report.Analysis.Min)
	}
	if opts.decisionTraceFormat != "" {
		report.Analysis.StructuredDecisionTrace = hpaanalysis.ExportStructuredDecisionTrace(hpa, report.Analysis)
	}
}

func enrichEvents(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if !opts.events.enabled && !opts.flappingAdvisor {
		return
	}
	coreEvents, err := kube.FetchRecentHPAEvents(ctx, client.Interface, hpa.Namespace, hpa.Name, int64(opts.events.limit))
	if err != nil {
		report.Events = []hpaanalysis.Event{{Reason: "Error", Message: fmt.Sprintf("failed to list events: %v", err)}}
		return
	}
	events := make([]hpaanalysis.Event, 0, len(coreEvents))
	for _, ce := range coreEvents {
		events = append(events, hpaanalysis.EventFromCore(ce))
	}
	report.Events = events
}

func enrichMetricsDiagnostics(opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if opts.diagnoseMetrics {
		report.Analysis.MetricsDiagnostics = hpaanalysis.DiagnoseMetricsPipeline(hpa)
	}
}

func enrichMetricFreshnessReport(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if !opts.metricsFreshness {
		return
	}
	report.Analysis.MetricFreshnessEntries = hpaanalysis.AnalyzeMetricFreshness(hpa, report.Events)
	enrichMetricFreshness(ctx, client, hpa, report)
}

func enrichResourceCheck(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if !opts.checkResources {
		return
	}
	resources, err := kube.FetchScaleTargetResources(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name)
	if err != nil {
		// The user explicitly asked for resource checks (--check-resources);
		// surface the fetch failure so it is not silently dropped.
		report.Analysis.Warnings = append(report.Analysis.Warnings,
			fmt.Sprintf("resource check unavailable: failed to read scale target resources: %v", err))
		return
	}
	if resources != nil {
		report.Analysis.ResourceCheck = hpaanalysis.CheckResourceConsistency(hpa, resources)
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

func enrichPodAnalysis(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if opts.explainPods {
		report.Analysis.PodAnalysis = fetchAndAnalyzePods(ctx, client, hpa)
	}
}

func enrichSimulations(_ context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	if len(opts.simulate) > 0 {
		applySimulationOverrides(opts, hpa, report)
	}
	if len(opts.simulateMetric) > 0 {
		return applyMetricSimulation(opts, hpa, report)
	}
	return nil
}

func applySimulationOverrides(opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	overrides, simErr := parseSimulateOverrides(opts.simulate)
	switch {
	case simErr != nil:
		report.Analysis.Interpretation = append(report.Analysis.Interpretation,
			fmt.Sprintf("simulation error: %v", simErr))
	case opts.simulateDuration > 0:
		sim, simErr := hpaanalysis.SimulateExtended(hpa, overrides,
			analysisOptions(opts.healthWeights, opts.debug).HealthWeights,
			hpaanalysis.SimulationExtendedOptions{
				DurationSeconds: opts.simulateDuration,
			})
		if simErr != nil {
			report.Analysis.Interpretation = append(report.Analysis.Interpretation,
				fmt.Sprintf("simulation error: %v", simErr))
		} else {
			report.Analysis.Simulation = sim
		}
	default:
		sim, simErr := hpaanalysis.SimulateHPA(hpa, overrides, analysisOptions(opts.healthWeights, opts.debug).HealthWeights)
		if simErr != nil {
			report.Analysis.Interpretation = append(report.Analysis.Interpretation,
				fmt.Sprintf("simulation error: %v", simErr))
		} else {
			report.Analysis.Simulation = sim
		}
	}
}

func applyMetricSimulation(opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	metricOverrides, metricErr := parseSimulateMetricOverrides(opts.simulateMetric)
	if metricErr != nil {
		return fmt.Errorf("parsing --simulate-metric: %w", metricErr)
	}
	sim, simErr := hpaanalysis.SimulateMetricChange(hpa, metricOverrides, opts.healthWeights)
	if simErr != nil {
		return fmt.Errorf("metric simulation: %w", simErr)
	}
	if report.Analysis.Simulation == nil {
		report.Analysis.Simulation = sim
	} else {
		report.Analysis.Simulation.MetricSimulations = sim.MetricSimulations
	}
	return nil
}

func enrichCapacityAnalysis(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if opts.capacityContext {
		report.Analysis.CapacityContext = buildCapacityContext(ctx, client, hpa)
	}
	if opts.capacityHeadroom {
		report.Analysis.CapacityHeadroom = buildCapacityHeadroom(ctx, client, hpa, report.Analysis.Target)
	}
	if opts.readinessImpact {
		report.Analysis.ReadinessImpact = buildReadinessImpact(ctx, client, hpa)
	}
	if opts.scalePath {
		report.Analysis.ScalePath = buildScalePath(ctx, client, hpa)
	}
}

func enrichRolloutAndBlockers(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if opts.rollout || opts.rolloutImpact {
		report.Analysis.RolloutDiagnosis = buildRolloutDiagnosis(ctx, client, hpa)
	}
	if opts.capacityDeep || opts.scaleoutBlockers {
		report.Analysis.BlockerReport = buildBlockerReportForStatus(ctx, client, hpa, report.Analysis.Target)
	}
}

func enrichControllerProfile(ctx context.Context, opts *options, client *kube.Client, _ *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if opts.controllerProfile || opts.assumeProfile != "" || opts.controllerProfileFile != "" {
		report.Analysis.ControllerProfile = buildControllerProfile(ctx, client, opts)
	}
}

func enrichCapacityPlan(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if opts.capacityPlan && hpa.Status.CurrentReplicas >= hpa.Spec.MaxReplicas {
		report.Analysis.CapacityPlan = buildCapacityPlanForStatus(ctx, client, hpa, report.Analysis.Target, opts.targetMax)
	}
}

func enrichGitOpsConflict(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if !opts.gitopsCheck && opts.manifestPath == "" {
		return
	}
	conflict, err := buildGitOpsConflict(ctx, client, hpa, opts.manifestPath)
	if err != nil {
		// GitOps check is opt-in; surface fetch/parse failures so the user
		// knows the conflict analysis did not run rather than silently no-op.
		report.Analysis.Warnings = append(report.Analysis.Warnings,
			fmt.Sprintf("gitops conflict check unavailable: %v", err))
		return
	}
	if conflict != nil {
		report.Analysis.GitOpsConflict = conflict
	}
}

func enrichMetricContractAndAdapter(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if opts.metricContract {
		input := buildMetricContractInput(ctx, client, hpa)
		report.Analysis.MetricContract = hpaanalysis.AnalyzeMetricContract(input)
	}
	if opts.adapterDiagnostics {
		if len(report.Analysis.MetricFreshnessEntries) == 0 {
			report.Analysis.MetricFreshnessEntries = hpaanalysis.AnalyzeMetricFreshness(hpa, report.Events)
		}
		report.Analysis.AdapterDiagnostics = hpaanalysis.DiagnoseAdapter(
			hpa, report.Analysis.MetricFreshnessEntries, report.Analysis.MetricContract)
	}
}

func enrichChurnAndFlapping(_ context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if opts.churnDetect && opts.events.enabled {
		report.Analysis.ChurnAnalysis = hpaanalysis.AnalyzeChurnFromEvents(report.Events, hpa)
		if report.Analysis.ChurnAnalysis != nil {
			hpaanalysis.ApplyChurnPenalty(&report.Analysis, opts.healthWeights)
		}
	}
	if opts.flappingAdvisor {
		report.Analysis.FlappingPrevention = hpaanalysis.AnalyzeFlappingPrevention(report.Events, hpa)
	}
}

func enrichVPAAdvisory(hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if report.Analysis.VPAConflict != nil {
		report.Analysis.VPAAdvisory = hpaanalysis.AnalyzeVPAAdvisory(hpa, report.Analysis.VPAConflict)
	}
}

func enrichMetricHints(opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if !opts.metricHints {
		return
	}
	report.Analysis.MetricHints = hpaanalysis.AnalyzeMetricHints(
		hpa, report.Events, report.Analysis.MetricFreshnessEntries, report.Analysis.MetricContract)
	if report.Analysis.MetricHints != nil && len(report.Analysis.MetricHints.Hints) > 0 {
		report.Analysis.MetricHints.TroubleshootingFlows = hpaanalysis.BuildTroubleshootingFlows(report.Analysis.MetricHints.Hints)
	}
}

func enrichAdvisors(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if opts.containerAdvisor {
		report.Analysis.ContainerAdvisor = buildContainerAdvisor(ctx, client, hpa)
	}
	if opts.behaviorAdvisor {
		report.Analysis.BehaviorAdvisor = hpaanalysis.AnalyzeBehaviorAdvisor(hpa)
	}
}

func recordHealthSnapshotAndTrend(_ context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	// History recording is an explicit opt-in side effect: without --trend we
	// do not touch the local health store, so plain `status` runs (and CI) stay
	// free of unexpected local file writes.
	if !opts.trend {
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
	if err := store.Prune(hpa.Namespace, hpa.Name, opts.trendRetain); err != nil {
		report.Analysis.Warnings = append(report.Analysis.Warnings, fmt.Sprintf("health trend prune failed: %v", err))
	}

	snapshots, loadErr := store.Load(hpa.Namespace, hpa.Name, opts.trendSince)
	if loadErr == nil && len(snapshots) > 0 {
		trend := hpaanalysis.AnalyzeHealthTrend(snapshots)
		report.Analysis.HealthTrend = &trend
	}
}
