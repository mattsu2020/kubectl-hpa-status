package kube

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// ContainerResources holds the resource requests and limits for a single
// container. Aliased from pkg/hpa for backwards compatibility.
type ContainerResources = hpaanalysis.ContainerResources

// ResourceRequests holds resource information for all containers in a pod
// template. Aliased from pkg/hpa for backwards compatibility.
type ResourceRequests = hpaanalysis.ResourceRequests

// FetchScaleTargetResources fetches the pod template from the scale target
// (Deployment, StatefulSet, or ReplicaSet) and extracts container resource
// requests and limits. Returns nil for unsupported kinds without error.
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

// extractResourcesFromAppsv1PodTemplate extracts resources from an apps/v1
// Deployment, StatefulSet, or ReplicaSet pod template using the shared
// extractResourcesFromPodTemplate helper.
func extractResourcesFromAppsv1PodTemplate(spec *appsv1.DeploymentSpec) *ResourceRequests {
	if spec == nil {
		return nil
	}
	return extractResourcesFromPodTemplate(&spec.Template)
}
