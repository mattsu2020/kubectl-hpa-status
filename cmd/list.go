package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/enrichment"
	"github.com/mattsu2020/kubectl-hpa-status/internal/history"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newListCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List HPAs and highlight visible issues",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.Watch.Watch {
				return runWatchList(cmd.Context(), cmd.OutOrStdout(), opts)
			}
			return runList(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.SortBy, "sort-by", "", "sort list by namespace, name, current, desired, diff, health-score, or issue")
	cmd.Flags().StringVar(&opts.Filter, "filter", "", "filter list by all, ok, error, limited, scaling-limited, or issue")
	cmd.Flags().IntVar(&opts.HealthScoreMax, "health-score", -1, "show only HPAs with health score at or below this threshold")
	cmd.Flags().IntVar(&opts.HealthScoreMin, "min-score", -1, "show only HPAs with health score at or above this threshold")
	cmd.Flags().BoolVar(&opts.Problem, "problem", false, "show only HPAs with visible problems")
	cmd.Flags().BoolVar(&opts.GitOpsDrift, "gitops-drift", false, "detect Argo CD/Flux-managed HPAs that should be checked for live-vs-Git drift")
	return cmd
}

func newScanCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "scan",
		Aliases: []string{"problems"},
		Short:   "Scan all namespaces for HPAs with visible problems",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Shallow copy to avoid mutating shared state.
			// NOTE: reference fields (clientOverride, outputTemplates, etc.) are shared.
			// This is safe because runList does not mutate them.
			scanOpts := copyOptions(opts)
			scanOpts.AllNamespaces = true
			scanOpts.Problem = true
			scanOpts.Wide = true
			return runList(cmd.Context(), cmd.OutOrStdout(), &scanOpts)
		},
	}
	cmd.Flags().BoolVar(&opts.Summary, "summary", false, "include cluster summary and prioritized actions in markdown/html reports")
	cmd.Flags().BoolVar(&opts.Conflicts, "conflicts", false, "detect HPAs and related controllers that may conflict on the same scale target")
	return cmd
}

func runList(ctx context.Context, out io.Writer, opts *options) error {
	if opts.Conflicts {
		return runConflictScan(ctx, out, opts)
	}

	// Uses the raw error so reportListError can render a list-specific error
	// document for JSON/YAML output; the standard wrapper would only add an
	// English prefix that breaks the structured output contract.
	client, err := opts.NewClient()
	if err != nil {
		return reportListError(out, opts.Output, err)
	}

	namespace := client.Namespace
	if opts.AllNamespaces {
		namespace = metav1.NamespaceAll
	}
	filter := opts.Filter
	if opts.Problem && filter == "" {
		filter = "issue"
	}

	// Page-by-page streaming path: when the output does not require the full
	// accumulated set (no sort, no apply, no export-directory, no conflicts),
	// process each Kubernetes list page and emit it immediately instead of
	// buffering every HPA. This keeps memory flat on clusters with thousands
	// of HPAs. KEDA/VPA enrichment is batched per-namespace, so it cannot
	// stream safely — when either is enabled we fall back to the accumulated
	// path below. Health-trend recording (opts.Trend) also needs the full set
	// for stable cross-HPA history, so it opts out too.
	if canStreamList(opts) {
		return runListStreaming(ctx, out, opts, client, namespace, filter)
	}

	hpas, err := client.ListHPAs(ctx, namespace, metav1.ListOptions{LabelSelector: opts.Selector}, opts.ChunkSize)
	if err != nil {
		return reportListError(out, opts.Output, fmt.Errorf("failed to list HPAs: %w", err))
	}

	report := hpaanalysis.ListReport{APIVersion: hpaanalysis.SchemaVersion, Items: buildListItems(ctx, opts, hpas.Items, filter)}
	if opts.GitOpsDrift {
		report.GitOpsDrift = buildGitOpsDriftSignals(hpas.Items)
	}

	sortBy := opts.SortBy
	if opts.Problem && sortBy == "" {
		sortBy = "problem"
	}
	sortListItems(report.Items, sortBy)

	return finishAccumulatedList(ctx, out, opts, filter, hpas.Items, report)
}

// finishAccumulatedList runs the apply / export / write phase for the buffered
// (non-streaming) list path after the report has been built and sorted.
func finishAccumulatedList(ctx context.Context, out io.Writer, opts *options, filter string, hpas []autoscalingv2.HorizontalPodAutoscaler, report hpaanalysis.ListReport) error {
	if opts.Apply {
		if err := validateListApply(opts, filter); err != nil {
			return err
		}
		if err := applyListSuggestions(ctx, out, opts, hpas, report.Items); err != nil {
			return err
		}
	}
	if opts.Export == "directory" {
		return exportListPatchesDirectory(out, opts, hpas, report.Items)
	}
	return writeListResult(out, opts, report)
}

// canStreamList reports whether runList may use the page-by-page streaming
// path. It requires all of:
//   - no --sort-by (sorting needs the complete set);
//   - no --apply / --export directory (they iterate over every HPA and need a
//     final count);
//   - no --gitops-drift (built from the accumulated slice);
//   - no KEDA/VPA enrichment (BatchKEDA/BatchVPA list per-namespace and would
//     re-issue those lists once per page that touches the namespace);
//   - no --trend (health-trend recording is a cross-HPA side effect);
//   - a streaming-friendly output format (jsonl, or the default table form
//     which has no enclosing array/separator).
//
// Any feature outside this set keeps the historical accumulated path, so the
// streaming path is purely additive and never changes existing output.
func canStreamList(opts *options) bool {
	if opts.SortBy != "" || opts.Apply || opts.Export == "directory" || opts.GitOpsDrift || opts.Trend {
		return false
	}
	if enrichment.Requested(opts.KEDA) || enrichment.Requested(opts.VPA) {
		return false
	}
	// --report pins a format (markdown/html/junit/sarif) that needs the whole
	// report; the streaming path only handles raw table and jsonl.
	if opts.Report != "" {
		return false
	}
	switch normalizeOutputFormat(opts.Output) {
	case "", "table", "wide", "jsonl":
		return true
	default:
		return false
	}
}

// runListStreaming lists HPAs page by page and emits each page's items as soon
// as they are analyzed, without buffering the whole cluster. The table form
// streams a continuous table; jsonl streams one JSON object per line. Only the
// canStreamList() subset of options reaches this path, so there is no sort,
// apply, export, GitOps-drift, or KEDA/VPA handling here.
func runListStreaming(ctx context.Context, out io.Writer, opts *options, client *kube.Client, namespace, filter string) error {
	streamer := newListStreamer(out, opts)
	if err := streamer.begin(); err != nil {
		return err
	}
	listOpts := metav1.ListOptions{LabelSelector: opts.Selector}
	err := kube.ListHPAsEachPage(ctx, client.Interface, namespace, listOpts, opts.ChunkSize, func(page *autoscalingv2.HorizontalPodAutoscalerList) error {
		items := buildListItems(ctx, opts, page.Items, filter)
		return streamer.writePage(items)
	})
	if werr := streamer.end(); werr != nil {
		return werr
	}
	if err != nil {
		return reportListError(out, opts.Output, fmt.Errorf("failed to list HPAs: %w", err))
	}
	return nil
}

// reportListError writes the error in the requested output format when applicable.
func reportListError(out io.Writer, output string, listErr error) error {
	writeErrorIfStructured(out, output, listErr)
	return listErr
}

// validateListApply ensures --apply is used with a bounded filter.
func validateListApply(opts *options, filter string) error {
	if normalizeSelector(filter) == "all" {
		return fmt.Errorf("--apply with list rejects --filter=all; select a bounded set with --problem, a specific --filter, --health-score, or --min-score")
	}
	if !opts.Problem && filter == "" && opts.HealthScoreMin <= 0 && effectiveHealthScoreMax(opts) < 0 {
		return fmt.Errorf("--apply with list requires --problem, a specific --filter, --health-score, or --min-score to avoid applying suggestions to an unbounded set")
	}
	return nil
}

// buildListItems analyzes each HPA and returns filtered list items.
func buildListItems(ctx context.Context, opts *options, hpas []autoscalingv2.HorizontalPodAutoscaler, filter string) []hpaanalysis.ListItem {
	ec := newEnrichmentContext(ctx, opts)
	kedaResults, kedaWarnings := enrichListKEDA(ctx, ec, hpas)
	vpaResults, vpaWarnings := enrichListVPA(ctx, ec, hpas)
	var store *history.HealthStore
	if opts.Trend {
		s, err := history.NewHealthStore()
		if err == nil {
			store = s
		} else {
			// Surface the init failure so --trend silently producing no trend data
			// (e.g. an unwritable cache dir) is not mistaken for "no history yet".
			_, _ = fmt.Fprintf(errorWriter(opts, os.Stderr), "warning: health trend store unavailable, --trend will show no data: %v\n", err)
		}
	}

	var items []hpaanalysis.ListItem
	for i := range hpas {
		analysis := hpaanalysis.AnalyzeWithOptions(&hpas[i], opts.Apply, analysisOptions(opts.HealthWeights, opts.Debug))

		// Surface per-namespace KEDA/VPA list failures on the affected HPAs so a
		// permissions error is distinguishable from "no objects found". The same
		// warning appears on every HPA in the failing namespace, which is the
		// intended signal: operators see it on the rows they are inspecting.
		analysis.Warnings = append(analysis.Warnings, kedaWarnings[analysis.Namespace]...)
		analysis.Warnings = append(analysis.Warnings, vpaWarnings[analysis.Namespace]...)

		key := analysis.Namespace + "/" + analysis.Name
		if kedaResults != nil {
			if keda, ok := kedaResults[key]; ok {
				analysis.KEDAInfo = keda
			}
		}
		if vpaResults != nil {
			if vpa, ok := vpaResults[key]; ok {
				analysis.VPAConflict = vpa
			}
		}
		if analysis.KEDAInfo != nil || analysis.VPAConflict != nil {
			hpaanalysis.ApplyEnrichmentPenalties(&analysis, opts.HealthWeights)
		}
		if store != nil {
			attachHealthTrend(store, &analysis, opts.TrendSince, opts.TrendRetain)
		}
		analysis = hpaanalysis.FinalizeAnalysis(analysis)

		item := hpaanalysis.NewListItem(analysis)
		if matchesListFilter(item, filter) && matchesHealthScoreRange(item, opts.HealthScoreMin, effectiveHealthScoreMax(opts)) {
			items = append(items, item)
		}
	}
	return items
}

func attachHealthTrend(store *history.HealthStore, analysis *hpaanalysis.Analysis, since, retention time.Duration) {
	if store == nil || analysis == nil {
		return
	}
	snapshot := hpaanalysis.HealthSnapshot{
		Timestamp:       time.Now(),
		HealthScore:     analysis.HealthScore,
		HealthState:     analysis.Health,
		DesiredReplicas: analysis.Desired,
		CurrentReplicas: analysis.Current,
		Stabilizing:     analysis.StabilizationRemaining != nil && *analysis.StabilizationRemaining > 0,
	}
	if err := store.Append(analysis.Namespace, analysis.Name, snapshot); err != nil {
		analysis.Warnings = append(analysis.Warnings, fmt.Sprintf("health trend append failed: %v", err))
	}
	if err := store.Prune(analysis.Namespace, analysis.Name, retention); err != nil {
		analysis.Warnings = append(analysis.Warnings, fmt.Sprintf("health trend prune failed: %v", err))
	}
	snapshots, err := store.Load(analysis.Namespace, analysis.Name, since)
	if err != nil || len(snapshots) == 0 {
		return
	}
	trend := hpaanalysis.AnalyzeHealthTrend(snapshots)
	analysis.HealthTrend = &trend
}

// writeListResult renders the list report in the selected output format.
func writeListResult(out io.Writer, opts *options, report hpaanalysis.ListReport) error {
	wide := opts.Wide || opts.Output == "wide"
	format, templateStr := selectOutputFromOptions(opts)
	if opts.Summary && (format == "markdown" || format == "md") {
		return writeClusterSummaryMarkdown(out, report)
	}
	if opts.Summary && format == "html" {
		return writeClusterSummaryHTML(out, report)
	}
	if format == "junit" {
		return writeListJUnit(out, report)
	}
	if format == "sarif" {
		return writeListSARIF(out, report)
	}
	return writeOutput(out, format, templateStr, report, func() error {
		if err := hpaanalysis.WriteListText(out, report, hpaanalysis.ListTextOptions{
			Wide:              wide,
			Color:             shouldColorize(opts.Color, out),
			Theme:             style.NewTheme(shouldColorize(opts.Color, out)),
			Lang:              outputLang(opts.Lang, opts.Output),
			Labels:            labelProviderForLang(opts.Lang, opts.Output),
			SummaryTranslator: summaryTranslatorForLang(opts.Lang, opts.Output),
		}); err != nil {
			return err
		}
		if len(report.GitOpsDrift) > 0 {
			if _, err := fmt.Fprintln(out, "\nGitOps drift candidates:"); err != nil {
				return err
			}
			for _, item := range report.GitOpsDrift {
				if _, err := fmt.Fprintf(out, "- %s/%s [%s]: %s\n", item.Namespace, item.Name, item.Tool, item.Advice); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func buildGitOpsDriftSignals(hpas []autoscalingv2.HorizontalPodAutoscaler) []hpaanalysis.GitOpsDriftSignal {
	var signals []hpaanalysis.GitOpsDriftSignal
	for i := range hpas {
		hpa := hpas[i]
		tool, evidence := gitOpsToolEvidence(hpa.Annotations, hpa.Labels)
		if tool == "" {
			continue
		}
		signals = append(signals, hpaanalysis.GitOpsDriftSignal{
			Namespace: hpa.Namespace,
			Name:      hpa.Name,
			Tool:      tool,
			Evidence:  evidence,
			Advice:    "compare live spec with the declared Git manifest; use status --gitops-check --manifest for field-level conflict checks",
		})
	}
	return signals
}

func gitOpsToolEvidence(annotations, labels map[string]string) (string, []string) {
	var evidence []string
	if v := annotations["argocd.argoproj.io/tracking-id"]; v != "" {
		return "argocd", []string{"argocd.argoproj.io/tracking-id=" + v}
	}
	if v := labels["app.kubernetes.io/managed-by"]; strings.EqualFold(v, "argocd") {
		return "argocd", []string{"app.kubernetes.io/managed-by=" + v}
	}
	if v := annotations["kustomize.toolkit.fluxcd.io/name"]; v != "" {
		evidence = append(evidence, "kustomize.toolkit.fluxcd.io/name="+v)
	}
	if v := annotations["helm.toolkit.fluxcd.io/name"]; v != "" {
		evidence = append(evidence, "helm.toolkit.fluxcd.io/name="+v)
	}
	if len(evidence) > 0 {
		return "flux", evidence
	}
	return "", nil
}

// batch apply/export helpers (applyListSuggestions, exportListPatchesDirectory,
// collectBatchEntries, printBatchSummary, confirmBatchApply,
// executeBatchPatches) live in list_apply.go.

func matchesListFilter(item hpaanalysis.ListItem, filter string) bool {
	switch normalizeSelector(filter) {
	case "", "all":
		return true
	case "ok":
		return item.Health == string(hpaanalysis.HealthOK)
	case "error":
		return item.Health == string(hpaanalysis.HealthError)
	case "limited", "scalinglimited":
		return item.Health == string(hpaanalysis.HealthLimited)
	case "issue":
		return item.Issue != ""
	default:
		return strings.EqualFold(item.Health, filter) || strings.Contains(normalizeSelector(item.Issue), normalizeSelector(filter))
	}
}

func matchesHealthScoreRange(item hpaanalysis.ListItem, minScore int, maxScore int) bool {
	if minScore > 100 {
		minScore = 100
	}
	if maxScore > 100 {
		maxScore = 100
	}
	if minScore >= 0 && item.HealthScore < minScore {
		return false
	}
	if maxScore >= 0 && item.HealthScore > maxScore {
		return false
	}
	return true
}

func effectiveHealthScoreMax(opts *options) int {
	if opts == nil {
		return -1
	}
	if opts.HealthScoreMax == 0 && !opts.HealthScoreMaxConfigured {
		return -1
	}
	return opts.HealthScoreMax
}

func sortListItems(items []hpaanalysis.ListItem, sortBy string) {
	sort.SliceStable(items, func(i, j int) bool {
		return listItemLess(items[i], items[j], sortBy)
	})
}

// listItemLess compares two list items according to the selected sort key. The switch is a flat
// key-dispatch table; each case is an independent, single-line comparison.
func listItemLess(left, right hpaanalysis.ListItem, sortBy string) bool {
	switch normalizeSelector(sortBy) {
	case "namespace":
		return left.Namespace < right.Namespace
	case "name", "":
		return left.Name < right.Name
	case "current", "currentreplicas":
		return left.Current < right.Current
	case "desired", "desiredreplicas":
		return left.Desired < right.Desired
	case "diff", "replicadiff", "difference":
		return absReplicaDiff(left) > absReplicaDiff(right)
	case "age", "creationtimestamp":
		return left.CreationTimestamp.Before(&right.CreationTimestamp)
	case "health":
		return left.Health < right.Health
	case "healthscore", "score":
		return left.HealthScore > right.HealthScore
	case "problem":
		return problemLess(left, right)
	case "issue":
		return left.Issue < right.Issue
	case "min", "minreplicas":
		return left.Min < right.Min
	case "max", "maxreplicas":
		return left.Max < right.Max
	case "target":
		return left.Target < right.Target
	default:
		return left.Namespace+"/"+left.Name < right.Namespace+"/"+right.Name
	}
}

// absReplicaDiff returns the absolute difference between desired and current replicas.
func absReplicaDiff(item hpaanalysis.ListItem) int32 {
	diff := item.Desired - item.Current
	if diff < 0 {
		return -diff
	}
	return diff
}

// problemLess orders by worst health score first, then largest replica drift, then namespace/name tiebreak.
func problemLess(left, right hpaanalysis.ListItem) bool {
	if left.HealthScore != right.HealthScore {
		return left.HealthScore < right.HealthScore
	}
	diffLeft, diffRight := absReplicaDiff(left), absReplicaDiff(right)
	if diffLeft != diffRight {
		return diffLeft > diffRight
	}
	return left.Namespace+"/"+left.Name < right.Namespace+"/"+right.Name
}
