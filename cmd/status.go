package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsu2020/kubectl-hpa-status/internal/render"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func newStatusCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "status NAME [NAME...]",
		Short:             "Show concise status for one or more HPAs",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			request := snapshotStatusRequest(opts, args)
			return executeStatusRequest(cmd.Context(), cmd.OutOrStdout(), request)
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

func executeStatusRequest(ctx context.Context, out io.Writer, request statusRequest) error {
	names := request.Names()
	executionOptions := request.Options()
	if request.WatchEnabled() {
		if len(names) != 1 {
			return fmt.Errorf("--watch supports exactly one HPA name")
		}
		return runWatch(ctx, out, &executionOptions, names[0], request.IncludeInterpretation())
	}
	return runStatusMany(ctx, out, &executionOptions, names, request.IncludeInterpretation())
}

func runStatus(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	return runStatusMany(ctx, out, opts, []string{name}, includeInterpretation)
}

func runStatusMany(ctx context.Context, out io.Writer, opts *options, names []string, includeInterpretation bool) error {
	// Derive the structured-mode decision-trace defaults on a scratch copy so
	// the caller's opts is never mutated: callers may pass a value they still
	// own (request copy or a shared preset). The derived copy is used for the
	// rest of this call unless no derivation is needed.
	derived := opts
	if opts.Format == "structured" && opts.DecisionTraceFormat == "" {
		copyOpts := *opts
		copyOpts.DecisionTrace = true
		copyOpts.DecisionTraceFormat = "json"
		derived = &copyOpts
		includeInterpretation = true
	}

	if derived.Apply && len(names) > 1 {
		return fmt.Errorf("--apply supports only a single HPA at a time; use 'list --apply' for batch mode")
	}

	if len(names) == 1 {
		return runStatusSingle(ctx, out, derived, names[0], includeInterpretation)
	}
	return runStatusMultiple(ctx, out, derived, names, includeInterpretation)
}

// runStatusSingle handles the single-HPA status path, including structured/AI/apply/export output modes.
func runStatusSingle(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	watchMode := opts.Watch.Watch
	var ec *enrichmentContext
	if !opts.NoEnrich {
		ec = newEnrichmentContext(ctx, opts)
	}
	client, err := newClientOrDefault(opts)
	if err != nil {
		if outputErr := writeStatusError(out, opts, unresolvedStatusNamespace(opts), name, err); outputErr != nil {
			return outputErr
		}
		return err
	}
	report, err := buildStatusReport(ctx, opts, client, name, includeInterpretation, ec)
	if err != nil {
		if outputErr := writeStatusError(out, opts, client.Namespace, name, err); outputErr != nil {
			return outputErr
		}
		return err
	}
	if opts.Format == "structured" {
		if report.Analysis.StructuredDecisionTrace == nil {
			report.Analysis.StructuredDecisionTrace = hpaanalysis.ExportStructuredDecisionTrace(nil, report.Analysis)
		}
		return joinOutputAndExit(
			render.Format(out, "json", "", report.Analysis.StructuredDecisionTrace, nil),
			warningExitCode(report.Analysis.Health, report.Analysis.Name, report.Analysis.Namespace, watchMode),
		)
	}
	if opts.ContextForAI || opts.Ask != "" {
		return joinOutputAndExit(
			writeAIContext(out, report, opts.Ask),
			warningExitCode(report.Analysis.Health, report.Analysis.Name, report.Analysis.Namespace, watchMode),
		)
	}
	if opts.Apply {
		applied, err := applySuggestions(ctx, out, opts, name, report.Analysis.Suggestions)
		if err != nil {
			return err
		}
		report.Analysis.Actions = append(report.Analysis.Actions, applied...)
	}
	if opts.Export != "" {
		return joinOutputAndExit(
			writeGitOpsExport(out, opts.Export, report),
			warningExitCode(report.Analysis.Health, report.Analysis.Name, report.Analysis.Namespace, watchMode),
		)
	}

	if err := renderWithOutput(out, opts, statusOutputValue(opts, report), func(out io.Writer) error {
		return hpaanalysis.WriteStatusTextWithOptions(out, report, statusTextOptions(opts, out))
	}); err != nil {
		return err
	}
	return warningExitCode(report.Analysis.Health, report.Analysis.Name, report.Analysis.Namespace, watchMode)
}

func unresolvedStatusNamespace(opts *options) string {
	if opts != nil && strings.TrimSpace(opts.Namespace) != "" {
		return opts.Namespace
	}
	return "<unknown>"
}

func writeStatusError(out io.Writer, opts *options, namespace, name string, reportErr error) error {
	if opts == nil {
		return nil
	}
	if !statusUsesV2Schema(opts) {
		if opts.Output == "json" || opts.Output == "yaml" {
			return render.Error(out, opts.Output, reportErr)
		}
		return nil
	}
	record := hpaanalysis.StatusRecordV2{
		APIVersion: hpaanalysis.SchemaVersionV2,
		Namespace:  namespace,
		Name:       name,
		Status:     hpaanalysis.StatusRecordErrorV2,
		Error:      reportErr.Error(),
	}
	return renderWithOutput(out, opts, record, nil)
}

// runStatusMultiple handles the multi-HPA status path. Unlike the single-HPA
// path, a per-item failure (e.g. one HPA is missing) does NOT abort the whole
// run: successful items are rendered and the failed item is surfaced in the
// output envelope / text as an error entry. The exit code reflects the most
// severe per-item outcome (error > warning > ok).
func runStatusMultiple(ctx context.Context, out io.Writer, opts *options, names []string, includeInterpretation bool) error {
	watchMode := opts.Watch.Watch
	var ec *enrichmentContext
	if !opts.NoEnrich {
		ec = newEnrichmentContext(ctx, opts)
	}
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
		emitPerItemErrors(errorWriter(opts, out), results)
	}

	// The gitops export path joins its write error with the batch exit code
	// instead of short-circuiting, so a render failure never masks per-item
	// health outcomes.
	if opts.Export != "" {
		return joinOutputAndExit(writeReportsGitOpsExport(out, opts.Export, successReports(results)), aggregateBatchExitCode(results, watchMode))
	}
	if err := renderBatchResults(out, opts, results); err != nil {
		return err
	}
	return aggregateBatchExitCode(results, watchMode)
}

// renderBatchResults dispatches batch rendering to the output mode selected
// by opts: structured decision traces, AI context, or the standard
// format/template pipeline.
func renderBatchResults(out io.Writer, opts *options, results []reportResult) error {
	if opts.Format == "structured" {
		return renderStructuredDecisionTraces(out, results)
	}
	if opts.ContextForAI || opts.Ask != "" {
		return writeAIContextMany(out, results, opts.Ask)
	}
	reports := successReports(results)
	return renderWithOutput(out, opts, batchValue(opts, results, reports), func(out io.Writer) error {
		return writeReportsStatusText(out, opts, results)
	})
}

// renderStructuredDecisionTraces emits one structured decision trace per
// successful item as a JSON array, exporting traces on demand for reports
// that did not carry one.
func renderStructuredDecisionTraces(out io.Writer, results []reportResult) error {
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
	return render.Format(out, "json", "", traces, nil)
}
