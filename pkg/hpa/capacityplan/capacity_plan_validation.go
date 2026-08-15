package capacityplan

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

func validateCapacityQuantityInputs(input Input) []CapacityObservationError {
	var validationErrors []CapacityObservationError
	validate := func(domain CapacityObservationDomain, source, value string, allowNegative bool) {
		if value == "" {
			return
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			validationErrors = append(validationErrors, CapacityObservationError{
				Domain:  domain,
				Source:  source,
				Message: "invalid Kubernetes quantity: value must not be whitespace-only",
			})
			return
		}
		quantity, err := resource.ParseQuantity(value)
		switch {
		case err != nil:
			validationErrors = append(validationErrors, CapacityObservationError{
				Domain:  domain,
				Source:  source,
				Message: fmt.Sprintf("invalid Kubernetes quantity %q: %v", value, err),
			})
		case !allowNegative && quantity.Sign() < 0:
			validationErrors = append(validationErrors, CapacityObservationError{
				Domain:  domain,
				Source:  source,
				Message: fmt.Sprintf("invalid Kubernetes quantity %q: value must not be negative", value),
			})
		}
	}

	validate(CapacityObservationPodResources, "PodRequestCPU", input.PodRequestCPU, false)
	validate(CapacityObservationPodResources, "PodRequestMemory", input.PodRequestMemory, false)
	validate(CapacityObservationPodResources, "PodLimitCPU", input.PodLimitCPU, false)
	validate(CapacityObservationPodResources, "PodLimitMemory", input.PodLimitMemory, false)
	validate(CapacityObservationPodResources, "PodLevelRequestCPU", input.PodLevelRequestCPU, false)
	validate(CapacityObservationPodResources, "PodLevelRequestMemory", input.PodLevelRequestMemory, false)
	validate(CapacityObservationPodResources, "PodLevelLimitCPU", input.PodLevelLimitCPU, false)
	validate(CapacityObservationPodResources, "PodLevelLimitMemory", input.PodLevelLimitMemory, false)
	for _, container := range input.ContainerResources {
		validate(CapacityObservationPodResources, fmt.Sprintf("container %q CPU request", container.Name), container.CPU, false)
		validate(CapacityObservationPodResources, fmt.Sprintf("container %q memory request", container.Name), container.Memory, false)
		validate(CapacityObservationPodResources, fmt.Sprintf("container %q CPU limit", container.Name), container.LimitCPU, false)
		validate(CapacityObservationPodResources, fmt.Sprintf("container %q memory limit", container.Name), container.LimitMemory, false)
	}
	for _, quota := range input.Quotas {
		validate(CapacityObservationResourceQuotas, fmt.Sprintf("ResourceQuota %q %s hard", quota.Name, quota.Resource), quota.Hard, false)
		validate(CapacityObservationResourceQuotas, fmt.Sprintf("ResourceQuota %q %s used", quota.Name, quota.Resource), quota.Used, false)
	}
	for _, limitRange := range input.LimitRanges {
		validate(CapacityObservationLimitRanges, fmt.Sprintf("LimitRange %q %s minimum", limitRange.Name, limitRange.Resource), limitRange.Min, false)
		validate(CapacityObservationLimitRanges, fmt.Sprintf("LimitRange %q %s maximum", limitRange.Name, limitRange.Resource), limitRange.Max, false)
		validate(CapacityObservationLimitRanges, fmt.Sprintf("LimitRange %q %s default", limitRange.Name, limitRange.Resource), limitRange.Default, false)
		validate(CapacityObservationLimitRanges, fmt.Sprintf("LimitRange %q %s default request", limitRange.Name, limitRange.Resource), limitRange.DefaultRequest, false)
		validate(CapacityObservationLimitRanges, fmt.Sprintf("LimitRange %q %s max limit/request ratio", limitRange.Name, limitRange.Resource), limitRange.MaxLimitRequestRatio, false)
	}
	if input.NodeCapacity != nil {
		validate(CapacityObservationNodeCapacity, "node allocatable CPU", input.NodeCapacity.AllocCPU, false)
		validate(CapacityObservationNodeCapacity, "node allocatable memory", input.NodeCapacity.AllocMemory, false)
		// Available headroom can legitimately be negative when aggregate
		// scheduled Pod requests exceed allocatable capacity.
		validate(CapacityObservationNodeCapacity, "node available CPU", input.NodeCapacity.AvailableCPU, true)
		validate(CapacityObservationNodeCapacity, "node available memory", input.NodeCapacity.AvailableMemory, true)
		validate(CapacityObservationNodeCapacity, "node requested CPU", input.NodeCapacity.RequestedCPU, false)
		validate(CapacityObservationNodeCapacity, "node requested memory", input.NodeCapacity.RequestedMemory, false)
		for _, node := range input.NodeCapacity.NodeHeadrooms {
			validate(CapacityObservationNodeCapacity, fmt.Sprintf("node %q available CPU", node.Name), node.AvailableCPU, true)
			validate(CapacityObservationNodeCapacity, fmt.Sprintf("node %q available memory", node.Name), node.AvailableMemory, true)
		}
	}
	return validationErrors
}

func unsupportedLimitRangeObservations(limitRanges []LimitRangeConstraint, containers []CapacityContainerResources) []CapacityObservationError {
	var observations []CapacityObservationError
	for _, limitRange := range limitRanges {
		var semantics []string
		requestMissing, limitMissing := limitRangeResourceMissing(containers, limitRange.Resource)
		if limitRange.Default != "" && (limitRange.Type != "Container" || limitMissing) {
			semantics = append(semantics, "default limit")
		}
		if limitRange.DefaultRequest != "" && (limitRange.Type != "Container" || requestMissing) {
			semantics = append(semantics, "default request")
		}
		if len(semantics) == 0 {
			continue
		}
		observations = append(observations, CapacityObservationError{
			Domain: CapacityObservationLimitRangeDefaults,
			Source: fmt.Sprintf("LimitRange %q %s", limitRange.Name, limitRange.Resource),
			Message: fmt.Sprintf(
				"admission-time %s cannot be projected safely",
				strings.Join(semantics, ", "),
			),
		})
	}
	return observations
}

func limitRangeResourceMissing(containers []CapacityContainerResources, resourceName string) (requestMissing, limitMissing bool) {
	if len(containers) == 0 {
		return true, true
	}
	for _, container := range containers {
		request, limit, _ := limitRangeResourceValues(container, LimitRangeConstraint{Resource: resourceName})
		if strings.TrimSpace(request) == "" {
			requestMissing = true
		}
		if strings.TrimSpace(limit) == "" {
			limitMissing = true
		}
	}
	return requestMissing, limitMissing
}
