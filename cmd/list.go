package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
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
			if opts.watch {
				return runWatchList(cmd.Context(), cmd.OutOrStdout(), opts)
			}
			return runList(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.sortBy, "sort-by", "", "sort list by namespace, name, current, desired, diff, health-score, or issue")
	cmd.Flags().StringVar(&opts.filter, "filter", "", "filter list by all, ok, error, limited, scaling-limited, or issue")
	cmd.Flags().IntVar(&opts.healthScoreMax, "health-score", -1, "show only HPAs with health score at or below this threshold")
	cmd.Flags().IntVar(&opts.healthScoreMax, "max-score", -1, "show only HPAs with health score at or below this threshold")
	cmd.Flags().IntVar(&opts.healthScoreMin, "min-score", -1, "show only HPAs with health score at or above this threshold")
	cmd.Flags().BoolVar(&opts.problem, "problem", false, "show only HPAs with visible problems")
	return cmd
}

func newScanCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:     "scan",
		Aliases: []string{"problems"},
		Short:   "Scan all namespaces for HPAs with visible problems",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.allNamespaces = true
			opts.problem = true
			opts.wide = true
			return runList(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
}

func runList(ctx context.Context, out io.Writer, opts *options) error {
	client, err := opts.newClient()
	if err != nil {
		listErr := fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
		if opts.output == "json" || opts.output == "yaml" {
			writeError(out, opts.output, listErr)
		}
		return listErr
	}

	namespace := client.Namespace
	if opts.allNamespaces {
		namespace = metav1.NamespaceAll
	}
	filter := opts.filter
	if opts.problem && filter == "" {
		filter = "issue"
	}

	hpas, err := client.ListHPAs(ctx, namespace, metav1.ListOptions{LabelSelector: opts.selector}, opts.chunkSize)
	if err != nil {
		listErr := fmt.Errorf("failed to list HPAs: %w", err)
		if opts.output == "json" || opts.output == "yaml" {
			writeError(out, opts.output, listErr)
		}
		return listErr
	}

	report := hpaanalysis.ListReport{}
	for i := range hpas.Items {
		item := hpaanalysis.NewListItem(hpaanalysis.AnalyzeWithOptions(&hpas.Items[i], opts.apply, analysisOptions(opts)))
		if matchesListFilter(item, filter) && matchesHealthScoreRange(item, opts.healthScoreMin, opts.healthScoreMax) {
			report.Items = append(report.Items, item)
		}
	}
	sortBy := opts.sortBy
	if opts.problem && sortBy == "" {
		sortBy = "problem"
	}
	sortListItems(report.Items, sortBy)

	if opts.apply {
		if !opts.problem && filter == "" && opts.healthScoreMin <= 0 && opts.healthScoreMax <= 0 {
			return fmt.Errorf("--apply with list requires --problem, --filter, --health-score, --max-score, or --min-score to avoid applying suggestions to an unbounded set")
		}
		if err := applyListSuggestions(ctx, out, opts, hpas.Items, report.Items); err != nil {
			return err
		}
	}

	wide := opts.wide || opts.output == "wide"
	format, templateStr := outputSelection(opts)
	return writeOutput(out, format, templateStr, report, func() error {
		return hpaanalysis.WriteListText(out, report, hpaanalysis.ListTextOptions{
			Wide:  wide,
			Color: shouldColorize(opts.color, out),
			Theme: style.NewTheme(shouldColorize(opts.color, out)),
			Lang:  outputLang(opts),
		})
	})
}

func applyListSuggestions(ctx context.Context, out io.Writer, opts *options, hpas []autoscalingv2.HorizontalPodAutoscaler, items []hpaanalysis.ListItem) error {
	selected := map[string]bool{}
	for _, item := range items {
		selected[item.Namespace+"/"+item.Name] = true
	}

	// Collect selected HPAs with their applicable suggestions.
	type batchEntry struct {
		Namespace  string
		Name       string
		Suggestion hpaanalysis.Suggestion
	}
	var entries []batchEntry
	for i := range hpas {
		hpa := &hpas[i]
		if !selected[hpa.Namespace+"/"+hpa.Name] {
			continue
		}
		analysis := hpaanalysis.AnalyzeWithOptions(hpa, true, analysisOptions(opts))
		for _, s := range analysis.Suggestions {
			if s.Apply && s.Patch != "" {
				entries = append(entries, batchEntry{
					Namespace:  hpa.Namespace,
					Name:       hpa.Name,
					Suggestion: s,
				})
			}
		}
	}

	if len(entries) == 0 {
		if _, err := fmt.Fprintln(out, "No applicable HPA patches found."); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	}

	// Display summary table of all patches.
	seenHPAs := make(map[string]bool)
	for _, e := range entries {
		seenHPAs[e.Namespace+"/"+e.Name] = true
	}
	if _, err := fmt.Fprintf(out, "\nBatch patch summary (%d patches across %d HPA(s)):\n", len(entries), len(seenHPAs)); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if _, err := fmt.Fprintln(out, "  NAMESPACE/NAME                    PATCH                           RISK"); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	for _, e := range entries {
		if _, err := fmt.Fprintf(out, "  %-35s %-30s %s\n", e.Namespace+"/"+e.Name, e.Suggestion.Title, e.Suggestion.Risk); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	// Single confirmation for the entire batch.
	if !opts.yes {
		action := "dry-run"
		if !opts.dryRun {
			action = "apply"
		}
		if _, err := fmt.Fprintf(out, "%s %d patches? [y/N]: ", action, len(entries)); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		if opts.in == nil {
			opts.in = os.Stdin
		}
		scanner := bufio.NewScanner(opts.in)
		if !scanner.Scan() {
			return nil
		}
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer != "y" && answer != "yes" {
			if _, err := fmt.Fprintln(out, "Batch apply skipped."); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			return nil
		}
	}

	// Apply each patch; continue on individual failure.
	var succeeded, failed int
	for _, e := range entries {
		results, err := applySuggestionsInNamespace(ctx, out, opts, e.Namespace, e.Name, []hpaanalysis.Suggestion{e.Suggestion})
		if err != nil {
			if _, err := fmt.Fprintf(out, "  FAILED %s/%s: %v\n", e.Namespace, e.Name, err); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			failed++
			continue
		}
		for _, line := range results {
			if _, err := fmt.Fprintf(out, "%s/%s: %s\n", e.Namespace, e.Name, line); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		succeeded++
	}

	if _, err := fmt.Fprintf(out, "\nBatch complete: %d succeeded, %d failed\n", succeeded, failed); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func matchesListFilter(item hpaanalysis.ListItem, filter string) bool {
	switch normalizeSelector(filter) {
	case "", "all":
		return true
	case "ok":
		return item.Health == "OK"
	case "error":
		return item.Health == "ERROR"
	case "limited", "scalinglimited":
		return item.Health == "LIMITED"
	case "issue":
		return item.Issue != ""
	default:
		return strings.EqualFold(item.Health, filter) || strings.Contains(normalizeSelector(item.Issue), normalizeSelector(filter))
	}
}

func matchesHealthScoreThreshold(item hpaanalysis.ListItem, threshold int) bool {
	return matchesHealthScoreRange(item, -1, threshold)
}

func matchesHealthScoreRange(item hpaanalysis.ListItem, minScore int, maxScore int) bool {
	if minScore > 100 {
		minScore = 100
	}
	if maxScore > 100 {
		maxScore = 100
	}
	if minScore > 0 && item.HealthScore < minScore {
		return false
	}
	if maxScore > 0 && item.HealthScore > maxScore {
		return false
	}
	return true
}

func sortListItems(items []hpaanalysis.ListItem, sortBy string) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
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
			diffLeft := left.Desired - left.Current
			if diffLeft < 0 {
				diffLeft = -diffLeft
			}
			diffRight := right.Desired - right.Current
			if diffRight < 0 {
				diffRight = -diffRight
			}
			return diffLeft > diffRight
		case "age", "creationtimestamp":
			return left.CreationTimestamp.Before(&right.CreationTimestamp)
		case "health":
			return left.Health < right.Health
		case "healthscore", "score":
			return left.HealthScore > right.HealthScore
		case "problem":
			if left.HealthScore != right.HealthScore {
				return left.HealthScore < right.HealthScore
			}
			diffLeft := left.Desired - left.Current
			if diffLeft < 0 {
				diffLeft = -diffLeft
			}
			diffRight := right.Desired - right.Current
			if diffRight < 0 {
				diffRight = -diffRight
			}
			if diffLeft != diffRight {
				return diffLeft > diffRight
			}
			return left.Namespace+"/"+left.Name < right.Namespace+"/"+right.Name
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
	})
}
