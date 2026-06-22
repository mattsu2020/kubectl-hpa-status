package hpa

import (
	"fmt"
	"strconv"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// ContainerResources holds the resource requests and limits for a single
// container. This type lives in pkg/hpa so external consumers can construct
// resource-check inputs without depending on internal/kube; internal/kube
// re-exports it as ContainerResources for backwards compatibility.
type ContainerResources struct {
	Name     string            `json:"name" yaml:"name"`
	Requests map[string]string `json:"requests,omitempty" yaml:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty" yaml:"limits,omitempty"`
}

// ResourceRequests holds resource information for all containers in a pod template.
type ResourceRequests struct {
	Containers []ContainerResources `json:"containers" yaml:"containers"`
}

// Tiny resource request thresholds below which HPA utilization becomes noisy.
const (
	tinyCPUThreshold    = "10m"
	tinyMemoryThreshold = "16Mi"
	// sidecarDistortionRatio is the minimum ratio between the largest and
	// smallest container request that triggers a sidecar-distortion warning.
	sidecarDistortionRatio = 3.0
)

// CheckResourceConsistency validates that HPA resource metrics have
// corresponding pod resource requests set and warns about potential
// misconfigurations. Returns nil when there are no warnings.
func CheckResourceConsistency(hpa *autoscalingv2.HorizontalPodAutoscaler, resources *ResourceRequests) *ResourceCheckResult {
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
			resName := string(metric.Resource.Name)
			warnings = append(warnings, checkResourceMetricAllContainers(
				resName,
				metric.Resource.Target,
				containerMap,
			)...)
			warnings = append(warnings, checkSidecarDistortion(containerMap, resName)...)
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
func checkResourceMetricAllContainers(resourceName string, target autoscalingv2.MetricTarget, containerMap map[string]ContainerResources) []ResourceWarning {
	var warnings []ResourceWarning
	for containerName, cr := range containerMap {
		warnings = append(warnings, checkSingleContainer(containerName, resourceName, target, cr)...)
	}
	return warnings
}

// checkSingleContainer checks a single container for resource consistency.
func checkSingleContainer(containerName, resourceName string, target autoscalingv2.MetricTarget, cr ContainerResources) []ResourceWarning {
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

	if isTinyRequest(resourceName, requestValue) {
		warnings = append(warnings, ResourceWarning{
			Container: containerName,
			Resource:  resourceName,
			Category:  "tiny-request",
			Details:   fmt.Sprintf("container %q has a very small %s request (%s); HPA utilization will be noisy because small absolute changes produce large percentage swings (threshold: %s)", containerName, resourceName, requestValue, tinyThreshold(resourceName)),
			Severity:  "warning",
		})
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

	// Check for missing limits.
	_, hasLimit := cr.Limits[resourceName]
	if !hasLimit {
		warnings = append(warnings, ResourceWarning{
			Container: containerName,
			Resource:  resourceName,
			Category:  "missing-limits",
			Details:   fmt.Sprintf("container %q has no %s limit; without limits, OOM kills may occur and resource utilization may be unpredictable", containerName, resourceName),
			Severity:  "warning",
		})
	}

	return warnings
}

// buildContainerMap creates a name-keyed map from a slice of ContainerResources.
func buildContainerMap(containers []ContainerResources) map[string]ContainerResources {
	m := make(map[string]ContainerResources, len(containers))
	for _, c := range containers {
		m[c.Name] = c
	}
	return m
}

// isTinyRequest checks if a resource request is below the "tiny" threshold
// where HPA utilization percentages become excessively noisy.
func isTinyRequest(resourceName, requestValue string) bool {
	threshold := tinyThreshold(resourceName)
	if threshold == "" {
		return false
	}
	thresholdQty := parseQuantityOrZero(threshold)
	requestQty := parseQuantityOrZero(requestValue)
	return requestQty.Cmp(thresholdQty) < 0
}

// tinyThreshold returns the tiny-request threshold string for the given resource name.
func tinyThreshold(resourceName string) string {
	switch resourceName {
	case "cpu":
		return tinyCPUThreshold
	case "memory":
		return tinyMemoryThreshold
	default:
		return ""
	}
}

// checkSidecarDistortion detects when a Resource-type metric (which averages
// across all containers) is applied to a pod with containers whose requests
// differ significantly, causing utilization distortion.
func checkSidecarDistortion(containerMap map[string]ContainerResources, resourceName string) []ResourceWarning {
	if len(containerMap) < 2 {
		return nil
	}

	var minVal, maxVal float64
	var minName, maxName string
	first := true
	for name, cr := range containerMap {
		requestStr, ok := cr.Requests[resourceName]
		if !ok || isZeroQuantity(requestStr) {
			continue
		}
		q := parseQuantityOrZero(requestStr)
		val := q.AsApproximateFloat64()
		if first {
			minVal, maxVal = val, val
			minName, maxName = name, name
			first = false
		} else {
			if val < minVal {
				minVal = val
				minName = name
			}
			if val > maxVal {
				maxVal = val
				maxName = name
			}
		}
	}

	if minVal == 0 || maxVal == 0 {
		return nil
	}

	ratio := maxVal / minVal
	if ratio < sidecarDistortionRatio {
		return nil
	}

	return []ResourceWarning{{
		Container: fmt.Sprintf("%s/%s", minName, maxName),
		Resource:  resourceName,
		Category:  "sidecar-distortion",
		Details: fmt.Sprintf("containers have a %.1fx difference in %s requests (%s=%v, %s=%v); "+
			"Resource metric averages across all containers, so the smaller container distorts utilization. "+
			"Consider switching to ContainerResource metric type to target specific containers.",
			ratio, resourceName, minName, parseQuantityOrZero(containerMap[minName].Requests[resourceName]),
			maxName, parseQuantityOrZero(containerMap[maxName].Requests[resourceName])),
		Severity: "warning",
	}}
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
