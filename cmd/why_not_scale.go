package cmd

import (
	"context"
	"fmt"
	"io"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
)

type whyNotScaleReport struct {
	Namespace        string   `json:"namespace" yaml:"namespace"`
	Name             string   `json:"name" yaml:"name"`
	Target           string   `json:"target" yaml:"target"`
	Summary          string   `json:"summary" yaml:"summary"`
	Observed         []string `json:"observed" yaml:"observed"`
	PossibleBlockers []string `json:"possibleBlockers,omitempty" yaml:"possibleBlockers,omitempty"`
	Estimated        []string `json:"estimated,omitempty" yaml:"estimated,omitempty"`
	Unknown          []string `json:"unknown,omitempty" yaml:"unknown,omitempty"`
	NextChecks       []string `json:"nextChecks,omitempty" yaml:"nextChecks,omitempty"`
}

type whyNotScaleListReport struct {
	Items []whyNotScaleReport `json:"items" yaml:"items"`
}

func newWhyNotScaleCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "why-not-scale NAME [NAME...]",
		Aliases:           []string{"why"},
		Short:             "Explain why an HPA is not visibly scaling",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWhyNotScale(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runWhyNotScale(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := *opts
	local.explain = true
	local.diagnoseMetrics = true
	local.metricsFreshness = true
	local.readinessImpact = true
	local.scalePath = true
	local.capacityHeadroom = true
	local.events.enabled = true
	if local.events.limit == 0 {
		local.events.limit = 10
	}

	ec := newEnrichmentContext(ctx, &local)
	reports := make([]whyNotScaleReport, 0, len(names))
	for _, name := range names {
		status, err := buildStatusReportWithClient(ctx, &local, name, true, ec)
		if err != nil {
			return err
		}
		reports = append(reports, buildWhyNotScaleReport(status))
	}

	var value any
	if len(reports) == 1 {
		value = reports[0]
	} else {
		value = whyNotScaleListReport{Items: reports}
	}
	return writeOutput(out, opts.output, opts.template, value, func() error {
		writeWhyNotScaleText(out, reports)
		return nil
	})
}

func buildWhyNotScaleReport(status hpaanalysis.StatusReport) whyNotScaleReport {
	a := status.Analysis
	report := whyNotScaleReport{
		Namespace:  a.Namespace,
		Name:       a.Name,
		Target:     a.Target,
		Summary:    whyNotScaleSummary(a),
		Estimated:  whyNotScaleEstimated(a),
		Unknown:    whyNotScaleUnknown(a),
		NextChecks: whyNotScaleNextChecks(a),
	}

	report.Observed = append(report.Observed,
		fmt.Sprintf("replicas: current=%d desired=%d min=%d max=%d", a.Current, a.Desired, a.Min, a.Max),
		fmt.Sprintf("health: %s score=%d", a.Health, a.HealthScore),
	)
	for _, metric := range a.Metrics {
		if metric.Ratio != nil {
			report.Observed = append(report.Observed,
				fmt.Sprintf("%s metric %s ratio=%.2f current=%s target=%s", metric.Type, metric.Name, *metric.Ratio, metric.Current, metric.Target))
		} else if metric.Text != "" {
			report.Observed = append(report.Observed, metric.Text)
		}
	}
	for _, condition := range a.Conditions {
		report.Observed = append(report.Observed,
			fmt.Sprintf("%s=%s reason=%s", condition.Type, condition.Status, condition.Reason))
	}
	if a.TargetReplicas != nil {
		tr := a.TargetReplicas
		report.Observed = append(report.Observed,
			fmt.Sprintf("target pods: ready=%d notReady=%d pending=%d total=%d", tr.ReadyReplicas, tr.NotReady, tr.Pending, tr.TotalReplicas))
	}
	if a.ReadinessImpact != nil && a.ReadinessImpact.LikelyAffected {
		report.Observed = append(report.Observed, a.ReadinessImpact.Evidence...)
	}

	report.PossibleBlockers = append(report.PossibleBlockers, whyNotScaleBlockers(a)...)
	if len(report.PossibleBlockers) == 0 {
		report.PossibleBlockers = append(report.PossibleBlockers, "no hard blocker is visible from HPA status; controller tolerance, stabilization, or metric freshness may still explain no visible change")
	}
	return report
}

func whyNotScaleEstimated(a hpaanalysis.Analysis) []string {
	var estimated []string

	estimated = appendToleranceEstimates(estimated, a)
	estimated = appendStabilizationEstimates(estimated, a)
	estimated = appendMissingMetricDampeningEstimate(estimated, a)

	return estimated
}

func appendToleranceEstimates(estimated []string, a hpaanalysis.Analysis) []string {
	const defaultTolerance = 0.1

	// Prefer structured decision trace tolerance data when available.
	if sdt := a.StructuredDecisionTrace; sdt != nil && sdt.ToleranceEffect != nil {
		te := sdt.ToleranceEffect
		if len(te.SuppressedMetrics) > 0 {
			for _, name := range te.SuppressedMetrics {
				estimated = append(estimated,
					fmt.Sprintf("tolerance (default %.2f) likely suppressed scaling for metric %s", te.EffectiveTolerance, name))
			}
		} else if te.Note != "" {
			estimated = append(estimated, te.Note)
		}
		return estimated
	}

	// Fall back to MetricDecisionTrace tolerance data.
	if mdt := a.MetricDecisionTrace; mdt != nil && mdt.ToleranceEffect != nil {
		te := mdt.ToleranceEffect
		if len(te.SuppressedMetrics) > 0 {
			for _, name := range te.SuppressedMetrics {
				estimated = append(estimated,
					fmt.Sprintf("tolerance (default %.2f) likely suppressed scaling for metric %s", te.DefaultTolerance, name))
			}
		} else if te.Note != "" {
			estimated = append(estimated, te.Note)
		}
		return estimated
	}

	// Estimate from metric ratios when no trace is available.
	for _, metric := range a.Metrics {
		if metric.Ratio == nil {
			continue
		}
		ratio := *metric.Ratio
		distance := ratio - 1.0
		if distance < 0 {
			distance = -distance
		}
		if distance > 0 && distance <= defaultTolerance {
			estimated = append(estimated,
				fmt.Sprintf("%s metric %s ratio=%.2f is within tolerance band (default %.2f); scaling suppressed", metric.Type, metric.Name, ratio, defaultTolerance))
		}
	}

	return estimated
}

func appendStabilizationEstimates(estimated []string, a hpaanalysis.Analysis) []string {
	// Prefer structured decision trace stabilization data.
	if sdt := a.StructuredDecisionTrace; sdt != nil && sdt.StabilizationEffect != nil {
		se := sdt.StabilizationEffect
		if se.Note != "" {
			estimated = append(estimated, se.Note)
		} else if se.SuppressedDirection != "" {
			estimated = append(estimated,
				fmt.Sprintf("stabilization window may be holding a %s decision", se.SuppressedDirection))
		}
		return estimated
	}

	// Fall back to MetricDecisionTrace stabilization data.
	if mdt := a.MetricDecisionTrace; mdt != nil && mdt.StabilizationEffect != nil {
		se := mdt.StabilizationEffect
		if se.Note != "" {
			estimated = append(estimated, se.Note)
		} else if se.SuppressedScaleDown {
			estimated = append(estimated,
				fmt.Sprintf("scale-down stabilization window (%ds) may be suppressing scale-down", se.WindowSeconds))
		}
		return estimated
	}

	// Estimate from Analysis stabilization fields.
	if a.StabilizationWindowSeconds != nil && *a.StabilizationWindowSeconds > 0 {
		if a.StabilizationRemaining != nil && *a.StabilizationRemaining > 0 {
			estimated = append(estimated,
				fmt.Sprintf("scale-down stabilization window (%ds) may hold recommendation for about %ds", *a.StabilizationWindowSeconds, *a.StabilizationRemaining))
		} else {
			estimated = append(estimated,
				fmt.Sprintf("scale-down stabilization window (%ds) may be delaying scale-down decisions", *a.StabilizationWindowSeconds))
		}
	}

	return estimated
}

func appendMissingMetricDampeningEstimate(estimated []string, a hpaanalysis.Analysis) []string {
	hasMissing := false
	for _, freshness := range a.MetricFreshnessEntries {
		if freshness.Status == string(hpaanalysis.FreshnessMissing) {
			hasMissing = true
			break
		}
	}
	if !hasMissing {
		return estimated
	}
	estimated = append(estimated,
		"missing metrics may cause conservative dampening; the HPA controller may use fewer pods than expected in utilization calculations")
	return estimated
}

func whyNotScaleUnknown(_ hpaanalysis.Analysis) []string {
	unknown := []string{
		"controller-internal per-metric replica recommendations are not exposed by the HPA API",
		"exact missing-metric dampening is not exposed by Kubernetes API",
		"controller-internal recommendation history is not visible",
	}
	return unknown
}

func whyNotScaleSummary(a hpaanalysis.Analysis) string {
	if a.Desired > a.Current {
		return "scale-up is requested by HPA, but the target has not caught up yet"
	}
	if a.Desired < a.Current {
		return "scale-down is requested by HPA, but replicas have not converged yet"
	}
	if a.Current >= a.Max {
		return "HPA is at maxReplicas; additional scale-up is capped"
	}
	if a.Current <= a.Min {
		return "HPA is at minReplicas; additional scale-down is capped"
	}
	if hasMetricPressure(a) {
		return "metric pressure is visible, but desiredReplicas still equals currentReplicas"
	}
	return "no visible scale change is requested in current HPA status"
}

func whyNotScaleBlockers(a hpaanalysis.Analysis) []string {
	var blockers []string
	if a.Current >= a.Max || a.Desired >= a.Max || conditionStatus(a, "ScalingLimited") == "True" {
		blockers = append(blockers, "maxReplicas may be capping scale-up")
	}
	if a.Current <= a.Min || a.Desired <= a.Min {
		blockers = append(blockers, "minReplicas may be capping scale-down")
	}
	if conditionStatus(a, "ScalingActive") == "False" {
		blockers = append(blockers, "ScalingActive=False; HPA cannot compute a valid scaling recommendation")
	}
	if conditionStatus(a, "AbleToScale") == "False" {
		blockers = append(blockers, "AbleToScale=False; controller reports it cannot apply scaling")
	}
	if a.StabilizationRemaining != nil && *a.StabilizationRemaining > 0 {
		blockers = append(blockers, fmt.Sprintf("stabilization window may hold the recommendation for about %ds", *a.StabilizationRemaining))
	}
	if a.TargetReplicas != nil {
		if a.TargetReplicas.Pending > 0 {
			blockers = append(blockers, fmt.Sprintf("%d target pod(s) are Pending", a.TargetReplicas.Pending))
		}
		if a.TargetReplicas.NotReady > 0 {
			blockers = append(blockers, fmt.Sprintf("%d target pod(s) are NotReady", a.TargetReplicas.NotReady))
		}
		if a.TargetReplicas.Unschedulable > 0 {
			blockers = append(blockers, fmt.Sprintf("%d Pending pod(s) are Unschedulable", a.TargetReplicas.Unschedulable))
		}
	}
	if a.ReadinessImpact != nil && a.ReadinessImpact.LikelyAffected {
		blockers = append(blockers, "not-yet-ready pods or missing PodMetrics may dampen HPA CPU/resource decisions")
	}
	for _, freshness := range a.MetricFreshnessEntries {
		if freshness.Status != "" && freshness.Status != string(hpaanalysis.FreshnessOK) {
			blockers = append(blockers, fmt.Sprintf("metric freshness for %s is %s", freshness.Name, freshness.Status))
		}
	}
	return blockers
}

func conditionStatus(a hpaanalysis.Analysis, conditionType string) string {
	for _, condition := range a.Conditions {
		if condition.Type == conditionType {
			return condition.Status
		}
	}
	return ""
}

func hasMetricPressure(a hpaanalysis.Analysis) bool {
	for _, metric := range a.Metrics {
		if metric.Ratio != nil && *metric.Ratio > 1.0 {
			return true
		}
	}
	return false
}

func whyNotScaleNextChecks(a hpaanalysis.Analysis) []string {
	ns := a.Namespace
	name := a.Name
	checks := []string{
		fmt.Sprintf("kubectl describe hpa %s -n %s", name, ns),
		fmt.Sprintf("kubectl top pods -n %s", ns),
	}
	if a.Target != "" {
		checks = append(checks, fmt.Sprintf("kubectl get pods -n %s --show-labels", ns))
		checks = append(checks, fmt.Sprintf("kubectl describe %s -n %s", a.Target, ns))
	}
	if a.Current >= a.Max || a.Desired >= a.Max {
		checks = append(checks, fmt.Sprintf("kubectl hpa_status preflight %s -n %s --raise-max %d", name, ns, a.Max+5))
	}
	return checks
}

func writeWhyNotScaleText(out io.Writer, reports []whyNotScaleReport) {
	for i, report := range reports {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		_, _ = fmt.Fprintf(out, "Why not scale: %s/%s\n", report.Namespace, report.Name)
		_, _ = fmt.Fprintf(out, "Summary: %s\n\n", report.Summary)
		writeWhySection(out, "Observed", report.Observed)
		writeWhySection(out, "Possible blockers", report.PossibleBlockers)
		writeWhySection(out, "Estimated", report.Estimated)
		writeWhySection(out, "Unknown", report.Unknown)
		if len(report.NextChecks) > 0 {
			_, _ = fmt.Fprintln(out, "Next checks:")
			for _, check := range report.NextChecks {
				_, _ = fmt.Fprintf(out, "  %s\n", check)
			}
		}
	}
}

func writeWhySection(out io.Writer, title string, items []string) {
	if len(items) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "%s:\n", title)
	for _, item := range items {
		_, _ = fmt.Fprintf(out, "  - %s\n", item)
	}
	_, _ = fmt.Fprintln(out)
}
