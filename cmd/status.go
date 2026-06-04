package cmd

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newStatusCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "status NAME [NAME...]",
		Short:             "Show concise status for one or more HPAs",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			includeInterpretation := (opts.interpret || opts.explain || opts.suggest) && !opts.noInterpret
			if opts.watch {
				if len(args) != 1 {
					return fmt.Errorf("--watch supports exactly one HPA name")
				}
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], includeInterpretation)
			}
			return runStatusMany(cmd.Context(), cmd.OutOrStdout(), opts, args, includeInterpretation)
		},
	}
}

func newAnalyzeCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "analyze NAME [NAME...]",
		Aliases:           []string{"diagnose"},
		Short:             "Analyze one or more HPAs using visible Kubernetes API signals",
		Deprecated:        "Use 'status NAME --explain' instead. Example: kubectl-hpa-status status my-hpa --explain. The analyze subcommand will be removed in a future release.",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.watch {
				if len(args) != 1 {
					return fmt.Errorf("--watch supports exactly one HPA name")
				}
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
			}
			return runStatusMany(cmd.Context(), cmd.OutOrStdout(), opts, args, !opts.noInterpret)
		},
	}
}

func runStatus(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	return runStatusMany(ctx, out, opts, []string{name}, includeInterpretation)
}

func runStatusMany(ctx context.Context, out io.Writer, opts *options, names []string, includeInterpretation bool) error {
	watchMode := opts.watch
	ec := newEnrichmentContext(ctx, opts)

	if len(names) == 1 {
		report, err := buildStatusReport(ctx, opts, names[0], includeInterpretation, ec)
		if err != nil {
			if opts.output == "json" || opts.output == "yaml" {
				writeError(out, opts.output, err)
			}
			return err
		}
		if opts.apply {
			applied, err := applySuggestions(ctx, out, opts, names[0], report.Analysis.Suggestions)
			if err != nil {
				return err
			}
			report.Analysis.Actions = append(report.Analysis.Actions, applied...)
		}

		format, templateStr := outputSelection(opts)
		if err := writeOutput(out, format, templateStr, report, func() error {
			return hpaanalysis.WriteStatusTextWithOptions(out, report, hpaanalysis.StatusTextOptions{
				Theme: style.NewTheme(shouldColorize(opts.color, out)),
				Lang:  outputLang(opts),
				Fix:   opts.fix,
				Diff:  opts.diff,
				Labels: labelProviderForOpts(opts),
			})
		}); err != nil {
			return err
		}
		return warningExitCode(report.Analysis.Health, report.Analysis.Name, report.Analysis.Namespace, watchMode)
	}

	reports := make([]hpaanalysis.StatusReport, len(names))
	g, gctx := errgroup.WithContext(ctx)
	limit := runtime.NumCPU()
	if limit < 1 {
		limit = 1
	}
	g.SetLimit(limit)

	for i, name := range names {
		i, name := i, name
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			report, err := buildStatusReport(gctx, opts, name, includeInterpretation, ec)
			if err != nil {
				if opts.output == "json" || opts.output == "yaml" {
					writeError(out, opts.output, err)
				}
				return err
			}
			if opts.apply {
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
		return err
	}

	format, templateStr := outputSelection(opts)
	if err := writeOutput(out, format, templateStr, reports, func() error {
		for i, report := range reports {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if err := hpaanalysis.WriteStatusTextWithOptions(out, report, hpaanalysis.StatusTextOptions{
				Theme: style.NewTheme(shouldColorize(opts.color, out)),
				Lang:  outputLang(opts),
				Fix:   opts.fix,
				Diff:  opts.diff,
				Labels: labelProviderForOpts(opts),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// Return warning exit code if any HPA has ERROR or LIMITED health.
	for _, r := range reports {
		if err := warningExitCode(r.Analysis.Health, r.Analysis.Name, r.Analysis.Namespace, watchMode); err != nil {
			return err
		}
	}
	return nil
}

func buildStatusReport(ctx context.Context, opts *options, name string, includeInterpretation bool, ec *enrichmentContext) (hpaanalysis.StatusReport, error) {
	client, err := opts.newClient()
	if err != nil {
		return hpaanalysis.StatusReport{}, fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
	}

	hpa, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(client.Namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return hpaanalysis.StatusReport{}, fmt.Errorf("HPA %q was not found in namespace %q. "+
				"If the cluster is running Kubernetes older than 1.23, the autoscaling/v2 API may not be available. "+
				"Check with: kubectl api-resources | grep autoscaling. Original error: %w", name, client.Namespace, err)
		}
		if apierrors.IsMethodNotSupported(err) {
			return hpaanalysis.StatusReport{}, fmt.Errorf("the Kubernetes API server does not support the autoscaling/v2 API. "+
				"This plugin requires Kubernetes 1.23+ (stable from 1.26). "+
				"Check with: kubectl api-resources | grep autoscaling. Original error: %w", err)
		}
		return hpaanalysis.StatusReport{}, fmt.Errorf("failed to get HPA %s/%s from the Kubernetes API server: %w", client.Namespace, name, err)
	}

	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.AnalyzeWithOptions(hpa, includeInterpretation, analysisOptions(opts)),
	}

	if opts.events.enabled {
		events, err := hpaanalysis.RecentEvents(ctx, client.Interface, hpa.Namespace, hpa.Name, int64(opts.events.limit))
		if err != nil {
			report.Events = []hpaanalysis.Event{{Reason: "Error", Message: fmt.Sprintf("failed to list events: %v", err)}}
		} else {
			report.Events = events
		}
	}

	enrichReport(ctx, ec, hpa, &report, opts.healthWeights)

	if opts.diagnoseMetrics {
		report.Analysis.MetricsDiagnostics = hpaanalysis.DiagnoseMetricsPipeline(hpa)
	}

	if opts.checkResources {
		resources, err := kube.FetchScaleTargetResources(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name)
		if err == nil && resources != nil {
			report.Analysis.ResourceCheck = hpaanalysis.CheckResourceConsistency(hpa, resources)
		}
	}

	report.Analysis.TargetReplicas = fetchTargetReplicaInfo(ctx, client, hpa)
	if report.Analysis.TargetReplicas != nil && report.Analysis.TargetReplicas.NotReady > 0 {
		tr := report.Analysis.TargetReplicas
		report.Analysis.Interpretation = append(report.Analysis.Interpretation,
			fmt.Sprintf("[confidence: high] %d of %d pods on the scale target are not ready — HPA excludes not-ready pods from utilization calculations, so scaling decisions may not reflect actual workload pressure.", tr.NotReady, tr.TotalReplicas),
		)
		report.Analysis.Actions = append(report.Analysis.Actions,
			fmt.Sprintf("Investigate why %d pod(s) are not ready on the scale target; not-ready pods can cause misleading metric utilization ratios.", tr.NotReady),
		)
	}
	if report.Analysis.TargetReplicas != nil && report.Analysis.TargetReplicas.Pending > 0 {
		tr := report.Analysis.TargetReplicas
		report.Analysis.Interpretation = append(report.Analysis.Interpretation,
			fmt.Sprintf("[confidence: high] %d pod(s) for the scale target are Pending; HPA may be requesting capacity that the cluster has not scheduled yet.", tr.Pending),
		)
		if tr.Unschedulable > 0 {
			report.Analysis.Interpretation = append(report.Analysis.Interpretation,
				fmt.Sprintf("[confidence: high] %d Pending pod(s) are marked Unschedulable, which points to node capacity, taint/toleration, affinity, or quota constraints rather than HPA math.", tr.Unschedulable),
			)
			report.Analysis.Actions = append(report.Analysis.Actions,
				"Check pending Pods, node capacity, Cluster Autoscaler/Karpenter events, quotas, affinity, and taints before raising HPA bounds.",
			)
		}
	}

	if opts.explainPods {
		report.Analysis.PodAnalysis = fetchAndAnalyzePods(ctx, client, hpa)
	}

	if len(opts.simulate) > 0 {
		overrides, simErr := parseSimulateOverrides(opts.simulate)
		if simErr != nil {
			report.Analysis.Interpretation = append(report.Analysis.Interpretation,
				fmt.Sprintf("simulation error: %v", simErr))
		} else {
			sim, simErr := hpaanalysis.SimulateHPA(hpa, overrides, analysisOptions(opts).HealthWeights)
			if simErr != nil {
				report.Analysis.Interpretation = append(report.Analysis.Interpretation,
					fmt.Sprintf("simulation error: %v", simErr))
			} else {
				report.Analysis.Simulation = sim
			}
		}
	}

	if opts.capacityContext {
		report.Analysis.CapacityContext = buildCapacityContext(ctx, client, hpa)
	}

	return report, nil
}

func fetchTargetReplicaInfo(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.TargetReplicaInfo {
	ref := hpa.Spec.ScaleTargetRef
	if ref.Kind != "Deployment" && ref.Kind != "StatefulSet" && ref.Kind != "ReplicaSet" {
		return nil
	}

	switch ref.Kind {
	case "Deployment":
		deploy, err := client.Interface.AppsV1().Deployments(hpa.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		total := deploy.Status.Replicas
		ready := deploy.Status.ReadyReplicas
		notReady := total - ready
		info := &hpaanalysis.TargetReplicaInfo{
			TotalReplicas: total,
			ReadyReplicas: ready,
			NotReady:      notReady,
		}
		enrichPendingPods(ctx, client, hpa.Namespace, metav1.FormatLabelSelector(deploy.Spec.Selector), info)
		if info.NotReady <= 0 && info.Pending <= 0 && info.Unschedulable <= 0 {
			return nil
		}
		return info
	case "StatefulSet":
		sts, err := client.Interface.AppsV1().StatefulSets(hpa.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		total := sts.Status.Replicas
		ready := sts.Status.ReadyReplicas
		notReady := total - ready
		info := &hpaanalysis.TargetReplicaInfo{
			TotalReplicas: total,
			ReadyReplicas: ready,
			NotReady:      notReady,
		}
		enrichPendingPods(ctx, client, hpa.Namespace, metav1.FormatLabelSelector(sts.Spec.Selector), info)
		if info.NotReady <= 0 && info.Pending <= 0 && info.Unschedulable <= 0 {
			return nil
		}
		return info
	case "ReplicaSet":
		rs, err := client.Interface.AppsV1().ReplicaSets(hpa.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		total := rs.Status.Replicas
		ready := rs.Status.ReadyReplicas
		notReady := total - ready
		info := &hpaanalysis.TargetReplicaInfo{
			TotalReplicas: total,
			ReadyReplicas: ready,
			NotReady:      notReady,
		}
		enrichPendingPods(ctx, client, hpa.Namespace, metav1.FormatLabelSelector(rs.Spec.Selector), info)
		if info.NotReady <= 0 && info.Pending <= 0 && info.Unschedulable <= 0 {
			return nil
		}
		return info
	}
	return nil
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
