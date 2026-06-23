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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		writeErrorIfStructured(out, opts.Output, err)
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

// runStatusMultiple handles the multi-HPA status path. Unlike the single-HPA
// path, a per-item failure (e.g. one HPA is missing) does NOT abort the whole
// run: successful items are rendered and the failed item is surfaced in the
// output envelope / text as an error entry. The exit code reflects the most
// severe per-item outcome (error > warning > ok).
func runStatusMultiple(ctx context.Context, out io.Writer, opts *options, names []string, includeInterpretation bool) error {
	watchMode := opts.Watch.Watch
	ec := newEnrichmentContext(ctx, opts)
	// Create client once for all HPAs to avoid redundant kubeconfig parsing.
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}

	results := buildReportsConcurrently(ctx, opts, client, names, includeInterpretation, ec)

	// emitPerItemErrors writes failed items to stderr so stdout stays clean for
	// machine-readable consumers. Output modes that carry per-item errors in
	// their own schema (json/yaml/text/ai-context) skip this for those items.
	if !batchOutputCarriesErrors(opts) {
		emitPerItemErrors(out, results)
	}

	if opts.Export != "" {
		return joinExportAndExit(writeReportsGitOpsExport(out, opts.Export, successReports(results)), aggregateBatchExitCode(results, watchMode))
	}
	if opts.Format == "structured" {
		traces := make([]*hpaanalysis.StructuredDecisionTrace, 0, len(results))
		for i := range results {
			if !results[i].hasReport {
				continue
			}
			tr := results[i].report.Analysis.StructuredDecisionTrace
			if tr == nil {
				tr = hpaanalysis.ExportStructuredDecisionTrace(nil, results[i].report.Analysis)
			}
			traces = append(traces, tr)
		}
		if err := writeOutput(out, "json", "", traces, nil); err != nil {
			return err
		}
		return aggregateBatchExitCode(results, watchMode)
	}
	if opts.ContextForAI || opts.Ask != "" {
		if err := writeAIContextMany(out, results, opts.Ask); err != nil {
			return err
		}
		return aggregateBatchExitCode(results, watchMode)
	}

	format, templateStr := selectStatusOutput(opts)
	reports := successReports(results)
	if err := writeOutput(out, format, templateStr, batchValue(opts, results, reports), func() error {
		return writeReportsStatusText(out, opts, results)
	}); err != nil {
		return err
	}

	return aggregateBatchExitCode(results, watchMode)
}

// batchOutputCarriesErrors reports whether the active output mode embeds
// per-item errors in its own schema (so failed items do not need to be
// re-emitted on stderr). Markdown/HTML/incident/export/structured only render
// successful items, so for those modes stderr is the only place a failure
// surfaces.
func batchOutputCarriesErrors(opts *options) bool {
	if opts.Export != "" || opts.Format == "structured" {
		return false
	}
	if opts.ContextForAI || opts.Ask != "" {
		return true // AI context renders an "Error:" block per failed item.
	}
	switch opts.Output {
	case "json", "yaml":
		return true // StatusBatch envelope carries per-item errors.
	case "", "table", "wide", "ja":
		return true // text path renders an "Error:" row per failed item.
	default:
		// jsonpath / go-template / prometheus / markdown / html / incident:
		// only successful items are rendered; failures must go to stderr.
		return false
	}
}

// batchValue picks the value passed to render.Format for the multi-HPA path.
// json/yaml carry the StatusBatch envelope so failed items are visible; all
// other formats render only the successful []StatusReport slice (their
// renderers have no per-item error slot).
func batchValue(opts *options, results []reportResult, reports []hpaanalysis.StatusReport) any {
	switch opts.Output {
	case "json", "yaml":
		return buildStatusBatch(results)
	default:
		return reports
	}
}

// buildStatusBatch assembles the StatusBatch envelope from per-item results,
// preserving input order.
func buildStatusBatch(results []reportResult) hpaanalysis.StatusBatch {
	items := make([]hpaanalysis.StatusBatchItem, 0, len(results))
	for i := range results {
		r := results[i]
		item := hpaanalysis.StatusBatchItem{
			Namespace: r.namespace,
			Name:      r.name,
			Status:    r.batchStatus(),
		}
		if r.hasReport {
			rep := r.report
			item.Report = &rep
		} else {
			item.Error = r.err.Error()
		}
		items = append(items, item)
	}
	return hpaanalysis.StatusBatch{APIVersion: hpaanalysis.SchemaVersion, Items: items}
}

// successReports returns the subset of reports that built successfully, in
// input order. Used by renderers that have no per-item error slot (export,
// markdown, html, incident).
func successReports(results []reportResult) []hpaanalysis.StatusReport {
	reports := make([]hpaanalysis.StatusReport, 0, len(results))
	for i := range results {
		if results[i].hasReport {
			reports = append(reports, results[i].report)
		}
	}
	return reports
}

// emitPerItemErrors writes one render.Error-shaped line per failed item to
// stderr (or out when stderr is unavailable, matching existing conventions).
// It is used only by output modes that cannot carry per-item errors in their
// own schema.
func emitPerItemErrors(out io.Writer, results []reportResult) {
	for i := range results {
		if results[i].hasReport {
			continue
		}
		_, _ = fmt.Fprintf(out, "HPA %q in namespace %q: %v\n", results[i].name, results[i].namespace, results[i].err)
	}
}

// joinExportAndExit returns the export error if non-nil, otherwise the
// aggregated exit-code error. An export write failure is treated as more
// severe than a per-item warning.
func joinExportAndExit(exportErr, exitErr error) error {
	if exportErr != nil {
		return exportErr
	}
	return exitErr
}

// buildReportsConcurrently builds status reports for all named HPAs
// concurrently. Unlike the historical errgroup variant, a per-item failure
// does NOT abort the whole batch: the error is captured in the corresponding
// reportResult and the run continues so partial results can be emitted. The
// parent context is still honored (Ctrl+C cancels in-flight work).
//
// apply is intentionally not handled here: runStatusMany rejects --apply with
// multiple names up front (cmd/status.go), so this path is never reached with
// opts.Apply set.
func buildReportsConcurrently(ctx context.Context, opts *options, client *kube.Client, names []string, includeInterpretation bool, ec *enrichmentContext) []reportResult {
	results := make([]reportResult, len(names))
	for i, name := range names {
		results[i] = reportResult{name: name, namespace: opts.Namespace, err: errPending}
	}

	g, gctx := errgroup.WithContext(ctx)
	limit := opts.Concurrency
	if limit < 1 {
		limit = runtime.NumCPU()
	}
	g.SetLimit(limit)

	for i, name := range names {
		i, name := i, name
		g.Go(func() error {
			if gctx.Err() != nil {
				results[i].err = gctx.Err()
				return nil // do not cancel the group; record and move on
			}
			report, err := buildStatusReport(gctx, opts, client, name, includeInterpretation, ec)
			if err != nil {
				results[i].err = err
				return nil // partial-result: do not cancel the group
			}
			results[i].report = report
			results[i].hasReport = true
			results[i].err = nil
			return nil
		})
	}
	// g.Wait() is intentionally discarded: every goroutine above returns nil
	// (per-HPA errors are captured into results[i].err instead of cancelling
	// the group), so by construction Wait() returns nil here. If a future
	// change makes a goroutine return a non-nil error, this discard would hide
	// it — keep the goroutines returning nil, or revisit this call site.
	_ = g.Wait()
	return results
}

// reportResult is the per-HPA outcome of a multi-HPA run. It captures either a
// successfully built report or the error that prevented one, preserving the
// input order via the results slice index.
type reportResult struct {
	name      string
	namespace string
	report    hpaanalysis.StatusReport
	hasReport bool
	err       error
}

// errPending is a placeholder for results whose goroutine has not yet filled
// in a real value; it is overwritten before results are consumed.
var errPending = errors.New("report build did not complete")

// batchStatus maps a reportResult to its StatusBatchItem.Status.
func (r reportResult) batchStatus() hpaanalysis.StatusBatchStatus {
	if !r.hasReport {
		return hpaanalysis.BatchStatusError
	}
	switch hpaanalysis.HealthState(r.report.Analysis.Health) {
	case hpaanalysis.HealthError, hpaanalysis.HealthLimited:
		return hpaanalysis.BatchStatusWarning
	case "WARNING": // Analysis.Health is a string; some paths emit "WARNING".
		return hpaanalysis.BatchStatusWarning
	default:
		return hpaanalysis.BatchStatusOK
	}
}

// healthIsWarning reports whether a health string should raise the exit code
// to warning (ERROR / LIMITED / WARNING).
func healthIsWarning(health string) bool {
	switch hpaanalysis.HealthState(health) {
	case hpaanalysis.HealthError, hpaanalysis.HealthLimited:
		return true
	default:
		return health == "WARNING"
	}
}

// aggregateBatchExitCode returns the most severe per-item outcome as an
// ExitCodeError: any build error dominates (ExitError, 1), otherwise any
// warning-health item (ExitWarning, 2), otherwise nil. watchMode suppresses
// warning aggregation exactly like the single-HPA path.
func aggregateBatchExitCode(results []reportResult, watchMode bool) error {
	hasError := false
	hasWarning := false
	for i := range results {
		if !results[i].hasReport {
			hasError = true
			break
		}
		if healthIsWarning(results[i].report.Analysis.Health) {
			hasWarning = true
		}
	}
	if hasError {
		return &ExitCodeError{Code: ExitError, Err: fmt.Errorf("%d of %d HPA(s) could not be reported; see output for details", countFailed(results), len(results))}
	}
	if hasWarning && !watchMode {
		// Reuse the single-HPA helper to format a representative message.
		for i := range results {
			if err := warningExitCode(results[i].report.Analysis.Health, results[i].report.Analysis.Name, results[i].report.Analysis.Namespace, watchMode); err != nil {
				return err
			}
		}
	}
	return nil
}

// countFailed returns the number of results that did not produce a report.
func countFailed(results []reportResult) int {
	n := 0
	for i := range results {
		if !results[i].hasReport {
			n++
		}
	}
	return n
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

// writeReportsStatusText writes each report's status text to out, separating
// reports with blank lines. In the partial-result path, failed items are
// passed in as zero-value StatusReports with Analysis.Health="ERROR" and a
// message in Analysis.Summary; for clarity we render those inline so the text
// output reflects the same per-item outcome as the JSON envelope.
func writeReportsStatusText(out io.Writer, opts *options, results []reportResult) error {
	for i, r := range results {
		if i > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if !r.hasReport {
			if _, err := fmt.Fprintf(out, "HPA %s/%s\nError: %v\n", r.namespace, r.name, r.err); err != nil {
				return err
			}
			continue
		}
		if err := hpaanalysis.WriteStatusTextWithOptions(out, r.report, statusTextOptions(opts, out)); err != nil {
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
	//
	// --no-enrich / --hpa-only skips the pipeline entirely so status shows
	// only the HPA object. This is the RBAC-light path: no Pod, Deployment,
	// ReplicaSet, Event, KEDA, or VPA reads, making status usable in audited
	// or restricted-permission environments where those reads are denied.
	pipeline := &PipelineContext{Client: client, EC: ec}
	if !opts.NoEnrich {
		if err := runEnrichers(ctx, buildStatusEnrichers(opts), pipeline, hpa, &report); err != nil {
			return hpaanalysis.StatusReport{}, err
		}
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
	vers := kube.KubernetesVersions()
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("HPA %q was not found in namespace %q. "+
			"If the cluster is running Kubernetes older than %s, the autoscaling/v2 API may not be available. "+
			"Check with: kubectl api-resources | grep autoscaling. Original error: %w",
			name, namespace, vers.MinAPIVersion, errors.Join(ErrHPANotFound, err))
	}
	if apierrors.IsMethodNotSupported(err) {
		return fmt.Errorf("the Kubernetes API server does not support the autoscaling/v2 API. "+
			"This plugin officially supports Kubernetes %s+ (the API exists from %s+). "+
			"Check with: kubectl api-resources | grep autoscaling. Original error: %w",
			vers.StableSinceVersion, vers.MinAPIVersion, err)
	}
	return fmt.Errorf("failed to get HPA %s/%s from the Kubernetes API server: %w", namespace, name, errors.Join(ErrHPANotFound, err))
}
