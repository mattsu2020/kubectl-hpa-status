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
	autoscalingv2.ResourceMetricSourceType:         resourceHandler{},
	autoscalingv2.ContainerResourceMetricSourceType: containerResourceHandler{},
	autoscalingv2.PodsMetricSourceType:              podsHandler{},
	autoscalingv2.ObjectMetricSourceType:            objectHandler{},
	autoscalingv2.ExternalMetricSourceType:          externalHandler{},
}

// --- Resource ---

type resourceHandler struct{}

func (resourceHandler) FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.Resource == nil {
		return Metric{Type: "Resource", Text: "Resource metric: <missing status>"}
	}
	targetSpec := FindResourceTargetSpec(hpa, string(metric.Resource.Name))
	target := FormatMetricTarget(targetSpec)
	current := FormatMetricValue(metric.Resource.Current.AverageUtilization, metric.Resource.Current.AverageValue)
	ratio, note := calculateRatioAndNote(metric.Resource.Current, targetSpec, target)
	text := fmt.Sprintf("Resource %s current=%s target=%s", metric.Resource.Name, current, target)
	if ratio != nil {
		text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
	}
	if note != "" {
		text = fmt.Sprintf("%s note=%q", text, note)
	}
	return Metric{
		Type: "Resource", Name: string(metric.Resource.Name),
		Current: current, Target: target, Ratio: ratio, Note: note, Text: text,
	}
}

func (resourceHandler) ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	if metric.Resource == nil {
		return "", nil
	}
	ratio := utilizationRatio(metric.Resource.Current.AverageUtilization, FindResourceTarget(hpa, string(metric.Resource.Name)))
	return string(metric.Resource.Name), ratio
}

func (resourceHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	if spec.Resource != nil {
		return "Resource", string(spec.Resource.Name)
	}
	return "Resource", "<unknown>"
}

func (resourceHandler) MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	return spec.Resource != nil && current.Resource != nil && spec.Resource.Name == current.Resource.Name
}

func (resourceHandler) Remediation(spec autoscalingv2.MetricSpec) string {
	if spec.Resource == nil {
		return ""
	}
	return fmt.Sprintf(
		"Resource metric %q is missing. Verify that the metrics-server is running and can scrape kubelet metrics: kubectl top pods -n <namespace>. "+
			"Check metrics-server deployment: kubectl get deploy -n kube-system -l k8s-app=metrics-server. "+
			"Verify the API service: kubectl get apiservice v1beta1.metrics.k8s.io.",
		spec.Resource.Name,
	)
}

func (resourceHandler) DisplayName(status autoscalingv2.MetricStatus) string {
	if status.Resource != nil {
		return string(status.Resource.Name)
	}
	return "Resource"
}

// --- ContainerResource ---

type containerResourceHandler struct{}

func (containerResourceHandler) FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.ContainerResource == nil {
		return Metric{Type: "ContainerResource", Text: "ContainerResource metric: <missing status>"}
	}
	targetSpec := FindContainerResourceTargetSpec(hpa, string(metric.ContainerResource.Name), metric.ContainerResource.Container)
	target := FormatMetricTarget(targetSpec)
	current := FormatMetricValueStatus(metric.ContainerResource.Current)
	ratio, note := calculateRatioAndNote(metric.ContainerResource.Current, targetSpec, target)
	text := fmt.Sprintf("ContainerResource %s/%s current=%s target=%s", metric.ContainerResource.Container, metric.ContainerResource.Name, current, target)
	if ratio != nil {
		text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
	}
	if note != "" {
		text = fmt.Sprintf("%s note=%q", text, note)
	}
	return Metric{
		Type: "ContainerResource", Name: fmt.Sprintf("%s/%s", metric.ContainerResource.Container, metric.ContainerResource.Name),
		Current: current, Target: target, Ratio: ratio, Note: note, Text: text,
	}
}

func (containerResourceHandler) ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	if metric.ContainerResource == nil {
		return "", nil
	}
	targetSpec := FindContainerResourceTargetSpec(hpa, string(metric.ContainerResource.Name), metric.ContainerResource.Container)
	target := FormatMetricTarget(targetSpec)
	ratio := utilizationRatio(metric.ContainerResource.Current.AverageUtilization, target)
	name := fmt.Sprintf("%s/%s", metric.ContainerResource.Container, metric.ContainerResource.Name)
	if ratio != nil {
		return name, ratio
	}
	if metric.ContainerResource.Current.AverageValue != nil && targetSpec.AverageValue != nil {
		ratio = quantityRatio(metric.ContainerResource.Current.AverageValue, targetSpec.AverageValue)
		return name, ratio
	}
	return name, nil
}

func (containerResourceHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	if spec.ContainerResource != nil {
		return "ContainerResource", fmt.Sprintf("%s/%s", spec.ContainerResource.Container, spec.ContainerResource.Name)
	}
	return "ContainerResource", "<unknown>"
}

func (containerResourceHandler) MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	return spec.ContainerResource != nil && current.ContainerResource != nil &&
		spec.ContainerResource.Name == current.ContainerResource.Name &&
		spec.ContainerResource.Container == current.ContainerResource.Container
}

func (containerResourceHandler) Remediation(spec autoscalingv2.MetricSpec) string {
	if spec.ContainerResource == nil {
		return ""
	}
	return fmt.Sprintf(
		"ContainerResource metric %s/%s is missing. Verify the metrics-server is running and the container is reporting resource usage.",
		spec.ContainerResource.Container, spec.ContainerResource.Name,
	)
}

func (containerResourceHandler) DisplayName(status autoscalingv2.MetricStatus) string {
	if status.ContainerResource != nil {
		return fmt.Sprintf("%s/%s", status.ContainerResource.Container, status.ContainerResource.Name)
	}
	return "ContainerResource"
}

// --- Pods ---

type podsHandler struct{}

func (podsHandler) FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.Pods == nil {
		return Metric{Type: "Pods", Text: "Pods metric: <missing status>"}
	}
	targetSpec := FindPodsTargetSpec(hpa, metric.Pods.Metric.Name)
	target := FormatMetricTarget(targetSpec)
	current := FormatMetricValueStatus(metric.Pods.Current)
	ratio, note := calculateRatioAndNote(metric.Pods.Current, targetSpec, target)
	selector := FormatMetricSelector(metric.Pods.Metric.Selector)
	text := fmt.Sprintf("Pods %s current=%s target=%s", metric.Pods.Metric.Name, current, target)
	if selector != "" {
		text = fmt.Sprintf("%s selector=%q", text, selector)
	}
	if ratio != nil {
		text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
	}
	if note != "" {
		text = fmt.Sprintf("%s note=%q", text, note)
	}
	return Metric{
		Type: "Pods", Name: metric.Pods.Metric.Name, Selector: selector,
		Current: current, Target: target, Ratio: ratio, Note: note, Text: text,
	}
}

func (podsHandler) ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	if metric.Pods == nil {
		return "", nil
	}
	targetSpec := FindPodsTargetSpec(hpa, metric.Pods.Metric.Name)
	ratio, _ := calculateRatioAndNote(metric.Pods.Current, targetSpec, FormatMetricTarget(targetSpec))
	return metric.Pods.Metric.Name, ratio
}

func (podsHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	if spec.Pods != nil {
		return "Pods", spec.Pods.Metric.Name
	}
	return "Pods", "<unknown>"
}

func (podsHandler) MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	return spec.Pods != nil && current.Pods != nil && spec.Pods.Metric.Name == current.Pods.Metric.Name
}

func (podsHandler) Remediation(spec autoscalingv2.MetricSpec) string {
	if spec.Pods == nil {
		return ""
	}
	return fmt.Sprintf(
		"Pods metric %q is missing. Verify the custom metrics adapter is serving this metric and check that pods are exposing the expected metric values.",
		spec.Pods.Metric.Name,
	)
}

func (podsHandler) DisplayName(status autoscalingv2.MetricStatus) string {
	if status.Pods != nil {
		return status.Pods.Metric.Name
	}
	return "Pods"
}

// --- Object ---

type objectHandler struct{}

func (objectHandler) FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.Object == nil {
		return Metric{Type: "Object", Text: "Object metric: <missing status>"}
	}
	targetSpec := FindObjectTargetSpec(hpa, metric.Object.Metric.Name)
	target := FormatMetricTarget(targetSpec)
	current := FormatMetricValueStatus(metric.Object.Current)
	ratio, note := calculateRatioAndNote(metric.Object.Current, targetSpec, target)
	name := fmt.Sprintf("%s/%s", metric.Object.DescribedObject.Kind, metric.Object.DescribedObject.Name)
	selector := FormatMetricSelector(metric.Object.Metric.Selector)
	text := fmt.Sprintf("Object %s %s current=%s target=%s", name, metric.Object.Metric.Name, current, target)
	if selector != "" {
		text = fmt.Sprintf("%s selector=%q", text, selector)
	}
	if ratio != nil {
		text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
	}
	if note != "" {
		text = fmt.Sprintf("%s note=%q", text, note)
	}
	return Metric{
		Type: "Object", Name: metric.Object.Metric.Name, Selector: selector,
		Object: name, Current: current, Target: target, Ratio: ratio, Note: note, Text: text,
	}
}

func (objectHandler) ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	if metric.Object == nil {
		return "", nil
	}
	targetSpec := FindObjectTargetSpec(hpa, metric.Object.Metric.Name)
	ratio, _ := calculateRatioAndNote(metric.Object.Current, targetSpec, FormatMetricTarget(targetSpec))
	return metric.Object.Metric.Name, ratio
}

func (objectHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	if spec.Object != nil {
		return "Object", spec.Object.Metric.Name
	}
	return "Object", "<unknown>"
}

func (objectHandler) MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	return spec.Object != nil && current.Object != nil && spec.Object.Metric.Name == current.Object.Metric.Name
}

func (objectHandler) Remediation(spec autoscalingv2.MetricSpec) string {
	if spec.Object == nil {
		return ""
	}
	return fmt.Sprintf(
		"Object metric %q is missing. Verify the described object %s/%s exists and the metrics adapter can retrieve its values.",
		spec.Object.Metric.Name, spec.Object.DescribedObject.Kind, spec.Object.DescribedObject.Name,
	)
}

func (objectHandler) DisplayName(status autoscalingv2.MetricStatus) string {
	if status.Object != nil {
		return status.Object.Metric.Name
	}
	return "Object"
}

// --- External ---

type externalHandler struct{}

func (externalHandler) FormatStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	if metric.External == nil {
		return Metric{Type: "External", Text: "External metric: <missing status>"}
	}
	targetSpec := FindExternalTargetSpec(hpa, metric.External.Metric.Name)
	target := FormatMetricTarget(targetSpec)
	current := FormatMetricValueStatus(metric.External.Current)
	ratio, note := calculateRatioAndNote(metric.External.Current, targetSpec, target)
	selector := FormatMetricSelector(metric.External.Metric.Selector)
	text := fmt.Sprintf("External %s current=%s target=%s", metric.External.Metric.Name, current, target)
	if selector != "" {
		text = fmt.Sprintf("%s selector=%q", text, selector)
	}
	if ratio != nil {
		text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
	}
	if note != "" {
		text = fmt.Sprintf("%s note=%q", text, note)
	}
	return Metric{
		Type: "External", Name: metric.External.Metric.Name, Selector: selector,
		Current: current, Target: target, Ratio: ratio, Note: note, Text: text,
	}
}

func (externalHandler) ImpactRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64) {
	if metric.External == nil {
		return "", nil
	}
	targetSpec := FindExternalTargetSpec(hpa, metric.External.Metric.Name)
	ratio, _ := calculateRatioAndNote(metric.External.Current, targetSpec, FormatMetricTarget(targetSpec))
	return metric.External.Metric.Name, ratio
}

func (externalHandler) SpecIdentity(spec autoscalingv2.MetricSpec) (string, string) {
	if spec.External != nil {
		return "External", spec.External.Metric.Name
	}
	return "External", "<unknown>"
}

func (externalHandler) MatchesCurrent(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	return spec.External != nil && current.External != nil && spec.External.Metric.Name == current.External.Metric.Name
}

func (externalHandler) Remediation(spec autoscalingv2.MetricSpec) string {
	if spec.External == nil {
		return ""
	}
	return fmt.Sprintf(
		"External metric %q is missing. Verify the external metrics adapter is serving the metric and check adapter logs for errors. "+
			"Check the API service: kubectl get --raw /apis/external.metrics.k8s.io/v1beta1. "+
			"If using Prometheus Adapter, check the API service and rules ConfigMap.",
		spec.External.Metric.Name,
	)
}

func (externalHandler) DisplayName(status autoscalingv2.MetricStatus) string {
	if status.External != nil {
		return status.External.Metric.Name
	}
	return "External"
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

// FindPodsTargetSpec finds the MetricTarget for a Pods metric by name.
func FindPodsTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.PodsMetricSourceType && m.Pods != nil && m.Pods.Metric.Name == name {
			return m.Pods.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindPodsTarget returns the formatted target string for a Pods metric.
func FindPodsTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindPodsTargetSpec(hpa, name))
}

// FindObjectTargetSpec finds the MetricTarget for an Object metric by name.
func FindObjectTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ObjectMetricSourceType && m.Object != nil && m.Object.Metric.Name == name {
			return m.Object.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindObjectTarget returns the formatted target string for an Object metric.
func FindObjectTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindObjectTargetSpec(hpa, name))
}

// FindExternalTargetSpec finds the MetricTarget for an External metric by name.
func FindExternalTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ExternalMetricSourceType && m.External != nil && m.External.Metric.Name == name {
			return m.External.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

// FindExternalTarget returns the formatted target string for an External metric.
func FindExternalTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindExternalTargetSpec(hpa, name))
}

// --- Metric lookup helpers ---

// hasCurrentExternalMetric reports whether the HPA status contains an
// External metric with the given name.
func hasCurrentExternalMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) bool {
	_, ok := currentExternalMetric(hpa, name)
	return ok
}

// currentExternalMetric returns the External metric status for the given name.
func currentExternalMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (autoscalingv2.MetricStatus, bool) {
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type == autoscalingv2.ExternalMetricSourceType &&
			metric.External != nil &&
			metric.External.Metric.Name == name {
			return metric, true
		}
	}
	return autoscalingv2.MetricStatus{}, false
}

// currentObjectMetric returns the Object metric status for the given name.
func currentObjectMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (autoscalingv2.MetricStatus, bool) {
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type == autoscalingv2.ObjectMetricSourceType &&
			metric.Object != nil &&
			metric.Object.Metric.Name == name {
			return metric, true
		}
	}
	return autoscalingv2.MetricStatus{}, false
}
