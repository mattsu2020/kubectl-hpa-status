// Package compare analyzes drift between two HPA configurations by comparing
// their spec fields, metric definitions, behavior settings, and health scores.
//
// It is a self-contained domain that depends only on the autoscaling/v2 API
// types plus the shared pkg/hpa/core formatting helpers and pkg/hpa/model
// constants. The package is pure: every exported function operates on the
// HPAs passed in and does not touch the network or the local filesystem.
// Health scores are computed by a caller-supplied HealthScorer (see
// BuildReportWithScorer), which keeps this package independent of the
// analysis root that itself embeds compare-adjacent report types.
//
// The primary entry point is BuildReport, which produces a Report listing the
// differences between a FROM and a TO HPA along with any risks the drift
// introduces (for example, a lower maxReplicas in the target environment).
// Callers that render the report for humans or marshal it to JSON consume the
// same Report type, so field names and JSON tags are part of the package's
// stability contract.
package compare

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/core"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/model"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// Report describes the differences observed between two HPAs.
type Report struct {
	From        string   `json:"from" yaml:"from"`
	To          string   `json:"to" yaml:"to"`
	Differences []Diff   `json:"differences" yaml:"differences"`
	Risks       []string `json:"risks,omitempty" yaml:"risks,omitempty"`
}

// ListReport aggregates multiple Reports, used when comparing sets of HPAs
// across contexts or namespaces.
type ListReport struct {
	Items []Report `json:"items" yaml:"items"`
}

// Diff describes a single field-level difference between the FROM and TO HPAs.
type Diff struct {
	Field string `json:"field" yaml:"field"`
	From  string `json:"from" yaml:"from"`
	To    string `json:"to" yaml:"to"`
}

// HealthScorer computes the health score (0-100) of an HPA for the
// healthScore diff line. Callers that have the full analysis pipeline
// available (such as cmd) pass a closure over it; the compare package stays
// free of the analysis-root dependency that would invite an import cycle.
type HealthScorer func(hpa *autoscalingv2.HorizontalPodAutoscaler) int

// BuildReport compares two HPAs and returns a Report describing their
// differences. fromLabel and toLabel identify the two sides (typically
// namespace/name pairs); from and to are the HPA objects being compared.
//
// The function compares minReplicas, maxReplicas, metric definitions, and
// behavior settings. The healthScore line is omitted; use
// BuildReportWithScorer to include it.
func BuildReport(fromLabel, toLabel string, from, to *autoscalingv2.HorizontalPodAutoscaler) Report {
	return BuildReportWithScorer(fromLabel, toLabel, from, to, nil)
}

// BuildReportWithScorer behaves like BuildReport and additionally compares
// health scores through the supplied scorer. A nil scorer skips the
// healthScore diff instead of guessing a score.
func BuildReportWithScorer(fromLabel, toLabel string, from, to *autoscalingv2.HorizontalPodAutoscaler, scorer HealthScorer) Report {
	report := Report{From: fromLabel, To: toLabel}
	addDiff := func(field, left, right string) {
		if left != right {
			report.Differences = append(report.Differences, Diff{Field: field, From: left, To: right})
		}
	}
	addDiff("minReplicas", strconv.FormatInt(int64(replicasOrDefault(from.Spec.MinReplicas)), 10), strconv.FormatInt(int64(replicasOrDefault(to.Spec.MinReplicas)), 10))
	addDiff("maxReplicas", strconv.FormatInt(int64(from.Spec.MaxReplicas), 10), strconv.FormatInt(int64(to.Spec.MaxReplicas), 10))
	addDiff("metrics", MetricSummary(from), MetricSummary(to))
	addDiff("behavior.scaleDown.stabilizationWindowSeconds", StabilizationWindow(from), StabilizationWindow(to))
	if scorer != nil {
		addDiff("healthScore", strconv.Itoa(scorer(from)), strconv.Itoa(scorer(to)))
	}
	if to.Spec.MaxReplicas < from.Spec.MaxReplicas {
		report.Risks = append(report.Risks, "target environment has lower maxReplicas and is more likely to hit a replica cap under the same load")
	}
	return report
}

// MetricSummary returns a compact string representation of an HPA's metric
// definitions. Each metric is formatted as "Type/name=target" and joined with
// commas. The format is stable and suitable for comparison.
func MetricSummary(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	parts := make([]string, 0, len(hpa.Spec.Metrics))
	for _, metric := range hpa.Spec.Metrics {
		switch {
		case metric.Resource != nil:
			parts = append(parts, fmt.Sprintf("Resource/%s=%s", metric.Resource.Name, core.FormatMetricTarget(metric.Resource.Target)))
		case metric.ContainerResource != nil:
			parts = append(parts, fmt.Sprintf("ContainerResource/%s/%s=%s", metric.ContainerResource.Container, metric.ContainerResource.Name, core.FormatMetricTarget(metric.ContainerResource.Target)))
		case metric.External != nil:
			parts = append(parts, fmt.Sprintf("External/%s=%s", metric.External.Metric.Name, core.FormatMetricTarget(metric.External.Target)))
		case metric.Pods != nil:
			parts = append(parts, fmt.Sprintf("Pods/%s=%s", metric.Pods.Metric.Name, core.FormatMetricTarget(metric.Pods.Target)))
		case metric.Object != nil:
			parts = append(parts, fmt.Sprintf("Object/%s=%s", metric.Object.Metric.Name, core.FormatMetricTarget(metric.Object.Target)))
		}
	}
	return strings.Join(parts, ",")
}

// StabilizationWindow returns a string representation of the HPA's scale-down
// stabilization window. Returns "<default>" when no explicit window is set.
func StabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil || hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil {
		return "<default>"
	}
	return strconv.FormatInt(int64(*hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds), 10)
}

// replicasOrDefault returns the value or the default minimum replica count if nil.
func replicasOrDefault(replicas *int32) int32 {
	if replicas == nil {
		return model.DefaultMinReplicas
	}
	return *replicas
}
