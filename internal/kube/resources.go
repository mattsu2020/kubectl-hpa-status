package kube

import (
	"context"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// ContainerResources holds the resource requests and limits for a single
// container, extracted from a pod template. This is a kube-layer DTO;
// callers in cmd/ convert it to the analysis model in pkg/hpa.
type ContainerResources struct {
	Name     string            `json:"name" yaml:"name"`
	Requests map[string]string `json:"requests,omitempty" yaml:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty" yaml:"limits,omitempty"`
}

// ResourceRequests holds resource information for all containers in a pod
// template. This is a kube-layer DTO; callers in cmd/ convert it to the
// analysis model in pkg/hpa.
type ResourceRequests struct {
	Containers []ContainerResources `json:"containers" yaml:"containers"`
}

// FetchScaleTargetResources fetches the pod template from the scale target
// (Deployment, StatefulSet, or ReplicaSet) and extracts container resource
// requests and limits. Returns (nil, nil) for unsupported kinds without error;
// callers must check for a nil pointer before use.
//
//nolint:nilnil // nil result with no error is intentional for unsupported kinds
func FetchScaleTargetResources(ctx context.Context, client kubernetes.Interface, namespace, kind, name string) (*ResourceRequests, error) {
	ref := autoscalingv2.CrossVersionObjectReference{Kind: kind, Name: name}
	info, err := FetchScaleTargetInfo(ctx, client, namespace, ref)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	return extractResourcesFromPodTemplate(info.PodTemplate), nil
}

// extractResourcesFromPodTemplate extracts resource requests and limits from
// a pod template spec into a ResourceRequests structure.
func extractResourcesFromPodTemplate(tmpl *corev1.PodTemplateSpec) *ResourceRequests {
	if tmpl == nil {
		return nil
	}

	containers := tmpl.Spec.Containers
	if len(containers) == 0 {
		return nil
	}

	result := &ResourceRequests{
		Containers: make([]ContainerResources, 0, len(containers)),
	}

	for _, container := range containers {
		cr := ContainerResources{
			Name:     container.Name,
			Requests: make(map[string]string),
			Limits:   make(map[string]string),
		}

		for name, quantity := range container.Resources.Requests {
			cr.Requests[string(name)] = quantity.String()
		}
		for name, quantity := range container.Resources.Limits {
			cr.Limits[string(name)] = quantity.String()
		}

		result.Containers = append(result.Containers, cr)
	}

	return result
}
