package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type compareReport struct {
	From        string        `json:"from" yaml:"from"`
	To          string        `json:"to" yaml:"to"`
	Differences []compareDiff `json:"differences" yaml:"differences"`
	Risks       []string      `json:"risks,omitempty" yaml:"risks,omitempty"`
}

type compareListReport struct {
	Items []compareReport `json:"items" yaml:"items"`
}

type compareDiff struct {
	Field string `json:"field" yaml:"field"`
	From  string `json:"from" yaml:"from"`
	To    string `json:"to" yaml:"to"`
}

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
		return err
	}
	toClient, err := newCompareClient(opts, toContext)
	if err != nil {
		return err
	}
	fromHPA, fromLabel, err := getCompareHPA(ctx, fromClient, fromRef)
	if err != nil {
		return err
	}
	toHPA, toLabel, err := getCompareHPA(ctx, toClient, toRef)
	if err != nil {
		return err
	}
	report := buildCompareReport(fromLabel, toLabel, fromHPA, toHPA)
	return writeOutput(out, opts.Output, opts.Template, report, func() error {
		_, err := fmt.Fprintf(out, "HPA Compare: %s -> %s\n\n", report.From, report.To)
		if err != nil {
			return err
		}
		if len(report.Differences) == 0 {
			if _, err := fmt.Fprintln(out, "Different:\n  none"); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(out, "Different:"); err != nil {
				return err
			}
			for _, diff := range report.Differences {
				if _, err := fmt.Fprintf(out, "  %s: from=%s to=%s\n", diff.Field, diff.From, diff.To); err != nil {
					return err
				}
			}
		}
		if len(report.Risks) > 0 {
			if _, err := fmt.Fprintln(out, "\nRisk:"); err != nil {
				return err
			}
			for _, risk := range report.Risks {
				if _, err := fmt.Fprintf(out, "  - %s\n", risk); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func runCompareAll(ctx context.Context, out io.Writer, opts *options, fromContext, toContext string, onlyDrift bool) error {
	fromClient, err := newCompareClient(opts, fromContext)
	if err != nil {
		return err
	}
	toClient, err := newCompareClient(opts, toContext)
	if err != nil {
		return err
	}
	fromHPAs, err := fromClient.ListHPAs(ctx, metav1.NamespaceAll, metav1.ListOptions{LabelSelector: opts.Selector}, opts.ChunkSize)
	if err != nil {
		return err
	}
	toHPAs, err := toClient.ListHPAs(ctx, metav1.NamespaceAll, metav1.ListOptions{LabelSelector: opts.Selector}, opts.ChunkSize)
	if err != nil {
		return err
	}
	toMap := map[string]*autoscalingv2.HorizontalPodAutoscaler{}
	for i := range toHPAs.Items {
		hpa := &toHPAs.Items[i]
		toMap[hpa.Namespace+"/"+hpa.Name] = hpa
	}
	var reports []compareReport
	for i := range fromHPAs.Items {
		from := &fromHPAs.Items[i]
		key := from.Namespace + "/" + from.Name
		to := toMap[key]
		if to == nil {
			reports = append(reports, compareReport{From: key, To: "<missing>", Differences: []compareDiff{{Field: "exists", From: "true", To: "false"}}, Risks: []string{"target environment is missing this HPA"}})
			continue
		}
		report := buildCompareReport(key, key, from, to)
		if !onlyDrift || len(report.Differences) > 0 {
			reports = append(reports, report)
		}
	}
	list := compareListReport{Items: reports}
	return writeOutput(out, opts.Output, opts.Template, list, func() error {
		if len(reports) == 0 {
			_, err := fmt.Fprintln(out, "No HPA drift found.")
			return err
		}
		for _, report := range reports {
			_, _ = fmt.Fprintf(out, "HPA drift: %s -> %s\n", report.From, report.To)
			for _, diff := range report.Differences {
				_, _ = fmt.Fprintf(out, "  - %s: %s -> %s\n", diff.Field, diff.From, diff.To)
			}
			for _, risk := range report.Risks {
				_, _ = fmt.Fprintf(out, "  risk: %s\n", risk)
			}
		}
		return nil
	})
}

func newCompareClient(opts *options, contextName string) (*kube.Client, error) {
	clone := copyOptions(opts)
	if contextName != "" {
		clone.ContextName = contextName
	}
	return clone.newClient()
}

func getCompareHPA(ctx context.Context, client *kube.Client, ref string) (*autoscalingv2.HorizontalPodAutoscaler, string, error) {
	namespace, name := splitNamespacedRef(ref, client.Namespace)
	hpa, err := client.Interface.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, "", err
	}
	return hpa, namespace + "/" + name, nil
}

func splitNamespacedRef(ref, defaultNamespace string) (string, string) {
	if ns, name, ok := strings.Cut(ref, "/"); ok {
		return ns, name
	}
	return defaultNamespace, ref
}

func buildCompareReport(fromLabel, toLabel string, from, to *autoscalingv2.HorizontalPodAutoscaler) compareReport {
	report := compareReport{From: fromLabel, To: toLabel}
	addDiff := func(field, left, right string) {
		if left != right {
			report.Differences = append(report.Differences, compareDiff{Field: field, From: left, To: right})
		}
	}
	addDiff("minReplicas", fmt.Sprintf("%d", replicasOrDefault(from.Spec.MinReplicas)), fmt.Sprintf("%d", replicasOrDefault(to.Spec.MinReplicas)))
	addDiff("maxReplicas", fmt.Sprintf("%d", from.Spec.MaxReplicas), fmt.Sprintf("%d", to.Spec.MaxReplicas))
	addDiff("metrics", compareMetricSummary(from), compareMetricSummary(to))
	addDiff("behavior.scaleDown.stabilizationWindowSeconds", stabilizationWindowString(from), stabilizationWindowString(to))
	addDiff("healthScore", fmt.Sprintf("%d", hpaanalysis.Analyze(from, false).HealthScore), fmt.Sprintf("%d", hpaanalysis.Analyze(to, false).HealthScore))
	if to.Spec.MaxReplicas < from.Spec.MaxReplicas {
		report.Risks = append(report.Risks, "target environment has lower maxReplicas and is more likely to hit a replica cap under the same load")
	}
	return report
}

func compareMetricSummary(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	parts := make([]string, 0, len(hpa.Spec.Metrics))
	for _, metric := range hpa.Spec.Metrics {
		switch {
		case metric.Resource != nil:
			parts = append(parts, fmt.Sprintf("Resource/%s=%s", metric.Resource.Name, hpaanalysis.FormatMetricTarget(metric.Resource.Target)))
		case metric.ContainerResource != nil:
			parts = append(parts, fmt.Sprintf("ContainerResource/%s/%s=%s", metric.ContainerResource.Container, metric.ContainerResource.Name, hpaanalysis.FormatMetricTarget(metric.ContainerResource.Target)))
		case metric.External != nil:
			parts = append(parts, fmt.Sprintf("External/%s=%s", metric.External.Metric.Name, hpaanalysis.FormatMetricTarget(metric.External.Target)))
		case metric.Pods != nil:
			parts = append(parts, fmt.Sprintf("Pods/%s=%s", metric.Pods.Metric.Name, hpaanalysis.FormatMetricTarget(metric.Pods.Target)))
		case metric.Object != nil:
			parts = append(parts, fmt.Sprintf("Object/%s=%s", metric.Object.Metric.Name, hpaanalysis.FormatMetricTarget(metric.Object.Target)))
		}
	}
	return strings.Join(parts, ",")
}

func stabilizationWindowString(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil || hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil {
		return "<default>"
	}
	return fmt.Sprintf("%d", *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds)
}
