package hpa

import (
	"fmt"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// AnalyzeMetricFreshness analyzes each HPA spec metric for data freshness,
// identifying whether the metric is OK, Stale, Missing, or Unknown. It returns
// per-metric freshness entries with source API, evidence from conditions and
// events, and remediation next steps.
func AnalyzeMetricFreshness(hpa *autoscalingv2.HorizontalPodAutoscaler, events []Event) []MetricFreshness {
	if hpa == nil {
		return nil
	}

	specMetrics := hpa.Spec.Metrics
	if len(specMetrics) == 0 {
		return nil
	}

	currentMetrics := hpa.Status.CurrentMetrics
	scalingActive := IsScalingActive(hpa)

	var entries []MetricFreshness
	for _, spec := range specMetrics {
		entries = append(entries, analyzeSingleMetricFreshness(spec, currentMetrics, scalingActive, events, hpa))
	}

	return entries
}

// analyzeSingleMetricFreshness evaluates a single spec metric's freshness.
func analyzeSingleMetricFreshness(
	spec autoscalingv2.MetricSpec,
	currentMetrics []autoscalingv2.MetricStatus,
	scalingActive bool,
	events []Event,
	hpa *autoscalingv2.HorizontalPodAutoscaler,
) MetricFreshness {
	metricType, metricName := specMetricIdentity(spec)
	source := metricSourceAPI(spec.Type)
	window := metricWindow(spec.Type)

	entry := MetricFreshness{
		Name:   metricName,
		Type:   metricType,
		Source: source,
		Window: window,
	}

	found := findMatchingCurrentMetric(spec, currentMetrics)

	switch {
	case !found && !scalingActive:
		entry.Status = string(FreshnessMissing)
		entry.Risk = "HPA cannot compute scaling decisions; metric data is unavailable"
		entry.Evidence = collectConditionEvidence(hpa, metricType, metricName)
		entry.Evidence = append(entry.Evidence, scanEventsForEvidence(events, metricName, metricType)...)
		entry.NextSteps = buildFreshnessNextSteps(FreshnessMissing, spec)

	case !found:
		entry.Status = string(FreshnessMissing)
		entry.Risk = "HPA may use conservative assumptions for this metric"
		entry.Evidence = scanEventsForEvidence(events, metricName, metricType)
		entry.NextSteps = buildFreshnessNextSteps(FreshnessMissing, spec)

	case isMetricValueZero(spec, currentMetrics):
		entry.Status = string(FreshnessStale)
		entry.Risk = "Metric value is zero; HPA may use conservative assumptions"
		entry.Evidence = []string{fmt.Sprintf("%s metric %q reports zero or nil current value", metricType, metricName)}
		entry.NextSteps = buildFreshnessNextSteps(FreshnessStale, spec)

	default:
		entry.Status = string(FreshnessOK)
	}

	return entry
}

// IsScalingActive checks whether the HPA ScalingActive condition is True.
func IsScalingActive(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, c := range hpa.Status.Conditions {
		if c.Type == autoscalingv2.ScalingActive && c.Status == "True" {
			return true
		}
	}
	return false
}

// metricSourceAPI maps a metric source type to the Kubernetes API group that
// serves it.
func metricSourceAPI(t autoscalingv2.MetricSourceType) string {
	switch t {
	case autoscalingv2.ResourceMetricSourceType, autoscalingv2.ContainerResourceMetricSourceType:
		return "metrics.k8s.io"
	case autoscalingv2.PodsMetricSourceType, autoscalingv2.ObjectMetricSourceType:
		return "custom.metrics.k8s.io"
	case autoscalingv2.ExternalMetricSourceType:
		return "external.metrics.k8s.io"
	default:
		return ""
	}
}

// metricWindow returns the expected collection window for a metric type.
// Resource metrics use a 30s default scrape interval from metrics-server;
// other types vary by adapter and return empty.
func metricWindow(t autoscalingv2.MetricSourceType) string {
	switch t {
	case autoscalingv2.ResourceMetricSourceType, autoscalingv2.ContainerResourceMetricSourceType:
		return "30s"
	default:
		return ""
	}
}

// scanEventsForEvidence scans Kubernetes events for warning signals related to
// the given metric. It looks for common HPA event reasons that indicate metric
// retrieval failures.
func scanEventsForEvidence(events []Event, metricName, metricType string) []string {
	if len(events) == 0 {
		return nil
	}

	var evidence []string
	metricLower := strings.ToLower(metricName)

	for _, event := range events {
		reason := event.Reason
		msg := event.Message
		if !isFailedMetricEvent(reason) {
			continue
		}
		if !eventMatchesMetricType(reason, metricType) {
			continue
		}
		// Check if the event message mentions this metric.
		msgLower := strings.ToLower(msg)
		if strings.Contains(msgLower, metricLower) || metricName == "" {
			evidence = append(evidence, fmt.Sprintf("%s event detected: %s", reason, truncateMessage(msg, 120)))
		}
	}

	return evidence
}

// isFailedMetricEvent returns true if the event reason indicates a metric
// retrieval failure.
func isFailedMetricEvent(reason string) bool {
	return strings.HasPrefix(reason, "FailedGet") || strings.HasPrefix(reason, "FailedMetric")
}

// eventMatchesMetricType checks whether a FailedGet/FailedMetric event reason
// is relevant for the given metric type.
func eventMatchesMetricType(reason, metricType string) bool {
	reasonLower := strings.ToLower(reason)
	switch metricType {
	case MetricTypeResource, "ContainerResource":
		return strings.Contains(reasonLower, "resource")
	case MetricTypeExternal:
		return strings.Contains(reasonLower, "external")
	case MetricTypePods:
		return strings.Contains(reasonLower, "pods") || strings.Contains(reasonLower, "pod")
	case MetricTypeObject:
		return strings.Contains(reasonLower, "object")
	default:
		return true
	}
}

// collectConditionEvidence extracts evidence from HPA conditions that indicate
// metric retrieval issues.
func collectConditionEvidence(hpa *autoscalingv2.HorizontalPodAutoscaler, _, _ string) []string {
	var evidence []string
	for _, c := range hpa.Status.Conditions {
		if c.Type == autoscalingv2.ScalingActive && c.Status != "True" {
			evidence = append(evidence, fmt.Sprintf("ScalingActive=%s reason=%s", c.Status, c.Reason))
		}
		if c.Type == autoscalingv2.AbleToScale && c.Status != "True" {
			evidence = append(evidence, fmt.Sprintf("AbleToScale=%s reason=%s", c.Status, c.Reason))
		}
	}
	return evidence
}

// buildFreshnessNextSteps generates remediation commands based on the freshness
// status and metric type.
func buildFreshnessNextSteps(status MetricFreshnessStatus, spec autoscalingv2.MetricSpec) []string {
	switch status {
	case FreshnessMissing:
		return buildMissingNextSteps(spec)
	case FreshnessStale:
		return buildStaleNextSteps(spec)
	default:
		return nil
	}
}

// buildMissingNextSteps returns remediation commands for a missing metric.
func buildMissingNextSteps(spec autoscalingv2.MetricSpec) []string {
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType, autoscalingv2.ContainerResourceMetricSourceType:
		return []string{
			"kubectl get apiservice v1beta1.metrics.k8s.io",
			"kubectl get deploy -n kube-system -l k8s-app=metrics-server",
			"kubectl logs -n kube-system -l k8s-app=metrics-server",
		}
	case autoscalingv2.ExternalMetricSourceType:
		return []string{
			"kubectl get apiservice | grep external.metrics",
			"kubectl describe hpa <name>",
			"kubectl logs -n <adapter-namespace> -l app=<adapter-name>",
		}
	case autoscalingv2.PodsMetricSourceType, autoscalingv2.ObjectMetricSourceType:
		return []string{
			"kubectl get apiservice | grep custom.metrics",
			"kubectl describe hpa <name>",
			"kubectl logs -n <adapter-namespace> -l app=<adapter-name>",
		}
	default:
		return []string{
			"kubectl describe hpa <name>",
		}
	}
}

// buildStaleNextSteps returns remediation commands for a stale metric.
func buildStaleNextSteps(spec autoscalingv2.MetricSpec) []string {
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType, autoscalingv2.ContainerResourceMetricSourceType:
		return []string{
			"kubectl logs -n kube-system -l k8s-app=metrics-server",
			"kubectl get --raw /apis/metrics.k8s.io/v1beta1",
		}
	default:
		return []string{
			"kubectl logs -n <adapter-namespace> -l app=<adapter-name>",
			"kubectl get --raw /apis/custom.metrics.k8s.io/v1beta1",
		}
	}
}

// isMetricValueZero checks whether a matching current metric has a zero or nil
// value, which may indicate staleness.
func isMetricValueZero(spec autoscalingv2.MetricSpec, currentMetrics []autoscalingv2.MetricStatus) bool {
	for _, current := range currentMetrics {
		if spec.Type != current.Type {
			continue
		}
		if !handlerFor(spec.Type).MatchesCurrent(spec, current) {
			continue
		}
		return isCurrentValueZero(current)
	}
	return false
}

// isCurrentValueZero checks if a metric status has a zero or nil value.
func isCurrentValueZero(status autoscalingv2.MetricStatus) bool {
	value, ok := currentMetricValueStatus(status)
	if !ok {
		return true
	}
	return isMetricValueStatusZero(value)
}

// isMetricValueStatusZero returns true if all value fields in a MetricValueStatus
// are nil or zero.
func isMetricValueStatusZero(v autoscalingv2.MetricValueStatus) bool {
	if v.AverageUtilization != nil && *v.AverageUtilization != 0 {
		return false
	}
	if v.AverageValue != nil && !v.AverageValue.IsZero() {
		return false
	}
	if v.Value != nil && !v.Value.IsZero() {
		return false
	}
	return true
}

// truncateMessage truncates a message to maxLen characters, appending "..." if
// truncated.
func truncateMessage(msg string, maxLen int) string {
	return rendutil.TruncateDisplayWidth(msg, maxLen, "...")
}
