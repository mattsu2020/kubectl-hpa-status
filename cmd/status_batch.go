// Package cmd provides batch rendering helpers for the multi-HPA status path. These functions
// assemble the per-item status envelope, decide which output modes carry
// failures in their own schema, and derive the aggregate exit code. They are
// split from status.go so the command wiring stays focused on orchestration.
package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

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
	case "jsonl":
		return opts.OutputSchema == "v2" // v2 records carry partial errors; v1 keeps the historical success-report array.
	case "", "table", "wide", "ja":
		return true // text path renders an "Error:" row per failed item.
	default:
		// jsonpath / go-template / prometheus / markdown / html / incident:
		// only successful items are rendered; failures must go to stderr.
		return false
	}
}

// batchValue picks the value passed to render.Format for the multi-HPA path.
// json/yaml carry the StatusBatch envelope so failed items are visible. v2
// JSONL emits canonical StatusRecordV2 values one per line; v1 JSONL keeps
// its historical one-line successful-report array. Other formats render only
// successful reports.
func batchValue(opts *options, results []reportResult, reports []hpaanalysis.StatusReport) any {
	switch opts.Output {
	case "json", "yaml":
		batch := buildStatusBatch(results)
		if opts.OutputSchema == "v2" {
			return hpaanalysis.ProjectStatusBatchV2(batch)
		}
		return batch
	case "jsonl":
		if opts.OutputSchema == "v2" {
			return hpaanalysis.ProjectStatusRecordsV2(buildStatusBatch(results))
		}
		return reports
	default:
		if opts.OutputSchema == "v2" {
			return hpaanalysis.ProjectStatusReportsV2(reports)
		}
		return reports
	}
}

// statusOutputValue picks the value passed to render.Format for the
// single-HPA path, projecting to the v2 schema when requested.
func statusOutputValue(opts *options, report hpaanalysis.StatusReport) any {
	if opts != nil && opts.OutputSchema == "v2" {
		if opts.Output == "jsonl" {
			return hpaanalysis.ProjectStatusRecordV2(report)
		}
		return hpaanalysis.ProjectStatusReportV2(report)
	}
	return report
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

// errorWriter returns the command's diagnostic stream. The fallback preserves
// the behavior of direct helper callers that do not use cobra (primarily unit
// tests), while normal CLI execution always supplies ErrOrStderr.
func errorWriter(opts *options, fallback io.Writer) io.Writer {
	if opts != nil && opts.Err != nil {
		return opts.Err
	}
	return fallback
}

// emitPerItemErrors writes one render.Error-shaped line per failed item to
// the diagnostic stream.
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

// joinOutputAndExit returns the output error if non-nil, otherwise the
// health-derived exit-code error. A write failure is treated as more severe
// than a warning result.
func joinOutputAndExit(outputErr, exitErr error) error {
	if outputErr != nil {
		return outputErr
	}
	return exitErr
}

// joinExportAndExit preserves the historical helper name used by batch export.
func joinExportAndExit(exportErr, exitErr error) error {
	return joinOutputAndExit(exportErr, exitErr)
}

// buildReportsConcurrently builds status reports for all named HPAs
// concurrently and adapts the shared mapPerHPA results into reportResult.
//
// status is the one multi-HPA command that renders partial output, so it uses
// mapPerHPA directly instead of collectPerHPA: a per-item failure does NOT
// abort the batch, it is carried in the corresponding reportResult and the
// remaining reports are still emitted. The parent context is still honored
// (Ctrl+C cancels in-flight work).
//
// apply is intentionally not handled here: runStatusMany rejects --apply with
// multiple names up front (cmd/status.go), so this path is never reached with
// opts.Apply set.
func buildReportsConcurrently(ctx context.Context, opts *options, client *kube.Client, names []string, includeInterpretation bool, ec *enrichmentContext) []reportResult {
	built := mapPerHPA(ctx, perHPAConcurrency(opts), names, func(ctx context.Context, name string) (hpaanalysis.StatusReport, error) {
		return buildStatusReport(ctx, opts, client, name, includeInterpretation, ec)
	})

	results := make([]reportResult, len(built))
	for i, item := range built {
		results[i] = reportResult{
			name:      item.name,
			namespace: client.Namespace,
			report:    item.value,
			hasReport: item.err == nil,
			err:       item.err,
		}
	}
	return results
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
