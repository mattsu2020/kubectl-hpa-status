package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
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

	if len(names) == 1 {
		report, err := buildStatusReport(ctx, opts, names[0], includeInterpretation)
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
			})
		}); err != nil {
			return err
		}
		return warningExitCode(report.Analysis.Health, report.Analysis.Name, report.Analysis.Namespace, watchMode)
	}

	reports := make([]hpaanalysis.StatusReport, 0, len(names))
	for _, name := range names {
		report, err := buildStatusReport(ctx, opts, name, includeInterpretation)
		if err != nil {
			if opts.output == "json" || opts.output == "yaml" {
				writeError(out, opts.output, err)
			}
			return err
		}
		if opts.apply {
			applied, err := applySuggestions(ctx, out, opts, name, report.Analysis.Suggestions)
			if err != nil {
				return err
			}
			report.Analysis.Actions = append(report.Analysis.Actions, applied...)
		}
		reports = append(reports, report)
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

func buildStatusReport(ctx context.Context, opts *options, name string, includeInterpretation bool) (hpaanalysis.StatusReport, error) {
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

	if opts.keda {
		report.Analysis.KEDAInfo = enrichKEDA(ctx, opts, hpa)
	}

	if opts.vpa {
		enrichVPA(ctx, opts, hpa, &report)
	} else if isKEDA, _ := kube.DetectKEDA(hpa); isKEDA && includeInterpretation {
		// Auto-attempt KEDA enrichment when KEDA labels are detected and interpretation is enabled
		report.Analysis.KEDAInfo = enrichKEDA(ctx, opts, hpa)
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
		if notReady <= 0 {
			return nil
		}
		return &hpaanalysis.TargetReplicaInfo{
			TotalReplicas: total,
			ReadyReplicas: ready,
			NotReady:      notReady,
		}
	case "StatefulSet":
		sts, err := client.Interface.AppsV1().StatefulSets(hpa.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		total := sts.Status.Replicas
		ready := sts.Status.ReadyReplicas
		notReady := total - ready
		if notReady <= 0 {
			return nil
		}
		return &hpaanalysis.TargetReplicaInfo{
			TotalReplicas: total,
			ReadyReplicas: ready,
			NotReady:      notReady,
		}
	}
	return nil
}

func enrichKEDA(ctx context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.KEDAAnalysis {
	isKEDA, _ := kube.DetectKEDA(hpa)
	if !isKEDA {
		return nil
	}

	dynClient, _, err := kube.NewDynamicClient(kube.Options{
		Namespace:  opts.namespace,
		Context:    opts.contextName,
		Kubeconfig: opts.kubeconfig,
		Cluster:    opts.cluster,
	})
	if err != nil {
		return &hpaanalysis.KEDAAnalysis{
			Lines: []string{fmt.Sprintf("[confidence: high] HPA appears KEDA-managed but dynamic client failed: %v", err)},
		}
	}

	scaledObject, err := kube.FindScaledObjectForHPA(ctx, dynClient, nil, hpa)
	if err != nil {
		return &hpaanalysis.KEDAAnalysis{
			Lines: []string{fmt.Sprintf("[confidence: high] HPA appears KEDA-managed but no ScaledObject found: %v", err)},
		}
	}

	info := kube.ExtractKEDAInfo(scaledObject)

	triggers := make([]hpaanalysis.KEDATriggerSummary, 0, len(info.Triggers))
	for _, t := range info.Triggers {
		triggers = append(triggers, hpaanalysis.KEDATriggerSummary{
			Type: t.Type,
			Name: t.Name,
		})
	}

	var conditionLines []string
	for _, c := range info.Conditions {
		if strings.EqualFold(c.Status, "False") {
			conditionLines = append(conditionLines, fmt.Sprintf("condition %q is False (reason: %s): %s", c.Type, c.Reason, c.Message))
		}
	}

	kedaAnalysis := &hpaanalysis.KEDAAnalysis{
		ScaledObjectName: info.ScaledObjectName,
		Triggers:         triggers,
		PollingInterval:  info.PollingInterval,
		CooldownPeriod:   info.CooldownPeriod,
		MinReplicaCount:  info.MinReplicaCount,
		MaxReplicaCount:  info.MaxReplicaCount,
		Lines:            conditionLines,
	}

	if len(conditionLines) == 0 && len(info.Conditions) > 0 {
		kedaAnalysis.Lines = []string{fmt.Sprintf("ScaledObject reports %d condition(s), all healthy.", len(info.Conditions))}
	}

	// Add KEDA interpretation lines to the analysis.
	kedaAnalysis.Lines = append(kedaAnalysis.Lines, hpaanalysis.AnalyzeKEDA(hpa, kedaAnalysis)...)

	return kedaAnalysis
}

func enrichVPA(ctx context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	dynClient, _, err := kube.NewDynamicClient(kube.Options{
		Namespace:  opts.namespace,
		Context:    opts.contextName,
		Kubeconfig: opts.kubeconfig,
		Cluster:    opts.cluster,
	})
	if err != nil {
		// VPA errors should not fail the command; skip silently.
		return
	}

	vpaInfo, err := kube.FindConflictingVPA(ctx, dynClient, report.Analysis.Namespace, hpa)
	if err != nil {
		// VPA CRD may not be installed; skip silently.
		return
	}
	if vpaInfo == nil {
		return
	}

	conflict := &hpaanalysis.VPAConflictInfo{
		VPAName:    vpaInfo.Name,
		TargetKind: vpaInfo.TargetKind,
		TargetName: vpaInfo.TargetName,
		Warning:    fmt.Sprintf("VPA %s and HPA both target %s/%s with overlapping resource metrics", vpaInfo.Name, vpaInfo.TargetKind, vpaInfo.TargetName),
	}
	report.Analysis.VPAConflict = conflict
	report.Analysis.Interpretation = append(report.Analysis.Interpretation, hpaanalysis.AnalyzeVPA(hpa, vpaInfo)...)
}
