// Package compare analyzes drift between two HPA configurations by comparing
// their spec fields, metric definitions, behavior settings, and health scores.
//
// It is a self-contained domain that depends only on the autoscaling/v2 API
// types and the shared analysis helpers from pkg/hpa. The package is pure:
// every exported function operates on the HPAs passed in and does not touch
// the network or the local filesystem.
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
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
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

// BuildReport compares two HPAs and returns a Report describing their
// differences. fromLabel and toLabel identify the two sides (typically
// namespace/name pairs); from and to are the HPA objects being compared.
//
// The function compares minReplicas, maxReplicas, metric definitions, behavior
// settings, and health scores. It also flags risks such as a lower maxReplicas
// in the target environment.
func BuildReport(fromLabel, toLabel string, from, to *autoscalingv2.HorizontalPodAutoscaler) Report {
	report := Report{From: fromLabel, To: toLabel}
	addDiff := func(field, left, right string) {
		if left != right {
			report.Differences = append(report.Differences, Diff{Field: field, From: left, To: right})
		}
	}
	addDiff("minReplicas", fmt.Sprintf("%d", replicasOrDefault(from.Spec.MinReplicas)), fmt.Sprintf("%d", replicasOrDefault(to.Spec.MinReplicas)))
	addDiff("maxReplicas", fmt.Sprintf("%d", from.Spec.MaxReplicas), fmt.Sprintf("%d", to.Spec.MaxReplicas))
	addDiff("metrics", MetricSummary(from), MetricSummary(to))
	addDiff("behavior.scaleDown.stabilizationWindowSeconds", StabilizationWindow(from), StabilizationWindow(to))
	addDiff("healthScore", healthScore(from), healthScore(to))
	if to.Spec.MaxReplicas < from.Spec.MaxReplicas {
		report.Risks = append(report.Risks, "target environment has lower maxReplicas and is more likely to hit a replica cap under the same load")
	}
	return report
}

// healthScore is the narrow adapter between configuration comparison and the
// full HPA analyzer. Keeping the dependency here makes the rest of report
// construction independent of Analysis and its derived output model.
func healthScore(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	analysis := hpaanalysis.Analyze(hpa, false)
	return fmt.Sprintf("%d", analysis.HealthScore())
}

// MetricSummary returns a compact string representation of an HPA's metric
// definitions. Each metric is formatted as "Type/name=target" and joined with
// commas. The format is stable and suitable for comparison.
func MetricSummary(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
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

// StabilizationWindow returns a string representation of the HPA's scale-down
// stabilization window. Returns "<default>" when no explicit window is set.
func StabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil || hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil {
		return "<default>"
	}
	return fmt.Sprintf("%d", *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds)
}

// replicasOrDefault returns the value or the default minimum replica count if nil.
func replicasOrDefault(replicas *int32) int32 {
	if replicas == nil {
		return hpaanalysis.DefaultMinReplicas
	}
	return *replicas
}
