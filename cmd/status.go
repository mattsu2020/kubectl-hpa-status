package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
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
			includeInterpretation := (opts.Interpret || opts.Explain || opts.Suggest) && !opts.NoInterpret
			if opts.Watch.Watch {
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
		Deprecated:        "Use 'status NAME --explain' instead. Example: kubectl-hpa-status status my-hpa --explain. The analyze subcommand is scheduled for removal in v2.0.",
		Hidden:            true,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Watch.Watch {
				if len(args) != 1 {
					return fmt.Errorf("--watch supports exactly one HPA name")
				}
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.NoInterpret)
			}
			return runStatusMany(cmd.Context(), cmd.OutOrStdout(), opts, args, !opts.NoInterpret)
		},
	}
}

func runStatus(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	return runStatusMany(ctx, out, opts, []string{name}, includeInterpretation)
}

func runStatusMany(ctx context.Context, out io.Writer, opts *options, names []string, includeInterpretation bool) error {
	if opts.Format == "structured" && opts.DecisionTraceFormat == "" {
		opts.DecisionTrace = true
		opts.DecisionTraceFormat = "json"
		includeInterpretation = true
	}

	if opts.Apply && len(names) > 1 {
		return fmt.Errorf("--apply supports only a single HPA at a time; use 'list --apply' for batch mode")
	}

	if len(names) == 1 {
		return runStatusSingle(ctx, out, opts, names[0], includeInterpretation)
	}
	return runStatusMultiple(ctx, out, opts, names, includeInterpretation)
}

// runStatusSingle handles the single-HPA status path, including structured/AI/apply/export output modes.
func runStatusSingle(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	watchMode := opts.Watch.Watch
	ec := newEnrichmentContext(ctx, opts)
	report, err := buildStatusReportWithClient(ctx, opts, name, includeInterpretation, ec)
	if err != nil {
		if opts.Output == "json" || opts.Output == "yaml" {
			writeError(out, opts.Output, err)
		}
		return err
	}
	if opts.Format == "structured" {
		if report.Analysis.StructuredDecisionTrace == nil {
			report.Analysis.StructuredDecisionTrace = hpaanalysis.ExportStructuredDecisionTrace(nil, report.Analysis)
		}
		return writeOutput(out, "json", "", report.Analysis.StructuredDecisionTrace, nil)
	}
	if opts.ContextForAI || opts.Ask != "" {
		return writeAIContext(out, report, opts.Ask)
	}
	if opts.Apply {
		applied, err := applySuggestions(ctx, out, opts, name, report.Analysis.Suggestions)
		if err != nil {
			return err
		}
		report.Analysis.Actions = append(report.Analysis.Actions, applied...)
	}
	if opts.Export != "" {
		return writeGitOpsExport(out, opts.Export, report)
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
	watchMode := opts.Watch.Watch
	ec := newEnrichmentContext(ctx, opts)
	// Create client once for all HPAs to avoid redundant kubeconfig parsing.
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}

	reports, err := buildReportsConcurrently(ctx, out, opts, client, names, includeInterpretation, ec)
	if err != nil {
		return err
	}

	if opts.Export != "" {
		return writeReportsGitOpsExport(out, opts.Export, reports)
	}
	if opts.Format == "structured" {
		traces := make([]*hpaanalysis.StructuredDecisionTrace, 0, len(reports))
		for i := range reports {
			traces = append(traces, reports[i].Analysis.StructuredDecisionTrace)
		}
		return writeOutput(out, "json", "", traces, nil)
	}
	if opts.ContextForAI || opts.Ask != "" {
		return writeAIContextMany(out, reports, opts.Ask)
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
	limit := opts.Concurrency
	if limit < 1 {
		limit = runtime.NumCPU()
	}
	g.SetLimit(limit)

	for i, name := range names {
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			report, err := buildStatusReport(gctx, opts, client, name, includeInterpretation, ec)
			if err != nil {
				if opts.Output == "json" || opts.Output == "yaml" {
					writeError(out, opts.Output, err)
				}
				return err
			}
			if opts.Apply {
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
		report: opts.Report, output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates,
	})
}

// statusTextOptions builds the StatusTextOptions used to render report text, including theme/lang/fix/diff settings.
func statusTextOptions(opts *options, out io.Writer) hpaanalysis.StatusTextOptions {
	return hpaanalysis.StatusTextOptions{
		Theme:             style.NewTheme(shouldColorize(opts.Color, out)),
		Lang:              outputLang(opts.Lang, opts.Output),
		Fix:               opts.Fix,
		Diff:              opts.Diff,
		HiddenFactors:     opts.HiddenFactors,
		Labels:            labelProviderForLang(opts.Lang, opts.Output),
		SummaryTranslator: summaryTranslatorForLang(opts.Lang, opts.Output),
	}
}

// buildStatusReportWithClient creates a client and delegates to buildStatusReport.
func buildStatusReportWithClient(ctx context.Context, opts *options, name string, includeInterpretation bool, ec *enrichmentContext) (hpaanalysis.StatusReport, error) {
	client, err := newClientOrDefault(opts)
	if err != nil {
		return hpaanalysis.StatusReport{}, err
	}
	return buildStatusReport(ctx, opts, client, name, includeInterpretation, ec)
}

func buildStatusReport(ctx context.Context, opts *options, client *kube.Client, name string, includeInterpretation bool, ec *enrichmentContext) (hpaanalysis.StatusReport, error) {
	hpa, err := fetchHPA(ctx, client, name)
	if err != nil {
		return hpaanalysis.StatusReport{}, err
	}

	report := hpaanalysis.StatusReport{
		APIVersion: hpaanalysis.SchemaVersion,
		Analysis:   hpaanalysis.AnalyzeWithOptions(hpa, includeInterpretation, analysisOptions(opts.HealthWeights, opts.Debug)),
	}

	// Run the enrichment pipeline. buildStatusEnrichers preserves the exact
	// order of the previous sequential calls; enrichSimulations remains the
	// only step whose error aborts the whole report (see
	// abortOnErrorEnrichers). Skipped steps are silently ignored to avoid
	// noise; failed steps record a message in report.Analysis.Warnings.
	pipeline := &PipelineContext{Client: client, EC: ec}
	if err := runEnrichers(ctx, buildStatusEnrichers(opts), pipeline, hpa, &report); err != nil {
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
	hpa, err := kube.GetHPAFromClient(ctx, client, name)
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
			"Check with: kubectl api-resources | grep autoscaling. Original error: %w",
			name, namespace, errors.Join(ErrHPANotFound, err))
	}
	if apierrors.IsMethodNotSupported(err) {
		return fmt.Errorf("the Kubernetes API server does not support the autoscaling/v2 API. "+
			"This plugin requires Kubernetes 1.23+ (stable from 1.26). "+
			"Check with: kubectl api-resources | grep autoscaling. Original error: %w", err)
	}
	return fmt.Errorf("failed to get HPA %s/%s from the Kubernetes API server: %w", namespace, name, err)
}

// enrich* helpers have moved to status_enrich.go; see the package-level
// comment there for the rationale (keeping command wiring separate from
// per-feature enrichment functions).

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
