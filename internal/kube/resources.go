package kube

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ContainerResources holds the resource requests and limits for a single container.
type ContainerResources struct {
	Name     string            `json:"name" yaml:"name"`
	Requests map[string]string `json:"requests,omitempty" yaml:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty" yaml:"limits,omitempty"`
}

// ResourceRequests holds resource information for all containers in a pod template.
type ResourceRequests struct {
	Containers []ContainerResources `json:"containers" yaml:"containers"`
}

// FetchScaleTargetResources fetches the pod template from the scale target
// (Deployment, StatefulSet, or ReplicaSet) and extracts container resource
// requests and limits. Returns nil for unsupported kinds without error.
func FetchScaleTargetResources(ctx context.Context, client kubernetes.Interface, namespace, kind, name string) (*ResourceRequests, error) {
	switch kind {
	case "Deployment":
		return fetchDeploymentResources(ctx, client, namespace, name)
	case "StatefulSet":
		return fetchStatefulSetResources(ctx, client, namespace, name)
	case "ReplicaSet":
		return fetchReplicaSetResources(ctx, client, namespace, name)
	default:
		return nil, nil
	}
}

func fetchDeploymentResources(ctx context.Context, client kubernetes.Interface, namespace, name string) (*ResourceRequests, error) {
	deploy, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Deployment %s/%s: %w", namespace, name, err)
	}
	return extractResourcesFromPodTemplate(&deploy.Spec.Template), nil
}

func fetchStatefulSetResources(ctx context.Context, client kubernetes.Interface, namespace, name string) (*ResourceRequests, error) {
	sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get StatefulSet %s/%s: %w", namespace, name, err)
	}
	return extractResourcesFromPodTemplate(&sts.Spec.Template), nil
}

func fetchReplicaSetResources(ctx context.Context, client kubernetes.Interface, namespace, name string) (*ResourceRequests, error) {
	rs, err := client.AppsV1().ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ReplicaSet %s/%s: %w", namespace, name, err)
	}
	return extractResourcesFromPodTemplate(&rs.Spec.Template), nil
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
