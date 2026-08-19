package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/render"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/compare"
)

func newCompareCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compare [FROM TO]",
		Short: "Compare HPA configuration and visible status across contexts or namespaces",
		Args:  cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			fromContext, _ := cmd.Flags().GetString("from-context")
			toContext, _ := cmd.Flags().GetString("to-context")
			onlyDrift, _ := cmd.Flags().GetBool("only-drift")
			if opts.AllNamespaces && len(args) == 0 {
				return runCompareAll(cmd.Context(), cmd.OutOrStdout(), opts, fromContext, toContext, onlyDrift)
			}
			if len(args) != 2 {
				return fmt.Errorf("compare requires FROM TO unless -A is used")
			}
			return runCompare(cmd.Context(), cmd.OutOrStdout(), opts, args[0], args[1], fromContext, toContext)
		},
	}
	cmd.Flags().String("from-context", "", "kubeconfig context for FROM")
	cmd.Flags().String("to-context", "", "kubeconfig context for TO")
	cmd.Flags().Bool("only-drift", false, "with -A, show only HPAs that differ")
	return cmd
}

func runCompare(ctx context.Context, out io.Writer, opts *options, fromRef, toRef, fromContext, toContext string) error {
	fromClient, err := newCompareClient(opts, fromContext)
	if err != nil {
		return fmt.Errorf("creating FROM client: %w", err)
	}
	toClient, err := newCompareClient(opts, toContext)
	if err != nil {
		return fmt.Errorf("creating TO client: %w", err)
	}
	fromHPA, fromLabel, err := getCompareHPA(ctx, fromClient, fromRef)
	if err != nil {
		return fmt.Errorf("fetching FROM HPA %s: %w", fromRef, err)
	}
	toHPA, toLabel, err := getCompareHPA(ctx, toClient, toRef)
	if err != nil {
		return fmt.Errorf("fetching TO HPA %s: %w", toRef, err)
	}
	report := compare.BuildReport(fromLabel, toLabel, fromHPA, toHPA)
	return render.Format(out, opts.Output, opts.Template, report, func(out io.Writer) error {
		return writeCompareText(out, report)
	})
}

// writeCompareText renders the human-readable single-pair compare output.
func writeCompareText(out io.Writer, report compare.Report) error {
	if _, err := fmt.Fprintf(out, "HPA Compare: %s -> %s\n\n", report.From, report.To); err != nil {
		return fmt.Errorf("write compare header: %w", err)
	}
	if len(report.Differences) == 0 {
		if _, err := fmt.Fprintln(out, "Different:\n  none"); err != nil {
			return fmt.Errorf("write compare differences: %w", err)
		}
	} else {
		if _, err := fmt.Fprintln(out, "Different:"); err != nil {
			return fmt.Errorf("write compare differences: %w", err)
		}
		for _, diff := range report.Differences {
			if _, err := fmt.Fprintf(out, "  %s: from=%s to=%s\n", diff.Field, diff.From, diff.To); err != nil {
				return fmt.Errorf("write compare differences: %w", err)
			}
		}
	}
	if len(report.Risks) > 0 {
		if _, err := fmt.Fprintln(out, "\nRisk:"); err != nil {
			return fmt.Errorf("write compare risks: %w", err)
		}
		for _, risk := range report.Risks {
			if _, err := fmt.Fprintf(out, "  - %s\n", risk); err != nil {
				return fmt.Errorf("write compare risks: %w", err)
			}
		}
	}
	return nil
}

func runCompareAll(ctx context.Context, out io.Writer, opts *options, fromContext, toContext string, onlyDrift bool) error {
	fromClient, err := newCompareClient(opts, fromContext)
	if err != nil {
		return fmt.Errorf("creating FROM client: %w", err)
	}
	toClient, err := newCompareClient(opts, toContext)
	if err != nil {
		return fmt.Errorf("creating TO client: %w", err)
	}
	fromHPAs, err := fromClient.ListHPAs(ctx, metav1.NamespaceAll, metav1.ListOptions{LabelSelector: opts.Selector}, opts.ChunkSize)
	if err != nil {
		return fmt.Errorf("listing FROM HPAs: %w", err)
	}
	toHPAs, err := toClient.ListHPAs(ctx, metav1.NamespaceAll, metav1.ListOptions{LabelSelector: opts.Selector}, opts.ChunkSize)
	if err != nil {
		return fmt.Errorf("listing TO HPAs: %w", err)
	}
	toMap := map[string]*autoscalingv2.HorizontalPodAutoscaler{}
	for i := range toHPAs.Items {
		hpa := &toHPAs.Items[i]
		toMap[hpa.Namespace+"/"+hpa.Name] = hpa
	}
	var reports []compare.Report
	for i := range fromHPAs.Items {
		from := &fromHPAs.Items[i]
		key := from.Namespace + "/" + from.Name
		to := toMap[key]
		if to == nil {
			reports = append(reports, compare.Report{From: key, To: "<missing>", Differences: []compare.Diff{{Field: "exists", From: "true", To: "false"}}, Risks: []string{"target environment is missing this HPA"}})
			continue
		}
		report := compare.BuildReport(key, key, from, to)
		if !onlyDrift || len(report.Differences) > 0 {
			reports = append(reports, report)
		}
	}
	list := compare.ListReport{Items: reports}
	return render.Format(out, opts.Output, opts.Template, list, func(out io.Writer) error {
		return renderCompareDriftText(out, reports)
	})
}

func renderCompareDriftText(out io.Writer, reports []compare.Report) error {
	if len(reports) == 0 {
		if _, err := fmt.Fprintln(out, "No HPA drift found."); err != nil {
			return fmt.Errorf("write compare drift: %w", err)
		}
		return nil
	}
	for _, report := range reports {
		if _, err := fmt.Fprintf(out, "HPA drift: %s -> %s\n", report.From, report.To); err != nil {
			return fmt.Errorf("write compare drift: %w", err)
		}
		for _, diff := range report.Differences {
			if _, err := fmt.Fprintf(out, "  - %s: %s -> %s\n", diff.Field, diff.From, diff.To); err != nil {
				return fmt.Errorf("write compare drift: %w", err)
			}
		}
		for _, risk := range report.Risks {
			if _, err := fmt.Fprintf(out, "  risk: %s\n", risk); err != nil {
				return fmt.Errorf("write compare drift: %w", err)
			}
		}
	}
	return nil
}

func newCompareClient(opts *options, contextName string) (*kube.Client, error) {
	// Intentional opts.NewClient() bypass (see client_helpers.go): each compare
	// side builds a per-context client from its own option clone, and the
	// caller wraps failures per compare pair instead of using the standard
	// "failed to create Kubernetes client" abort.
	clone := copyOptions(opts)
	if contextName != "" {
		clone.ContextName = contextName
	}
	return clone.NewClient()
}

func getCompareHPA(ctx context.Context, client *kube.Client, ref string) (*autoscalingv2.HorizontalPodAutoscaler, string, error) {
	namespace, name := splitNamespacedRef(ref, client.Namespace)
	hpa, err := kube.GetHPA(ctx, client.Interface, namespace, name)
	if err != nil {
		return nil, "", wrapHPALookupError(namespace, name, err)
	}
	return hpa, namespace + "/" + name, nil
}

func splitNamespacedRef(ref, defaultNamespace string) (string, string) {
	if ns, name, ok := strings.Cut(ref, "/"); ok {
		return ns, name
	}
	return defaultNamespace, ref
}
