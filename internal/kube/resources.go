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
	// PodLimits is the effective whole-Pod limit used for quota projection,
	// including init-container peaks and Pod-level limits.
	PodLimits map[string]string `json:"podLimits,omitempty" yaml:"podLimits,omitempty"`
	// PodLevelRequests/PodLevelLimits preserve spec.resources so ResourceQuota
	// completeness checks can distinguish valid Pod-level declarations from
	// missing per-container resources.
	PodLevelRequests map[string]string `json:"podLevelRequests,omitempty" yaml:"podLevelRequests,omitempty"`
	PodLevelLimits   map[string]string `json:"podLevelLimits,omitempty" yaml:"podLevelLimits,omitempty"`
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

	spec := materializePodSpec(tmpl.Spec)
	containers := spec.Containers
	if len(containers) == 0 {
		return nil
	}

	result := &ResourceRequests{
		Containers:       make([]ContainerResources, 0, len(containers)),
		InitContainers:   make([]ContainerResources, 0, len(tmpl.Spec.InitContainers)),
		PodRequests:      make(map[string]string),
		PodLimits:        make(map[string]string),
		PodLevelRequests: make(map[string]string),
		PodLevelLimits:   make(map[string]string),
	}

	for _, container := range containers {
		result.Containers = append(result.Containers, containerResources(container))
	}
	for _, container := range spec.InitContainers {
		result.InitContainers = append(result.InitContainers, containerResources(container))
	}
	for name, quantity := range EffectivePodRequests(spec) {
		result.PodRequests[string(name)] = quantity.String()
	}
	for name, quantity := range EffectivePodLimits(spec) {
		result.PodLimits[string(name)] = quantity.String()
	}
	if spec.Resources != nil {
		for name, quantity := range spec.Resources.Requests {
			result.PodLevelRequests[string(name)] = quantity.String()
		}
		for name, quantity := range spec.Resources.Limits {
			result.PodLevelLimits[string(name)] = quantity.String()
		}
	}

	return result
}

// materializePodSpec applies the request-from-limit defaulting performed when
// a workload PodTemplate creates a real Pod. The stored template itself is not
// defaulted this way, but capacity planning must model the admitted Pod.
func materializePodSpec(source corev1.PodSpec) corev1.PodSpec {
	spec := *source.DeepCopy()
	defaultRequests := func(containers []corev1.Container) {
		for i := range containers {
			if containers[i].Resources.Requests == nil {
				containers[i].Resources.Requests = corev1.ResourceList{}
			}
			for name, limit := range containers[i].Resources.Limits {
				if _, exists := containers[i].Resources.Requests[name]; !exists {
					containers[i].Resources.Requests[name] = limit.DeepCopy()
				}
			}
		}
	}
	defaultRequests(spec.Containers)
	defaultRequests(spec.InitContainers)
	if spec.Resources != nil {
		if spec.Resources.Requests == nil {
			spec.Resources.Requests = corev1.ResourceList{}
		}
		aggregateSpec := spec
		aggregateSpec.Resources = nil
		aggregateSpec.Overhead = nil
		for name, aggregate := range effectivePodResources(aggregateSpec, false) {
			if _, exists := spec.Resources.Requests[name]; !exists && !aggregate.IsZero() {
				spec.Resources.Requests[name] = aggregate.DeepCopy()
			}
		}
		for name, limit := range spec.Resources.Limits {
			if _, exists := spec.Resources.Requests[name]; !exists {
				spec.Resources.Requests[name] = limit.DeepCopy()
			}
		}
	}
	return spec
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
	return effectivePodResources(spec, false)
}

// EffectivePodLimits returns the effective whole-Pod limits used for quota
// projection, including regular containers, init-container peaks, Pod-level
// limits, and Pod overhead.
func EffectivePodLimits(spec corev1.PodSpec) corev1.ResourceList {
	return effectivePodResources(spec, true)
}

func effectivePodResources(spec corev1.PodSpec, limits bool) corev1.ResourceList {
	resourcesFor := func(container corev1.Container) corev1.ResourceList {
		if limits {
			return container.Resources.Limits
		}
		return container.Resources.Requests
	}

	running := corev1.ResourceList{}
	for _, container := range spec.Containers {
		addResourceList(running, resourcesFor(container))
	}

	restartableInit := corev1.ResourceList{}
	initPeak := corev1.ResourceList{}
	for _, container := range spec.InitContainers {
		resources := resourcesFor(container)
		if container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			addResourceList(running, resources)
			addResourceList(restartableInit, resources)
			maxResourceList(initPeak, restartableInit)
			continue
		}

		phase := copyResourceList(restartableInit)
		addResourceList(phase, resources)
		maxResourceList(initPeak, phase)
	}
	maxResourceList(running, initPeak)

	if spec.Resources != nil {
		if limits {
			overrideResourceList(running, spec.Resources.Limits)
		} else {
			overrideResourceList(running, spec.Resources.Requests)
		}
	}
	if limits {
		// ResourceQuota only adds Pod overhead to a limit dimension that is
		// already present. Overhead alone must not make a missing container
		// limit look explicitly configured.
		for name, overhead := range spec.Overhead {
			current, ok := running[name]
			if !ok || current.IsZero() {
				continue
			}
			current.Add(overhead)
			running[name] = current
		}
	} else {
		addResourceList(running, spec.Overhead)
	}
	return running
}

func overrideResourceList(dst, overrides corev1.ResourceList) {
	for name, quantity := range overrides {
		dst[name] = quantity.DeepCopy()
	}
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
