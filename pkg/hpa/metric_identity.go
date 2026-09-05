package hpa

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/metricidentity"
)

// MetricID is the canonical identity of one HPA metric. Metric name alone is
// insufficient for ContainerResource, selectors, and Object metrics.
//
// The implementation lives in pkg/hpa/internal/metricidentity so the simulate
// package can share it directly; this alias keeps the public hpa package
// shape unchanged.
type MetricID = metricidentity.MetricID

// MetricDescriptor is the normalized representation consumed by analysis,
// simulation, freshness, and rendering. Spec and status are converted once at
// their boundary instead of every consumer maintaining five type switches.
type MetricDescriptor = metricidentity.MetricDescriptor

// MetricDescriptorFromSpec normalizes a metric specification and reports
// malformed selectors instead of treating them as equal display strings.
func MetricDescriptorFromSpec(spec autoscalingv2.MetricSpec) (MetricDescriptor, error) {
	return metricidentity.MetricDescriptorFromSpec(spec)
}

// MetricIDFromSpec derives the canonical identity of a metric specification.
func MetricIDFromSpec(spec autoscalingv2.MetricSpec) (MetricID, error) {
	return metricidentity.MetricIDFromSpec(spec)
}

// MetricDescriptorFromStatus normalizes the identity and current value of a metric status.
func MetricDescriptorFromStatus(status autoscalingv2.MetricStatus) (MetricDescriptor, error) {
	return metricidentity.MetricDescriptorFromStatus(status)
}

// MetricIDFromStatus derives the canonical identity of a current metric.
func MetricIDFromStatus(status autoscalingv2.MetricStatus) (MetricID, error) {
	return metricidentity.MetricIDFromStatus(status)
}

func canonicalMetricSelector(selector *metav1.LabelSelector) (string, error) {
	return metricidentity.CanonicalMetricSelector(selector)
}

func metricIdentityMatches(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	return metricidentity.IdentityMatches(spec, current)
}
