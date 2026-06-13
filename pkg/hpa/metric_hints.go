package hpa

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// MetricHint holds a troubleshooting hint for a custom or external metric issue.
type MetricHint struct {
	MetricType   string   `json:"metricType" yaml:"metricType"`
	MetricName   string   `json:"metricName" yaml:"metricName"`
	Pattern      string   `json:"pattern" yaml:"pattern"`
	Severity     string   `json:"severity" yaml:"severity"`
	Title        string   `json:"title" yaml:"title"`
	Description  string   `json:"description" yaml:"description"`
	Checks       []string `json:"checks,omitempty" yaml:"checks,omitempty"`
	CommonCauses []string `json:"commonCauses,omitempty" yaml:"commonCauses,omitempty"`
	FixSteps     []string `json:"fixSteps,omitempty" yaml:"fixSteps,omitempty"`
}

// MetricHintsReport holds the complete metric hints analysis for an HPA.
type MetricHintsReport struct {
	Namespace            string                      `json:"namespace" yaml:"namespace"`
	Name                 string                      `json:"name" yaml:"name"`
	Hints                []MetricHint                `json:"hints,omitempty" yaml:"hints,omitempty"`
	Summary              string                      `json:"summary" yaml:"summary"`
	TroubleshootingFlows []MetricHintTroubleshooting `json:"troubleshootingFlows,omitempty" yaml:"troubleshootingFlows,omitempty"`
}

// AnalyzeMetricHints examines HPA metric specs against events, freshness data,
// and contract checks to detect common custom/external metric failure patterns.
// Returns nil if the HPA is nil.
func AnalyzeMetricHints(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	events []Event,
	freshness []MetricFreshness,
	contract *MetricContractReport,
) *MetricHintsReport {
	if hpa == nil {
		return nil
	}
	var hints []MetricHint
	cur := hpa.Status.CurrentMetrics
	for _, spec := range hpa.Spec.Metrics {
		hints = append(hints, concatHints(
			checkExternalMetricMissing(spec, events),
			checkExternalMetricStale(spec, freshness),
			checkCustomAPIServiceUnavailable(spec, freshness),
			checkExternalAPIServiceUnavailable(spec, freshness),
			checkMetricValueZero(spec, cur),
			checkObjectMetricTargetNotFound(spec, events),
			checkMissingMetricInStatus(spec, cur, freshness, contract),
		)...)
	}
	summary := "All metrics appear healthy"
	if len(hints) > 0 {
		summary = fmt.Sprintf("%d issue(s) detected across %d metric(s)", len(hints), uniqueMetricCount(hints))
	}
	return &MetricHintsReport{Namespace: hpa.Namespace, Name: hpa.Name, Hints: hints, Summary: summary}
}

func concatHints(slices ...[]MetricHint) []MetricHint {
	var out []MetricHint
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}

func checkExternalMetricMissing(spec autoscalingv2.MetricSpec, events []Event) []MetricHint {
	if spec.Type != autoscalingv2.ExternalMetricSourceType || spec.External == nil {
		return nil
	}
	name := spec.External.Metric.Name
	if !hasEventWithReason(events, "FailedGetExternalMetric", name) {
		return nil
	}
	return []MetricHint{{
		MetricType: "External", MetricName: name, Pattern: "external-metric-missing", Severity: "error",
		Title:       "External metric adapter not serving metric",
		Description: fmt.Sprintf("HPA events report FailedGetExternalMetric for metric %q", name),
		Checks:      []string{"kubectl get --raw /apis/external.metrics.k8s.io/v1beta1", "kubectl get pods -n <adapter-namespace> -l app=prometheus-adapter"},
		CommonCauses: []string{
			"prometheus-adapter relabel rules don't match the metric",
			"metric name mismatch between HPA spec and adapter output",
			"adapter service is unavailable",
		},
		FixSteps: []string{
			"Verify the metric exists in Prometheus: query the metric directly",
			"Check prometheus-adapter relabel configuration",
			"Verify the APIService is registered: kubectl get apiservice v1beta1.external.metrics.k8s.io",
		},
	}}
}

func checkExternalMetricStale(spec autoscalingv2.MetricSpec, freshness []MetricFreshness) []MetricHint {
	if spec.Type != autoscalingv2.ExternalMetricSourceType || spec.External == nil {
		return nil
	}
	name := spec.External.Metric.Name
	entry := findFreshnessEntry(freshness, "External", name)
	if entry == nil || entry.Status != "Stale" {
		return nil
	}
	return []MetricHint{{
		MetricType: "External", MetricName: name, Pattern: "external-metric-stale", Severity: "warning",
		Title:       "External metric data is stale",
		Description: fmt.Sprintf("Metric %q data is stale; HPA may use outdated values for scaling decisions", name),
		CommonCauses: []string{
			"adapter polling interval is too long",
			"upstream metric source (e.g., Prometheus) is slow or unavailable",
			"network connectivity issues between adapter and metric source",
		},
		FixSteps: []string{"Check adapter logs for scrape errors", "Verify upstream metric source health", "Consider reducing adapter --metrics-relist-interval"},
	}}
}

func checkCustomAPIServiceUnavailable(spec autoscalingv2.MetricSpec, freshness []MetricFreshness) []MetricHint {
	if spec.Type != autoscalingv2.PodsMetricSourceType && spec.Type != autoscalingv2.ObjectMetricSourceType {
		return nil
	}
	var name string
	if spec.Type == autoscalingv2.PodsMetricSourceType && spec.Pods != nil {
		name = spec.Pods.Metric.Name
	} else if spec.Type == autoscalingv2.ObjectMetricSourceType && spec.Object != nil {
		name = spec.Object.Metric.Name
	}
	entry := findFreshnessEntry(freshness, string(spec.Type), name)
	if entry == nil || entry.Source != "custom.metrics.k8s.io" || entry.APIServiceAvailable == nil || *entry.APIServiceAvailable {
		return nil
	}
	return []MetricHint{{
		MetricType: string(spec.Type), MetricName: name, Pattern: "custom-api-service-unavailable", Severity: "error",
		Title:        "Custom metrics API service not available",
		Description:  "The custom.metrics.k8s.io APIService is not available; custom metrics cannot be retrieved",
		Checks:       []string{"kubectl get apiservice v1beta1.custom.metrics.k8s.io", "kubectl get pods -n <adapter-namespace>"},
		CommonCauses: []string{"prometheus-adapter not installed", "APIService registration failed", "adapter pods are crashing"},
		FixSteps:     []string{"Install prometheus-adapter if not present", "Check APIService status for error messages", "Verify adapter has RBAC permissions to read metrics"},
	}}
}

func checkExternalAPIServiceUnavailable(spec autoscalingv2.MetricSpec, freshness []MetricFreshness) []MetricHint {
	if spec.Type != autoscalingv2.ExternalMetricSourceType || spec.External == nil {
		return nil
	}
	name := spec.External.Metric.Name
	entry := findFreshnessEntry(freshness, "External", name)
	if entry == nil || entry.APIServiceAvailable == nil || *entry.APIServiceAvailable {
		return nil
	}
	return []MetricHint{{
		MetricType: "External", MetricName: name, Pattern: "external-api-service-unavailable", Severity: "error",
		Title:        "External metrics API service not available",
		Description:  "The external.metrics.k8s.io APIService is not available; external metrics cannot be retrieved",
		CommonCauses: []string{"no adapter installed for external metrics", "APIService registration failed"},
		FixSteps:     []string{"Install a metrics adapter that serves external.metrics.k8s.io (e.g., prometheus-adapter, KEDA)", "Check APIService status"},
	}}
}

func checkMetricValueZero(spec autoscalingv2.MetricSpec, currentMetrics []autoscalingv2.MetricStatus) []MetricHint {
	if spec.Type != autoscalingv2.ExternalMetricSourceType && spec.Type != autoscalingv2.PodsMetricSourceType {
		return nil
	}
	name := specMetricName(spec)
	if name == "" || !hasNonZeroTarget(spec) || !hasZeroCurrentValue(spec, currentMetrics) {
		return nil
	}
	return []MetricHint{{
		MetricType: string(spec.Type), MetricName: name, Pattern: "metric-value-zero", Severity: "warning",
		Title:        "Metric reporting zero values",
		Description:  fmt.Sprintf("Metric %q reports zero current value while target is non-zero", name),
		CommonCauses: []string{"metric export pipeline is not producing data", "application is not instrumented for this metric", "label selector mismatch"},
		FixSteps:     []string{"Verify the application exports the metric", "Check metric source (e.g., Prometheus) for data", "Verify label selectors match"},
	}}
}

func checkObjectMetricTargetNotFound(spec autoscalingv2.MetricSpec, events []Event) []MetricHint {
	if spec.Type != autoscalingv2.ObjectMetricSourceType || spec.Object == nil {
		return nil
	}
	name := spec.Object.Metric.Name
	if !hasEventWithReason(events, "FailedGetObjectMetric", name) {
		return nil
	}
	kind, objName := spec.Object.DescribedObject.Kind, spec.Object.DescribedObject.Name
	return []MetricHint{{
		MetricType: "Object", MetricName: name, Pattern: "object-metric-target-not-found", Severity: "error",
		Title:        "Object metric target may not exist",
		Description:  fmt.Sprintf("HPA events report FailedGetObjectMetric for %s/%s referenced by metric %q", kind, objName, name),
		Checks:       []string{fmt.Sprintf("kubectl get %s %s", strings.ToLower(kind), objName)},
		CommonCauses: []string{"referenced object was deleted", "object kind is incorrect", "cross-namespace reference"},
		FixSteps:     []string{"Verify the referenced object exists", "Check the object kind and name in HPA spec"},
	}}
}

func checkMissingMetricInStatus(
	spec autoscalingv2.MetricSpec,
	currentMetrics []autoscalingv2.MetricStatus,
	freshness []MetricFreshness,
	contract *MetricContractReport,
) []MetricHint {
	_, name := specMetricIdentity(spec)
	if findMatchingCurrentMetric(spec, currentMetrics) {
		return nil
	}
	if entry := findFreshnessEntry(freshness, string(spec.Type), name); entry != nil && entry.Status != "" {
		return nil
	}
	if contractHasMetricIssue(contract, string(spec.Type), name) {
		return nil
	}
	return []MetricHint{{
		MetricType: string(spec.Type), MetricName: name, Pattern: "missing-metric-in-status", Severity: "warning",
		Title:        "Metric has no current data in HPA status",
		Description:  fmt.Sprintf("Metric %q has no matching entry in HPA status current metrics", name),
		CommonCauses: []string{"metrics adapter is not returning data for this metric", "metric name or labels don't match adapter configuration", "adapter hasn't scraped this metric yet"},
		FixSteps:     []string{"Check if the metrics adapter is running", "Verify metric name matches adapter configuration", "Wait a few reconciliation cycles and check again"},
	}}
}

// hasEventWithReason checks whether events contain an event with the given
// reason that mentions the metric name in its message.
func hasEventWithReason(events []Event, reason, metricName string) bool {
	lower := strings.ToLower(metricName)
	for _, e := range events {
		if e.Reason == reason && strings.Contains(strings.ToLower(e.Message), lower) {
			return true
		}
	}
	return false
}

func findFreshnessEntry(entries []MetricFreshness, metricType, metricName string) *MetricFreshness {
	for i := range entries {
		if entries[i].Type == metricType && entries[i].Name == metricName {
			return &entries[i]
		}
	}
	return nil
}

func specMetricName(spec autoscalingv2.MetricSpec) string {
	switch spec.Type {
	case autoscalingv2.ExternalMetricSourceType:
		if spec.External != nil {
			return spec.External.Metric.Name
		}
	case autoscalingv2.PodsMetricSourceType:
		if spec.Pods != nil {
			return spec.Pods.Metric.Name
		}
	}
	return ""
}

func hasNonZeroTarget(spec autoscalingv2.MetricSpec) bool {
	var target autoscalingv2.MetricTarget
	switch spec.Type {
	case autoscalingv2.ExternalMetricSourceType:
		if spec.External != nil {
			target = spec.External.Target
		}
	case autoscalingv2.PodsMetricSourceType:
		if spec.Pods != nil {
			target = spec.Pods.Target
		}
	default:
		return false
	}
	return (target.AverageUtilization != nil && *target.AverageUtilization > 0) ||
		(target.AverageValue != nil && !target.AverageValue.IsZero()) ||
		(target.Value != nil && !target.Value.IsZero())
}

func hasZeroCurrentValue(spec autoscalingv2.MetricSpec, currentMetrics []autoscalingv2.MetricStatus) bool {
	for _, cur := range currentMetrics {
		if spec.Type == cur.Type && handlerFor(spec.Type).MatchesCurrent(spec, cur) {
			return isCurrentValueZero(cur)
		}
	}
	return false
}

func contractHasMetricIssue(contract *MetricContractReport, metricType, metricName string) bool {
	if contract == nil {
		return false
	}
	for _, check := range contract.Checks {
		if check.MetricType == metricType && check.MetricName == metricName && check.Status != "ok" {
			return true
		}
	}
	return false
}

func uniqueMetricCount(hints []MetricHint) int {
	seen := make(map[string]struct{})
	for _, h := range hints {
		seen[h.MetricType+"/"+h.MetricName] = struct{}{}
	}
	return len(seen)
}
