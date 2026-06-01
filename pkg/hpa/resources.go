package hpa

import (
	"fmt"
	"strconv"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// CheckResourceConsistency validates that HPA resource metrics have
// corresponding pod resource requests set and warns about potential
// misconfigurations. Returns nil when there are no warnings.
func CheckResourceConsistency(hpa *autoscalingv2.HorizontalPodAutoscaler, resources *kube.ResourceRequests) *ResourceCheckResult {
	if hpa == nil || resources == nil {
		return nil
	}

	containerMap := buildContainerMap(resources.Containers)
	var warnings []ResourceWarning

	for _, metric := range hpa.Spec.Metrics {
		switch metric.Type {
		case autoscalingv2.ResourceMetricSourceType:
			if metric.Resource == nil {
				continue
			}
			warnings = append(warnings, checkResourceMetricAllContainers(
				string(metric.Resource.Name),
				metric.Resource.Target,
				containerMap,
			)...)
		case autoscalingv2.ContainerResourceMetricSourceType:
			if metric.ContainerResource == nil {
				continue
			}
			containerName := metric.ContainerResource.Container
			cr, ok := containerMap[containerName]
			if !ok {
				warnings = append(warnings, ResourceWarning{
					Container: containerName,
					Resource:  string(metric.ContainerResource.Name),
					Category:  "missing-requests",
					Details:   fmt.Sprintf("container %q not found in pod template", containerName),
					Severity:  "error",
				})
				continue
			}
			warnings = append(warnings, checkSingleContainer(
				containerName,
				string(metric.ContainerResource.Name),
				metric.ContainerResource.Target,
				cr,
			)...)
		}
	}

	if len(warnings) == 0 {
		return nil
	}

	return &ResourceCheckResult{Warnings: warnings}
}

// checkResourceMetricAllContainers checks a Resource-type metric (applies to
// all containers) against every container in the pod template.
func checkResourceMetricAllContainers(resourceName string, target autoscalingv2.MetricTarget, containerMap map[string]kube.ContainerResources) []ResourceWarning {
	var warnings []ResourceWarning
	for containerName, cr := range containerMap {
		warnings = append(warnings, checkSingleContainer(containerName, resourceName, target, cr)...)
	}
	return warnings
}

// checkSingleContainer checks a single container for resource consistency.
func checkSingleContainer(containerName, resourceName string, target autoscalingv2.MetricTarget, cr kube.ContainerResources) []ResourceWarning {
	var warnings []ResourceWarning

	requestValue, hasRequest := cr.Requests[resourceName]

	if !hasRequest {
		warnings = append(warnings, ResourceWarning{
			Container: containerName,
			Resource:  resourceName,
			Category:  "missing-requests",
			Details:   fmt.Sprintf("container %q has no %s request; HPA cannot calculate utilization without resource requests", containerName, resourceName),
			Severity:  "error",
		})
		return warnings
	}

	if isZeroQuantity(requestValue) {
		warnings = append(warnings, ResourceWarning{
			Container: containerName,
			Resource:  resourceName,
			Category:  "zero-requests",
			Details:   fmt.Sprintf("container %q has a zero %s request (%s); HPA utilization calculation will divide by zero", containerName, resourceName, requestValue),
			Severity:  "error",
		})
		return warnings
	}

	if target.AverageUtilization != nil && *target.AverageUtilization > 90 {
		warnings = append(warnings, ResourceWarning{
			Container: containerName,
			Resource:  resourceName,
			Category:  "target-vs-request-mismatch",
			Details:   fmt.Sprintf("container %q has %s target utilization %d%% which is very high; only %d%% headroom remains before hitting 100%%", containerName, resourceName, *target.AverageUtilization, 100-*target.AverageUtilization),
			Severity:  "warning",
		})
	}

	return warnings
}

// buildContainerMap creates a name-keyed map from a slice of ContainerResources.
func buildContainerMap(containers []kube.ContainerResources) map[string]kube.ContainerResources {
	m := make(map[string]kube.ContainerResources, len(containers))
	for _, c := range containers {
		m[c.Name] = c
	}
	return m
}

// isZeroQuantity checks if a resource quantity string represents zero.
// Handles common formats like "0", "0m", "0Ki", "0Mi", "0Gi", etc.
func isZeroQuantity(value string) bool {
	if value == "" || value == "0" {
		return true
	}
	// Try parsing as an integer (handles bare "0")
	if n, err := strconv.Atoi(value); err == nil && n == 0 {
		return true
	}
	// Handle Kubernetes quantity suffixes: try parsing the numeric prefix
	// for formats like "0m", "0Ki", "0Mi", "0Gi", "0Ti", "0Pi", "0Ei"
	// and decimal formats like "0.1", "0.00"
	numStr := value
	for i, r := range value {
		if (r < '0' || r > '9') && r != '.' && r != '-' {
			numStr = value[:i]
			break
		}
	}
	if f, err := strconv.ParseFloat(numStr, 64); err == nil && f == 0 {
		return true
	}
	return false
}
