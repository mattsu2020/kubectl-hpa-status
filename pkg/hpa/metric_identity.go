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

// MetricIDFromSpec derives a canonical identity and reports malformed selectors
// instead of treating them as equal display strings.
func MetricIDFromSpec(spec autoscalingv2.MetricSpec) (MetricID, error) { //nolint:dupl // Spec and status Kubernetes types intentionally require parallel extraction.
	id := MetricID{Type: spec.Type}
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if spec.Resource == nil {
			return id, fmt.Errorf("resource metric source is nil")
		}
		id.Name = string(spec.Resource.Name)
	case autoscalingv2.ContainerResourceMetricSourceType:
		if spec.ContainerResource == nil {
			return id, fmt.Errorf("container resource metric source is nil")
		}
		id.Name = string(spec.ContainerResource.Name)
		id.Container = spec.ContainerResource.Container
	case autoscalingv2.PodsMetricSourceType:
		if spec.Pods == nil {
			return id, fmt.Errorf("pods metric source is nil")
		}
		id.Name = spec.Pods.Metric.Name
		selector, err := canonicalMetricSelector(spec.Pods.Metric.Selector)
		if err != nil {
			return id, err
		}
		id.Selector = selector
	case autoscalingv2.ExternalMetricSourceType:
		if spec.External == nil {
			return id, fmt.Errorf("external metric source is nil")
		}
		id.Name = spec.External.Metric.Name
		selector, err := canonicalMetricSelector(spec.External.Metric.Selector)
		if err != nil {
			return id, err
		}
		id.Selector = selector
	case autoscalingv2.ObjectMetricSourceType:
		if spec.Object == nil {
			return id, fmt.Errorf("object metric source is nil")
		}
		id.Name = spec.Object.Metric.Name
		selector, err := canonicalMetricSelector(spec.Object.Metric.Selector)
		if err != nil {
			return id, err
		}
		id.Selector = selector
		id.DescribedObject = spec.Object.DescribedObject.Kind + "/" + spec.Object.DescribedObject.Name
		id.DescribedAPIVersion = spec.Object.DescribedObject.APIVersion
	default:
		return id, fmt.Errorf("unsupported metric source type %q", spec.Type)
	}
	return id, nil
}

// MetricIDFromStatus derives the canonical identity of a current metric.
func MetricIDFromStatus(status autoscalingv2.MetricStatus) (MetricID, error) { //nolint:dupl // Spec and status Kubernetes types intentionally require parallel extraction.
	id := MetricID{Type: status.Type}
	switch status.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if status.Resource == nil {
			return id, fmt.Errorf("resource metric status is nil")
		}
		id.Name = string(status.Resource.Name)
	case autoscalingv2.ContainerResourceMetricSourceType:
		if status.ContainerResource == nil {
			return id, fmt.Errorf("container resource metric status is nil")
		}
		id.Name = string(status.ContainerResource.Name)
		id.Container = status.ContainerResource.Container
	case autoscalingv2.PodsMetricSourceType:
		if status.Pods == nil {
			return id, fmt.Errorf("pods metric status is nil")
		}
		id.Name = status.Pods.Metric.Name
		selector, err := canonicalMetricSelector(status.Pods.Metric.Selector)
		if err != nil {
			return id, err
		}
		id.Selector = selector
	case autoscalingv2.ExternalMetricSourceType:
		if status.External == nil {
			return id, fmt.Errorf("external metric status is nil")
		}
		id.Name = status.External.Metric.Name
		selector, err := canonicalMetricSelector(status.External.Metric.Selector)
		if err != nil {
			return id, err
		}
		id.Selector = selector
	case autoscalingv2.ObjectMetricSourceType:
		if status.Object == nil {
			return id, fmt.Errorf("object metric status is nil")
		}
		id.Name = status.Object.Metric.Name
		selector, err := canonicalMetricSelector(status.Object.Metric.Selector)
		if err != nil {
			return id, err
		}
		id.Selector = selector
		id.DescribedObject = status.Object.DescribedObject.Kind + "/" + status.Object.DescribedObject.Name
		id.DescribedAPIVersion = status.Object.DescribedObject.APIVersion
	default:
		return id, fmt.Errorf("unsupported metric status type %q", status.Type)
	}
	return id, nil
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
