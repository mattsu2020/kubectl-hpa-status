package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetricID is the canonical identity of one HPA metric. Metric name alone is
// insufficient for ContainerResource, selectors, and Object metrics.
type MetricID struct {
	Type                autoscalingv2.MetricSourceType `json:"type" yaml:"type"`
	Name                string                         `json:"name" yaml:"name"`
	Container           string                         `json:"container,omitempty" yaml:"container,omitempty"`
	Selector            string                         `json:"selector,omitempty" yaml:"selector,omitempty"`
	DescribedObject     string                         `json:"describedObject,omitempty" yaml:"describedObject,omitempty"`
	DescribedAPIVersion string                         `json:"describedApiVersion,omitempty" yaml:"describedApiVersion,omitempty"`
}

// MetricDescriptor is the normalized representation consumed by analysis,
// simulation, freshness, and rendering. Spec and status are converted once at
// their boundary instead of every consumer maintaining five type switches.
type MetricDescriptor struct {
	ID       MetricID
	Target   *autoscalingv2.MetricTarget
	Current  *autoscalingv2.MetricValueStatus
	Selector *metav1.LabelSelector
}

// MetricDescriptorFromSpec normalizes a metric specification and reports
// malformed selectors instead of treating them as equal display strings.
func MetricDescriptorFromSpec(spec autoscalingv2.MetricSpec) (MetricDescriptor, error) { //nolint:dupl,gocyclo // Mirrors the upstream spec/status union types.
	id := MetricID{Type: spec.Type}
	descriptor := MetricDescriptor{ID: id}
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if spec.Resource == nil {
			return descriptor, fmt.Errorf("resource metric source is nil")
		}
		id.Name = string(spec.Resource.Name)
		descriptor.Target = &spec.Resource.Target
	case autoscalingv2.ContainerResourceMetricSourceType:
		if spec.ContainerResource == nil {
			return descriptor, fmt.Errorf("container resource metric source is nil")
		}
		id.Name = string(spec.ContainerResource.Name)
		id.Container = spec.ContainerResource.Container
		descriptor.Target = &spec.ContainerResource.Target
	case autoscalingv2.PodsMetricSourceType:
		if spec.Pods == nil {
			return descriptor, fmt.Errorf("pods metric source is nil")
		}
		id.Name = spec.Pods.Metric.Name
		descriptor.Target, descriptor.Selector = &spec.Pods.Target, spec.Pods.Metric.Selector
		selector, err := canonicalMetricSelector(spec.Pods.Metric.Selector)
		if err != nil {
			return descriptor, err
		}
		id.Selector = selector
	case autoscalingv2.ExternalMetricSourceType:
		if spec.External == nil {
			return descriptor, fmt.Errorf("external metric source is nil")
		}
		id.Name = spec.External.Metric.Name
		descriptor.Target, descriptor.Selector = &spec.External.Target, spec.External.Metric.Selector
		selector, err := canonicalMetricSelector(spec.External.Metric.Selector)
		if err != nil {
			return descriptor, err
		}
		id.Selector = selector
	case autoscalingv2.ObjectMetricSourceType:
		if spec.Object == nil {
			return descriptor, fmt.Errorf("object metric source is nil")
		}
		id.Name = spec.Object.Metric.Name
		descriptor.Target, descriptor.Selector = &spec.Object.Target, spec.Object.Metric.Selector
		selector, err := canonicalMetricSelector(spec.Object.Metric.Selector)
		if err != nil {
			return descriptor, err
		}
		id.Selector = selector
		id.DescribedObject = spec.Object.DescribedObject.Kind + "/" + spec.Object.DescribedObject.Name
		id.DescribedAPIVersion = spec.Object.DescribedObject.APIVersion
	default:
		return descriptor, fmt.Errorf("unsupported metric source type %q", spec.Type)
	}
	descriptor.ID = id
	return descriptor, nil
}

// MetricIDFromSpec derives the canonical identity of a metric specification.
func MetricIDFromSpec(spec autoscalingv2.MetricSpec) (MetricID, error) {
	descriptor, err := MetricDescriptorFromSpec(spec)
	return descriptor.ID, err
}

// MetricDescriptorFromStatus normalizes the identity and current value of a metric status.
func MetricDescriptorFromStatus(status autoscalingv2.MetricStatus) (MetricDescriptor, error) { //nolint:dupl,gocyclo // Mirrors the upstream spec/status union types.
	id := MetricID{Type: status.Type}
	descriptor := MetricDescriptor{ID: id}
	switch status.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if status.Resource == nil {
			return descriptor, fmt.Errorf("resource metric status is nil")
		}
		id.Name = string(status.Resource.Name)
		descriptor.Current = &status.Resource.Current
	case autoscalingv2.ContainerResourceMetricSourceType:
		if status.ContainerResource == nil {
			return descriptor, fmt.Errorf("container resource metric status is nil")
		}
		id.Name = string(status.ContainerResource.Name)
		id.Container = status.ContainerResource.Container
		descriptor.Current = &status.ContainerResource.Current
	case autoscalingv2.PodsMetricSourceType:
		if status.Pods == nil {
			return descriptor, fmt.Errorf("pods metric status is nil")
		}
		id.Name = status.Pods.Metric.Name
		descriptor.Current, descriptor.Selector = &status.Pods.Current, status.Pods.Metric.Selector
		selector, err := canonicalMetricSelector(status.Pods.Metric.Selector)
		if err != nil {
			return descriptor, err
		}
		id.Selector = selector
	case autoscalingv2.ExternalMetricSourceType:
		if status.External == nil {
			return descriptor, fmt.Errorf("external metric status is nil")
		}
		id.Name = status.External.Metric.Name
		descriptor.Current, descriptor.Selector = &status.External.Current, status.External.Metric.Selector
		selector, err := canonicalMetricSelector(status.External.Metric.Selector)
		if err != nil {
			return descriptor, err
		}
		id.Selector = selector
	case autoscalingv2.ObjectMetricSourceType:
		if status.Object == nil {
			return descriptor, fmt.Errorf("object metric status is nil")
		}
		id.Name = status.Object.Metric.Name
		descriptor.Current, descriptor.Selector = &status.Object.Current, status.Object.Metric.Selector
		selector, err := canonicalMetricSelector(status.Object.Metric.Selector)
		if err != nil {
			return descriptor, err
		}
		id.Selector = selector
		id.DescribedObject = status.Object.DescribedObject.Kind + "/" + status.Object.DescribedObject.Name
		id.DescribedAPIVersion = status.Object.DescribedObject.APIVersion
	default:
		return descriptor, fmt.Errorf("unsupported metric status type %q", status.Type)
	}
	descriptor.ID = id
	return descriptor, nil
}

// MetricIDFromStatus derives the canonical identity of a current metric.
func MetricIDFromStatus(status autoscalingv2.MetricStatus) (MetricID, error) {
	descriptor, err := MetricDescriptorFromStatus(status)
	return descriptor.ID, err
}

func canonicalMetricSelector(selector *metav1.LabelSelector) (string, error) {
	if selector == nil {
		return "", nil
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return "", fmt.Errorf("invalid metric selector: %w", err)
	}
	if parsed.Empty() {
		return "", nil
	}
	return parsed.String(), nil
}

func metricIdentityMatches(spec autoscalingv2.MetricSpec, current autoscalingv2.MetricStatus) bool {
	specID, specErr := MetricIDFromSpec(spec)
	currentID, currentErr := MetricIDFromStatus(current)
	return specErr == nil && currentErr == nil && specID == currentID
}
