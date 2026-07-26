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
	Containers     []ContainerResources `json:"containers" yaml:"containers"`
	InitContainers []ContainerResources `json:"initContainers,omitempty" yaml:"initContainers,omitempty"`
	// PodRequests is the effective scheduler request for the whole Pod. It
	// includes regular containers, restartable and non-restartable init
	// containers, Pod-level requests, and Pod overhead.
	PodRequests map[string]string `json:"podRequests,omitempty" yaml:"podRequests,omitempty"`
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
	return ResourceRequestsFromPodTemplate(info.PodTemplate), nil
}

// ResourceRequestsFromPodTemplate extracts container resources and the
// effective scheduler request from a Pod template.
func ResourceRequestsFromPodTemplate(tmpl *corev1.PodTemplateSpec) *ResourceRequests {
	if tmpl == nil {
		return nil
	}

	containers := tmpl.Spec.Containers
	if len(containers) == 0 {
		return nil
	}

	result := &ResourceRequests{
		Containers:     make([]ContainerResources, 0, len(containers)),
		InitContainers: make([]ContainerResources, 0, len(tmpl.Spec.InitContainers)),
		PodRequests:    make(map[string]string),
	}

	for _, container := range containers {
		result.Containers = append(result.Containers, containerResources(container))
	}
	for _, container := range tmpl.Spec.InitContainers {
		result.InitContainers = append(result.InitContainers, containerResources(container))
	}
	for name, quantity := range EffectivePodRequests(tmpl.Spec) {
		result.PodRequests[string(name)] = quantity.String()
	}

	return result
}

// extractResourcesFromPodTemplate is kept as a package-local compatibility
// wrapper for existing tests and helpers.
func extractResourcesFromPodTemplate(tmpl *corev1.PodTemplateSpec) *ResourceRequests {
	return ResourceRequestsFromPodTemplate(tmpl)
}

func containerResources(container corev1.Container) ContainerResources {
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
	return cr
}

// EffectivePodRequests returns the resources reserved by the scheduler for a
// Pod. It follows the init-container scheduling model: normal containers and
// restartable init containers are summed, while each non-restartable init
// phase is compared against that running restartable-init total. Pod-level
// requests are treated as a lower bound, and Pod overhead is added last.
func EffectivePodRequests(spec corev1.PodSpec) corev1.ResourceList {
	running := corev1.ResourceList{}
	for _, container := range spec.Containers {
		addResourceList(running, container.Resources.Requests)
	}

	restartableInit := corev1.ResourceList{}
	initPeak := corev1.ResourceList{}
	for _, container := range spec.InitContainers {
		requests := container.Resources.Requests
		if container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			addResourceList(running, requests)
			addResourceList(restartableInit, requests)
			maxResourceList(initPeak, restartableInit)
			continue
		}

		phase := copyResourceList(restartableInit)
		addResourceList(phase, requests)
		maxResourceList(initPeak, phase)
	}
	maxResourceList(running, initPeak)

	if spec.Resources != nil {
		maxResourceList(running, spec.Resources.Requests)
	}
	addResourceList(running, spec.Overhead)
	return running
}

func copyResourceList(in corev1.ResourceList) corev1.ResourceList {
	out := make(corev1.ResourceList, len(in))
	for name, quantity := range in {
		out[name] = quantity.DeepCopy()
	}
	return out
}

func addResourceList(dst, src corev1.ResourceList) {
	for name, quantity := range src {
		total := dst[name]
		total.Add(quantity)
		dst[name] = total
	}
}

func maxResourceList(dst, candidate corev1.ResourceList) {
	for name, quantity := range candidate {
		current, ok := dst[name]
		if !ok || current.Cmp(quantity) < 0 {
			dst[name] = quantity.DeepCopy()
		}
	}
}
