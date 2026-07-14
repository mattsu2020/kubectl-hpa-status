// Package hpa provides metric type dispatch via a handler registry.
// Each MetricSourceType (Resource, ContainerResource, Pods, Object, External)
// has a handler that implements type-specific formatting, matching, and
// remediation. Adding a new metric type requires only adding a handler to
// the registry — no changes to the callers.
package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
)

// MetricHandler encapsulates type-specific logic for one HPA metric source type.
// All public functions that previously switched on MetricSourceType delegate
// to the handler registry instead.
type MetricHandler interface {
	// FormatStatus converts a MetricStatus into a formatted Metric struct.
	FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, status autoscalingv2.MetricStatus) Metric
	// ImpactRatio returns the display name and ratio for impact estimation.
	ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, status autoscalingv2.MetricStatus) (string, *float64)
	// SpecIdentity returns (typeLabel, displayName) for a spec metric.
	SpecIdentity(spec autoscalingv2.MetricSpec) (string, string)
	// MatchesCurrent checks whether a spec metric has a matching current metric.
	MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool
	// Remediation returns a human-readable fix string when this metric is missing.
	Remediation(spec autoscalingv2.MetricSpec) string
	// DisplayName returns the short name for a metric status entry.
	DisplayName(status autoscalingv2.MetricStatus) string
}

// handlerFor returns the MetricHandler for the given source type, or a
// fallback handler for unknown types.
func handlerFor(t autoscalingv2.MetricSourceType) MetricHandler {
	if h, ok := metricHandlers[t]; ok {
		return h
	}
	return fallbackHandler{}
}

var metricHandlers = map[autoscalingv2.MetricSourceType]MetricHandler{
	autoscalingv2.ResourceMetricSourceType:          resourceHandler{},
	autoscalingv2.ContainerResourceMetricSourceType: containerResourceHandler{},
	autoscalingv2.PodsMetricSourceType:              podsHandler{},
	autoscalingv2.ObjectMetricSourceType:            objectHandler{},
	autoscalingv2.ExternalMetricSourceType:          externalHandler{},
}

// --- Fallback ---

type fallbackHandler struct{}

func (fallbackHandler) FormatStatus(_ *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	return Metric{
		Type: string(metric.Type),
		Text: fmt.Sprintf("%s metric is present, but this plugin does not know how to format it in detail", metric.Type),
	}
}

func (fallbackHandler) ImpactRatio(_ *autoscalingv2.HorizontalPodAutoscaler, _ autoscalingv2.MetricStatus) (string, *float64) {
	return "", nil
}

func (fallbackHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	return string(spec.Type), "<unknown>"
}

func (fallbackHandler) MatchesCurrent(_ autoscalingv2.MetricSpec, _ autoscalingv2.MetricStatus) bool {
	return false
}

func (fallbackHandler) Remediation(_ autoscalingv2.MetricSpec) string {
	return "Verify the metrics adapter for this metric type is installed and functioning correctly."
}

func (fallbackHandler) DisplayName(metric autoscalingv2.MetricStatus) string {
	return string(metric.Type)
}

// --- Dispatch wrappers (public API preserved) ---

// FormatMetricStatus formats a metric status entry into a Metric struct.
func FormatMetricStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.Type == "" {
		return Metric{Text: "Metric status is present, but details are unavailable"}
	}
	return handlerFor(metric.Type).FormatStatus(hpa, metric)
}

// metricImpactRatio returns the metric display name and ratio for any metric type.
func metricImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	return handlerFor(metric.Type).ImpactRatio(hpa, metric)
}

// specMetricIdentity returns the type and name of a spec metric for display.
func specMetricIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	return handlerFor(spec.Type).SpecIdentity(spec)
}

// findMatchingCurrentMetric checks whether a spec metric has a matching entry
// in the current metrics status.
func findMatchingCurrentMetric(spec autoscalingv2.MetricSpec, currentMetrics []autoscalingv2.MetricStatus) bool {
	for _, current := range currentMetrics {
		if spec.Type == current.Type && handlerFor(spec.Type).MatchesCurrent(spec, current) {
			return true
		}
	}
	return false
}

// buildMetricRemediation returns a remediation string for a missing spec metric.
func buildMetricRemediation(spec autoscalingv2.MetricSpec) string {
	if r := handlerFor(spec.Type).Remediation(spec); r != "" {
		return r
	}
	return "Verify the metrics adapter for this metric type is installed and functioning correctly."
}

// metricDisplayName returns a human-readable name for a metric status entry.
func metricDisplayName(metric autoscalingv2.MetricStatus) string {
	return handlerFor(metric.Type).DisplayName(metric)
}

// currentMetricValueStatus returns the value payload for any supported metric
// status. Keeping this type switch next to the handler registry prevents
// decision tracing and freshness analysis from maintaining their own copies.
func currentMetricValueStatus(metric autoscalingv2.MetricStatus) (autoscalingv2.MetricValueStatus, bool) {
	switch metric.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource != nil {
			return metric.Resource.Current, true
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if metric.ContainerResource != nil {
			return metric.ContainerResource.Current, true
		}
	case autoscalingv2.PodsMetricSourceType:
		if metric.Pods != nil {
			return metric.Pods.Current, true
		}
	case autoscalingv2.ObjectMetricSourceType:
		if metric.Object != nil {
			return metric.Object.Current, true
		}
	case autoscalingv2.ExternalMetricSourceType:
		if metric.External != nil {
			return metric.External.Current, true
		}
	}
	return autoscalingv2.MetricValueStatus{}, false
}

func matchingMetricTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, current autoscalingv2.MetricStatus) (*autoscalingv2.MetricTarget, bool) {
	for i := range hpa.Spec.Metrics {
		spec := &hpa.Spec.Metrics[i]
		if spec.Type == current.Type && handlerFor(spec.Type).MatchesCurrent(*spec, current) {
			if target := metricTargetPointer(spec); target != nil {
				return target, true
			}
		}
	}
	return nil, false
}

// specMetricSelector returns the formatted selector string for a spec metric,
// or empty string if the metric type does not support selectors.
func specMetricSelector(spec autoscalingv2.MetricSpec) string {
	switch {
	case spec.External != nil:
		return FormatMetricSelector(spec.External.Metric.Selector)
	case spec.Object != nil:
		return FormatMetricSelector(spec.Object.Metric.Selector)
	case spec.Pods != nil:
		return FormatMetricSelector(spec.Pods.Metric.Selector)
	default:
		return ""
	}
}

// selectorsEqual compares two LabelSelectors for equality.
// Both nil selectors are considered equal. Non-nil selectors are compared
// by formatting them into stable string representations.
func selectorsEqual(a, b *metav1.LabelSelector) bool {
	return FormatMetricSelector(a) == FormatMetricSelector(b)
}

// appendRatioAndNote appends the standard " ratio=%.3f" and " note=%q" suffixes
// to a metric text line when ratio and note carry data. All five FormatStatus
// handlers render this identical tail, so centralising it keeps the output
// format consistent and removes copy-paste drift.
func appendRatioAndNote(text string, ratio *float64, note string) string {
	if ratio != nil {
		text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
	}
	if note != "" {
		text = fmt.Sprintf("%s note=%q", text, note)
	}
	return text
}

// --- Helper functions for metric value formatting ---

// FormatMetricTarget returns a human-readable string for a metric target.
func FormatMetricTarget(target autoscalingv2.MetricTarget) string {
	switch target.Type {
	case autoscalingv2.UtilizationMetricType:
		if target.AverageUtilization != nil {
			return fmt.Sprintf("%d%%", *target.AverageUtilization)
		}
	case autoscalingv2.AverageValueMetricType:
		if target.AverageValue != nil {
			return target.AverageValue.String()
		}
	case autoscalingv2.ValueMetricType:
		if target.Value != nil {
			return target.Value.String()
		}
	}
	return "<unknown>"
}

// FormatMetricSelector returns a stable selector string for custom/external
// metrics. Empty selectors are omitted from text output.
func FormatMetricSelector(selector *metav1.LabelSelector) string {
	if selector == nil {
		return ""
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return klabels.FormatLabels(selector.MatchLabels)
	}
	if parsed.Empty() {
		return ""
	}
	return parsed.String()
}

// FormatMetricValue returns a formatted string for utilization or average value.
func FormatMetricValue(utilization *int32, averageValue *resource.Quantity) string {
	if utilization != nil {
		return fmt.Sprintf("%d%%", *utilization)
	}
	if averageValue != nil && !averageValue.IsZero() {
		return averageValue.String()
	}
	return "<unknown>"
}

// FormatMetricValueStatus returns a formatted string for a metric value status.
func FormatMetricValueStatus(value autoscalingv2.MetricValueStatus) string {
	if value.AverageUtilization != nil {
		return fmt.Sprintf("%d%%", *value.AverageUtilization)
	}
	if value.AverageValue != nil && !value.AverageValue.IsZero() {
		return value.AverageValue.String()
	}
	if value.Value != nil && !value.Value.IsZero() {
		return value.Value.String()
	}
	return "<unknown>"
}

// FindResourceTargetSpec finds the MetricTarget for a resource metric by name.
func FindResourceTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ResourceMetricSourceType && m.Resource != nil && string(m.Resource.Name) == name {
			return m.Resource.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindResourceTarget returns the formatted target string for a resource metric.
func FindResourceTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindResourceTargetSpec(hpa, name))
}

// FindContainerResourceTargetSpec finds the MetricTarget for a container resource metric.
func FindContainerResourceTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name, container string) autoscalingv2.MetricTarget {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ContainerResourceMetricSourceType && m.ContainerResource != nil &&
			string(m.ContainerResource.Name) == name && m.ContainerResource.Container == container {
			return m.ContainerResource.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindContainerResourceTarget returns the formatted target string for a container resource metric.
func FindContainerResourceTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name, container string) string {
	return FormatMetricTarget(FindContainerResourceTargetSpec(hpa, name, container))
}

// FindPodsTargetSpec finds the MetricTarget for a Pods metric by name and selector.
func FindPodsTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string, selector *metav1.LabelSelector) autoscalingv2.MetricTarget {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.PodsMetricSourceType && m.Pods != nil && m.Pods.Metric.Name == name && selectorsEqual(m.Pods.Metric.Selector, selector) {
			return m.Pods.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindPodsTarget returns the formatted target string for a Pods metric.
func FindPodsTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string, selector *metav1.LabelSelector) string {
	return FormatMetricTarget(FindPodsTargetSpec(hpa, name, selector))
}

// FindObjectTargetSpec finds the MetricTarget for an Object metric by name, selector, and described object.
func FindObjectTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string, selector *metav1.LabelSelector, describedObject autoscalingv2.CrossVersionObjectReference) autoscalingv2.MetricTarget {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ObjectMetricSourceType && m.Object != nil && m.Object.Metric.Name == name && selectorsEqual(m.Object.Metric.Selector, selector) && m.Object.DescribedObject.Kind == describedObject.Kind && m.Object.DescribedObject.Name == describedObject.Name {
			return m.Object.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindObjectTarget returns the formatted target string for an Object metric.
func FindObjectTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string, selector *metav1.LabelSelector, describedObject autoscalingv2.CrossVersionObjectReference) string {
	return FormatMetricTarget(FindObjectTargetSpec(hpa, name, selector, describedObject))
}

// FindExternalTargetSpec finds the MetricTarget for an External metric by name and selector.
func FindExternalTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string, selector *metav1.LabelSelector) autoscalingv2.MetricTarget {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ExternalMetricSourceType && m.External != nil && m.External.Metric.Name == name && selectorsEqual(m.External.Metric.Selector, selector) {
			return m.External.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindExternalTarget returns the formatted target string for an External metric.
func FindExternalTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string, selector *metav1.LabelSelector) string {
	return FormatMetricTarget(FindExternalTargetSpec(hpa, name, selector))
}

// --- Metric lookup helpers ---

// hasCurrentExternalMetric reports whether the HPA status contains an
// External metric matching the given spec metric's name and selector.
func hasCurrentExternalMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string, selector *metav1.LabelSelector) bool {
	_, ok := currentExternalMetric(hpa, name, selector)
	return ok
}

// currentExternalMetric returns the External metric status matching the given
// name and selector.
func currentExternalMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string, selector *metav1.LabelSelector) (autoscalingv2.MetricStatus, bool) {
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type == autoscalingv2.ExternalMetricSourceType &&
			metric.External != nil &&
			metric.External.Metric.Name == name &&
			selectorsEqual(metric.External.Metric.Selector, selector) {
			return metric, true
		}
	}
	return autoscalingv2.MetricStatus{}, false
}

// currentObjectMetric returns the Object metric status matching the given
// name, selector, and described object.
func currentObjectMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string, selector *metav1.LabelSelector, describedObject autoscalingv2.CrossVersionObjectReference) (autoscalingv2.MetricStatus, bool) {
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type == autoscalingv2.ObjectMetricSourceType &&
			metric.Object != nil &&
			metric.Object.Metric.Name == name &&
			selectorsEqual(metric.Object.Metric.Selector, selector) &&
			metric.Object.DescribedObject.Kind == describedObject.Kind &&
			metric.Object.DescribedObject.Name == describedObject.Name {
			return metric, true
		}
	}
	return autoscalingv2.MetricStatus{}, false
}
