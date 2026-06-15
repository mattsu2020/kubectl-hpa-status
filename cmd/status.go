package cmd

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/history"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newStatusCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "status NAME [NAME...]",
		Short:             "Show concise status for one or more HPAs",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			includeInterpretation := (opts.interpret || opts.explain || opts.suggest) && !opts.noInterpret
			if opts.watch {
				if len(args) != 1 {
					return fmt.Errorf("--watch supports exactly one HPA name")
				}
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], includeInterpretation)
			}
			return runStatusMany(cmd.Context(), cmd.OutOrStdout(), opts, args, includeInterpretation)
		},
	}

	// Status-specific flags are Local (cmd.Flags()) so they only appear under
	// the status subcommand and not on root --help. Cross-command flags such
	// as --apply/--diff/--export/--trend/--health-weight remain on root via
	// registerCommonFlags (PersistentFlags).
	registerStatusFlags(cmd, opts)
	if events := cmd.Flags().Lookup("events"); events != nil {
		events.NoOptDefVal = "true"
	}

	return cmd
}

func newAnalyzeCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "analyze NAME [NAME...]",
		Aliases:           []string{"diagnose"},
		Short:             "Analyze one or more HPAs using visible Kubernetes API signals",
		Deprecated:        "Use 'status NAME --explain' instead. Example: kubectl-hpa-status status my-hpa --explain. The analyze subcommand will be removed in a future release.",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.watch {
				if len(args) != 1 {
					return fmt.Errorf("--watch supports exactly one HPA name")
				}
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
			}
			return runStatusMany(cmd.Context(), cmd.OutOrStdout(), opts, args, !opts.noInterpret)
		},
	}
}

func runStatus(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	return runStatusMany(ctx, out, opts, []string{name}, includeInterpretation)
}

func runStatusMany(ctx context.Context, out io.Writer, opts *options, names []string, includeInterpretation bool) error {
	if opts.format == "structured" && opts.decisionTraceFormat == "" {
		opts.decisionTrace = true
		opts.decisionTraceFormat = "json"
		includeInterpretation = true
	}

	if opts.apply && len(names) > 1 {
		return fmt.Errorf("--apply supports only a single HPA at a time; use 'list --apply' for batch mode")
	}

	if len(names) == 1 {
		return runStatusSingle(ctx, out, opts, names[0], includeInterpretation)
	}
	return runStatusMultiple(ctx, out, opts, names, includeInterpretation)
}

// runStatusSingle handles the single-HPA status path, including structured/AI/apply/export output modes.
func runStatusSingle(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	watchMode := opts.watch
	ec := newEnrichmentContext(ctx, opts)
	report, err := buildStatusReportWithClient(ctx, opts, name, includeInterpretation, ec)
	if err != nil {
		if opts.output == "json" || opts.output == "yaml" {
			writeError(out, opts.output, err)
		}
		return err
	}
	if opts.format == "structured" {
		if report.Analysis.StructuredDecisionTrace == nil {
			report.Analysis.StructuredDecisionTrace = hpaanalysis.ExportStructuredDecisionTrace(nil, report.Analysis)
		}
		return writeOutput(out, "json", "", report.Analysis.StructuredDecisionTrace, nil)
	}
	if opts.contextForAI || opts.ask != "" {
		return writeAIContext(out, report, opts.ask)
	}
	if opts.apply {
		applied, err := applySuggestions(ctx, out, opts, name, report.Analysis.Suggestions)
		if err != nil {
			return err
		}
		report.Analysis.Actions = append(report.Analysis.Actions, applied...)
	}
	if opts.export != "" {
		return writeGitOpsExport(out, opts.export, report)
	}

	format, templateStr := selectStatusOutput(opts)
	if err := writeOutput(out, format, templateStr, report, func() error {
		return hpaanalysis.WriteStatusTextWithOptions(out, report, statusTextOptions(opts, out))
	}); err != nil {
		return err
	}
	return warningExitCode(report.Analysis.Health, report.Analysis.Name, report.Analysis.Namespace, watchMode)
}

// runStatusMultiple handles the multi-HPA status path with concurrent report building and multi-report output.
func runStatusMultiple(ctx context.Context, out io.Writer, opts *options, names []string, includeInterpretation bool) error {
	watchMode := opts.watch
	ec := newEnrichmentContext(ctx, opts)
	// Create client once for all HPAs to avoid redundant kubeconfig parsing.
	client, err := opts.newClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
	}

	reports, err := buildReportsConcurrently(ctx, out, opts, client, names, includeInterpretation, ec)
	if err != nil {
		return err
	}

	if opts.export != "" {
		return writeReportsGitOpsExport(out, opts.export, reports)
	}
	if opts.format == "structured" {
		traces := make([]*hpaanalysis.StructuredDecisionTrace, 0, len(reports))
		for i := range reports {
			traces = append(traces, reports[i].Analysis.StructuredDecisionTrace)
		}
		return writeOutput(out, "json", "", traces, nil)
	}
	if opts.contextForAI || opts.ask != "" {
		return writeAIContextMany(out, reports, opts.ask)
	}

	format, templateStr := selectStatusOutput(opts)
	if err := writeOutput(out, format, templateStr, reports, func() error {
		return writeReportsStatusText(out, opts, reports)
	}); err != nil {
		return err
	}

	return checkReportsWarningExitCodes(reports, watchMode)
}

// buildReportsConcurrently builds status reports for all named HPAs concurrently, applying suggestions when requested.
func buildReportsConcurrently(ctx context.Context, out io.Writer, opts *options, client *kube.Client, names []string, includeInterpretation bool, ec *enrichmentContext) ([]hpaanalysis.StatusReport, error) {
	reports := make([]hpaanalysis.StatusReport, len(names))
	g, gctx := errgroup.WithContext(ctx)
	limit := opts.concurrency
	if limit < 1 {
		limit = runtime.NumCPU()
	}
	g.SetLimit(limit)

	for i, name := range names {
		i, name := i, name
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			report, err := buildStatusReport(gctx, opts, client, name, includeInterpretation, ec)
			if err != nil {
				if opts.output == "json" || opts.output == "yaml" {
					writeError(out, opts.output, err)
				}
				return err
			}
			if opts.apply {
				applied, err := applySuggestions(gctx, out, opts, name, report.Analysis.Suggestions)
				if err != nil {
					return err
				}
				report.Analysis.Actions = append(report.Analysis.Actions, applied...)
			}
			reports[i] = report
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return reports, nil
}

// writeReportsGitOpsExport writes each report as a GitOps export, separated by blank lines.
func writeReportsGitOpsExport(out io.Writer, exportFormat string, reports []hpaanalysis.StatusReport) error {
	for i, report := range reports {
		if i > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if err := writeGitOpsExport(out, exportFormat, report); err != nil {
			return err
		}
	}
	return nil
}

// writeReportsStatusText writes each report's status text to out, separating reports with blank lines.
func writeReportsStatusText(out io.Writer, opts *options, reports []hpaanalysis.StatusReport) error {
	for i, report := range reports {
		if i > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if err := hpaanalysis.WriteStatusTextWithOptions(out, report, statusTextOptions(opts, out)); err != nil {
			return err
		}
	}
	return nil
}

// checkReportsWarningExitCodes returns the first warning exit code (if any) among the reports.
func checkReportsWarningExitCodes(reports []hpaanalysis.StatusReport, watchMode bool) error {
	for _, r := range reports {
		if err := warningExitCode(r.Analysis.Health, r.Analysis.Name, r.Analysis.Namespace, watchMode); err != nil {
			return err
		}
	}
	return nil
}

// selectStatusOutput resolves the output format and template string from the user's report/output/template selection.
func selectStatusOutput(opts *options) (string, string) {
	return outputSelection(outputConfig{
		report: opts.report, output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates,
	})
}

// statusTextOptions builds the StatusTextOptions used to render report text, including theme/lang/fix/diff settings.
func statusTextOptions(opts *options, out io.Writer) hpaanalysis.StatusTextOptions {
	return hpaanalysis.StatusTextOptions{
		Theme:         style.NewTheme(shouldColorize(opts.color, out)),
		Lang:          outputLang(opts.lang, opts.output),
		Fix:           opts.fix,
		Diff:          opts.diff,
		HiddenFactors: opts.hiddenFactors,
		Labels:        labelProviderForLang(opts.lang, opts.output),
	}
}

// buildStatusReportWithClient creates a client and delegates to buildStatusReport.
func buildStatusReportWithClient(ctx context.Context, opts *options, name string, includeInterpretation bool, ec *enrichmentContext) (hpaanalysis.StatusReport, error) {
	client, err := opts.newClient()
	if err != nil {
		return hpaanalysis.StatusReport{}, fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
	}
	return buildStatusReport(ctx, opts, client, name, includeInterpretation, ec)
}

func buildStatusReport(ctx context.Context, opts *options, client *kube.Client, name string, includeInterpretation bool, ec *enrichmentContext) (hpaanalysis.StatusReport, error) {
	hpa, err := fetchHPA(ctx, client, name)
	if err != nil {
		return hpaanalysis.StatusReport{}, err
	}

	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.AnalyzeWithOptions(hpa, includeInterpretation, analysisOptions(opts.healthWeights, opts.debug)),
	}

	// Run the enrichment pipeline. statusEnrichers preserves the exact order of
	// the previous sequential calls; enrichSimulations remains the only step
	// whose error aborts the whole report (see abortOnErrorEnrichers). Skipped
	// steps are silently ignored to avoid noise; failed steps record a message
	// in report.Analysis.Warnings.
	pipeline := &PipelineContext{Client: client, EC: ec, Opts: opts}
	if err := runEnrichers(ctx, pipeline, hpa, &report); err != nil {
		return hpaanalysis.StatusReport{}, err
	}

	// Finalize post-enrichment derivations (e.g. stabilization/churn
	// correlation) that depend on fields populated above. Must run before the
	// health snapshot is recorded so trend history reflects the final state.
	report.Analysis = hpaanalysis.FinalizeAnalysis(report.Analysis)
	recordHealthSnapshotAndTrend(ctx, opts, hpa, &report)

	return report, nil
}

// fetchHPA retrieves a single HPA and wraps known API errors with actionable guidance.
func fetchHPA(ctx context.Context, client *kube.Client, name string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	hpa, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(client.Namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, hpaFetchError(err, name, client.Namespace)
	}
	return hpa, nil
}

// hpaFetchError maps known Kubernetes API errors to user-facing guidance, preserving the wrapped cause.
func hpaFetchError(err error, name, namespace string) error {
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("HPA %q was not found in namespace %q. "+
			"If the cluster is running Kubernetes older than 1.23, the autoscaling/v2 API may not be available. "+
			"Check with: kubectl api-resources | grep autoscaling. Original error: %w", name, namespace, err)
	}
	if apierrors.IsMethodNotSupported(err) {
		return fmt.Errorf("the Kubernetes API server does not support the autoscaling/v2 API. "+
			"This plugin requires Kubernetes 1.23+ (stable from 1.26). "+
			"Check with: kubectl api-resources | grep autoscaling. Original error: %w", err)
	}
	return fmt.Errorf("failed to get HPA %s/%s from the Kubernetes API server: %w", namespace, name, err)
}

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
	events, err := hpaanalysis.RecentEvents(ctx, client.Interface, hpa.Namespace, hpa.Name, int64(opts.events.limit))
	if err != nil {
		report.Events = []hpaanalysis.Event{{Reason: "Error", Message: fmt.Sprintf("failed to list events: %v", err)}}
		return
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
	if err == nil && resources != nil {
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
	if opts.gitopsCheck || opts.manifestPath != "" {
		if conflict, err := buildGitOpsConflict(ctx, client, hpa, opts.manifestPath); err == nil && conflict != nil {
			report.Analysis.GitOpsConflict = conflict
		}
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
	_ = store.Append(hpa.Namespace, hpa.Name, snapshot)
	_ = store.Prune(hpa.Namespace, hpa.Name, opts.trendRetain)

	snapshots, loadErr := store.Load(hpa.Namespace, hpa.Name, opts.trendSince)
	if loadErr == nil && len(snapshots) > 0 {
		trend := hpaanalysis.AnalyzeHealthTrend(snapshots)
		report.Analysis.HealthTrend = &trend
	}
}

func buildScalePath(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.ScalePath {
	input := hpaanalysis.ScalePathInput{}
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err == nil && info != nil {
		input.Target = &hpaanalysis.ScalePathTarget{
			Kind:            info.Kind,
			Name:            info.Name,
			DesiredReplicas: info.DesiredReplicas,
			CurrentReplicas: info.Replicas,
			ReadyReplicas:   info.ReadyReplicas,
		}
		if pods, podErr := kube.FetchPodInfosForSelector(ctx, client.Interface, hpa.Namespace, info.SelectorStr); podErr == nil {
			input.Pods = convertScalePathPods(pods)
		}
		if replicaSets, rsErr := kube.FetchReplicaSetsForScaleTarget(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef, info.SelectorStr); rsErr == nil {
			input.ReplicaSets = convertScalePathReplicaSets(replicaSets)
		}
		objectNames := scalePathEventObjectNames(hpa, input.Pods, input.ReplicaSets)
		input.Events = convertScalePathEvents(kube.FetchRecentEventsForObjects(ctx, client.Interface, hpa.Namespace, objectNames, 10))
	}
	return hpaanalysis.AnalyzeScalePath(hpa, input)
}

func convertScalePathPods(pods []kube.PodInfo) []hpaanalysis.ScalePathPod {
	if len(pods) == 0 {
		return nil
	}
	result := make([]hpaanalysis.ScalePathPod, 0, len(pods))
	for _, pod := range pods {
		result = append(result, hpaanalysis.ScalePathPod{
			Name:          pod.Name,
			Phase:         pod.Phase,
			Ready:         pod.Ready,
			Unschedulable: pod.Unschedulable,
			Reasons:       pod.Reasons,
		})
	}
	return result
}

func convertScalePathReplicaSets(replicaSets []kube.ReplicaSetInfo) []hpaanalysis.ScalePathReplicaSet {
	if len(replicaSets) == 0 {
		return nil
	}
	result := make([]hpaanalysis.ScalePathReplicaSet, 0, len(replicaSets))
	for _, rs := range replicaSets {
		result = append(result, hpaanalysis.ScalePathReplicaSet{
			Name:            rs.Name,
			DesiredReplicas: rs.DesiredReplicas,
			CurrentReplicas: rs.CurrentReplicas,
			ReadyReplicas:   rs.ReadyReplicas,
		})
	}
	return result
}

func convertScalePathEvents(events []kube.EventInfo) []hpaanalysis.Event {
	if len(events) == 0 {
		return nil
	}
	result := make([]hpaanalysis.Event, 0, len(events))
	for _, event := range events {
		result = append(result, hpaanalysis.Event{
			Reason:    event.Reason,
			Message:   event.Message,
			Timestamp: event.Timestamp,
		})
	}
	return result
}

func scalePathEventObjectNames(hpa *autoscalingv2.HorizontalPodAutoscaler, pods []hpaanalysis.ScalePathPod, replicaSets []hpaanalysis.ScalePathReplicaSet) []string {
	names := []string{hpa.Name, hpa.Spec.ScaleTargetRef.Name}
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	for _, rs := range replicaSets {
		names = append(names, rs.Name)
	}
	return names
}

func fetchTargetReplicaInfo(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.TargetReplicaInfo {
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil {
		return nil
	}

	notReady := info.Replicas - info.ReadyReplicas
	result := &hpaanalysis.TargetReplicaInfo{
		TotalReplicas: info.Replicas,
		ReadyReplicas: info.ReadyReplicas,
		NotReady:      notReady,
	}
	enrichPendingPods(ctx, client, hpa.Namespace, info.SelectorStr, result)
	if result.NotReady <= 0 && result.Pending <= 0 && result.Unschedulable <= 0 {
		return nil
	}
	return result
}

func enrichPendingPods(ctx context.Context, client *kube.Client, namespace string, selector string, info *hpaanalysis.TargetReplicaInfo) {
	if selector == "" || info == nil {
		return
	}
	pods, err := client.Interface.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodPending {
			info.Pending++
			if podUnschedulable(pod) {
				info.Unschedulable++
			}
		}
	}
}

func podUnschedulable(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled &&
			condition.Status == corev1.ConditionFalse &&
			condition.Reason == corev1.PodReasonUnschedulable {
			return true
		}
	}
	return false
}

// buildContainerAdvisor builds the ContainerResource advisor result.
func buildContainerAdvisor(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.ContainerAdvisorResult {
	resources, err := kube.FetchScaleTargetResources(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name)
	if err != nil || resources == nil {
		return nil
	}

	containerCount := len(resources.Containers)
	var containerNames []string
	for _, c := range resources.Containers {
		containerNames = append(containerNames, c.Name)
	}

	usesResource := false
	usesContainerResource := false
	for _, spec := range hpa.Spec.Metrics {
		switch spec.Type {
		case autoscalingv2.ResourceMetricSourceType:
			usesResource = true
		case autoscalingv2.ContainerResourceMetricSourceType:
			usesContainerResource = true
		}
	}

	input := hpaanalysis.ContainerAdvisorInput{
		ContainerCount:              containerCount,
		ContainerNames:              containerNames,
		UsesResourceMetric:          usesResource,
		UsesContainerResourceMetric: usesContainerResource,
	}

	return hpaanalysis.AnalyzeContainerAdvisor(hpa, input)
}
