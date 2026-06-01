package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
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

	hpas, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(namespace).
		List(ctx, metav1.ListOptions{LabelSelector: opts.selector})
	if err != nil {
		listErr := fmt.Errorf("failed to list HPAs: %w", err)
		if opts.output == "json" || opts.output == "yaml" {
			writeError(out, opts.output, listErr)
		}
		return listErr
	}

	report := hpaanalysis.ListReport{}
	for i := range hpas.Items {
		item := hpaanalysis.NewListItem(hpaanalysis.AnalyzeWithOptions(&hpas.Items[i], false, analysisOptions(opts)))
		if matchesListFilter(item, filter) && matchesHealthScoreRange(item, opts.healthScoreMin, opts.healthScoreMax) {
			report.Items = append(report.Items, item)
		}
	}
	sortBy := opts.sortBy
	if opts.problem && sortBy == "" {
		sortBy = "problem"
	}
	sortListItems(report.Items, sortBy)

	wide := opts.wide || opts.output == "wide"
	return writeOutput(out, opts.output, opts.template, report, func() error {
		return hpaanalysis.WriteListText(out, report, hpaanalysis.ListTextOptions{
			Wide:  wide,
			Color: shouldColorize(opts.color, out),
			Theme: style.NewTheme(shouldColorize(opts.color, out)),
			Lang:  outputLang(opts),
		})
	})
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
