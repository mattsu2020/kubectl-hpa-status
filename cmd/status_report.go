// Package cmd provides single- and multi-HPA report construction and text/export rendering for the
// status command. These functions build the StatusReport from a fetched HPA,
// run the enrichment pipeline, and render the gitops export / status text
// forms. Split from status.go so command wiring stays focused on
// orchestration.
package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/observation"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// writeReportsGitOpsExport writes a multi-HPA YAML export as a proper
// multi-document stream. Kustomize and Helm values exports describe a single
// target/file layout, so concatenating more than one would be ambiguous and is
// rejected before anything is written.
func writeReportsGitOpsExport(out io.Writer, exportFormat string, reports []hpaanalysis.StatusReport) error {
	format, err := normalizeGitOpsExportFormat(exportFormat)
	if err != nil {
		return err
	}
	if len(reports) > 1 && format != "yaml" {
		return fmt.Errorf("--export %s supports only a single HPA; use --export yaml for multi-HPA output or export each HPA separately", format)
	}

	rendered := make([][]byte, len(reports))
	for i, report := range reports {
		var buffer bytes.Buffer
		if err := writeGitOpsExport(&buffer, format, report); err != nil {
			return err
		}
		rendered[i] = buffer.Bytes()
	}
	for i, document := range rendered {
		if i > 0 {
			if _, err := fmt.Fprintln(out, "---"); err != nil {
				return err
			}
		}
		if _, err := out.Write(document); err != nil {
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
	return buildStatusReportFromHPA(ctx, opts, client, hpa, includeInterpretation, ec)
}

// buildStatusReportFromHPA reuses an HPA already read by the caller. Compound
// workflows such as snapshot and rollout use this entry point to keep one
// request-scoped view of cluster state and avoid duplicate API reads.
func buildStatusReportFromHPA(ctx context.Context, opts *options, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, includeInterpretation bool, ec *enrichmentContext) (hpaanalysis.StatusReport, error) {
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
	pipeline := &PipelineContext{
		Client:       client,
		EC:           ec,
		Observations: observation.New(client.Interface, hpa),
	}
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
	report.FreezeCanonical()

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
