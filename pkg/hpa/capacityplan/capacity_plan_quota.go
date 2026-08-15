package capacityplan

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// This file holds the namespace-quota and LimitRange capacity checks, split
// out of capacity_plan.go so that file stays within the project's file-size
// guidance. AnalyzeCapacityPlan in capacity_plan.go remains the only caller.

func checkQuotaHeadroom(
	quotas []CapacityQuotaInfo,
	input Input,
	requiredCPU, requiredMemory, requiredLimitCPU, requiredLimitMemory resource.Quantity,
	additionalPods int32,
) []CapacityCheckResult {
	if len(quotas) == 0 {
		return []CapacityCheckResult{
			newCapacityCheckResult(CapacityCheckQuota, CapacityCheckPass, "no namespace ResourceQuotas found"),
		}
	}

	var results []CapacityCheckResult
	constrained := false
	requiredPods := *resource.NewQuantity(int64(additionalPods), resource.DecimalSI)
	for _, q := range quotas {
		display, required, checkID, ok := quotaResourceRequirement(
			q.Resource,
			requiredCPU,
			requiredMemory,
			requiredLimitCPU,
			requiredLimitMemory,
			requiredPods,
		)
		if !ok {
			continue
		}
		constrained = true
		if q.Scoped {
			result := newCapacityCheckResult(
				CapacityCheckObservation,
				CapacityCheckUnknown,
				fmt.Sprintf("ResourceQuota %q has scopes whose applicability to the target Pod was not evaluated", q.Name),
			)
			result.ObservationDomain = CapacityObservationResourceQuotas
			results = append(results, result)
			continue
		}
		if q.HardObserved != nil && !*q.HardObserved {
			result := newCapacityCheckResult(
				CapacityCheckObservation,
				CapacityCheckUnknown,
				fmt.Sprintf("ResourceQuota %q status.hard is missing %s", q.Name, q.Resource),
			)
			result.ObservationDomain = CapacityObservationResourceQuotas
			results = append(results, result)
			continue
		}
		if additionalPods > 0 && !allContainersSpecifyQuotaResource(input, q.Resource) {
			result := newCapacityCheckResult(
				CapacityCheckObservation,
				CapacityCheckUnknown,
				fmt.Sprintf(
					"ResourceQuota %q constrains %s, but at least one target container has no explicit matching request or limit",
					q.Name,
					q.Resource,
				),
			)
			result.ObservationDomain = CapacityObservationPodResources
			results = append(results, result)
			continue
		}
		if q.UsageObserved != nil && !*q.UsageObserved {
			result := newCapacityCheckResult(
				CapacityCheckObservation,
				CapacityCheckUnknown,
				fmt.Sprintf("ResourceQuota %q status.used is missing %s", q.Name, q.Resource),
			)
			result.ObservationDomain = CapacityObservationResourceQuotas
			results = append(results, result)
			continue
		}
		hard := parseQuantityOrZero(q.Hard)
		used := parseQuantityOrZero(q.Used)
		rem := hard.DeepCopy()
		rem.Sub(used)
		if rem.Cmp(required) >= 0 {
			results = append(results, newCapacityCheckResult(
				checkID,
				CapacityCheckPass,
				fmt.Sprintf("namespace quota %s remaining in %q is sufficient (%s)", display, q.Name, rem.String()),
			))
		} else {
			results = append(results, newCapacityCheckResult(
				checkID,
				CapacityCheckFail,
				fmt.Sprintf("namespace quota %s remaining in %q: %s, required: %s", display, q.Name, rem.String(), required.String()),
			))
		}
	}

	// If no cpu/memory quota found, report pass.
	if !constrained {
		results = append(results, newCapacityCheckResult(
			CapacityCheckQuota,
			CapacityCheckPass,
			"namespace quota does not constrain cpu, memory, or pods",
		))
	}

	return results
}

// allContainersSpecifyQuotaResource reports whether the workload declares the
// value a given quota constrains, so a shortfall can be stated as a fact
// instead of inferred from a partially-specified Pod spec.
//
// A Pod-level declaration satisfies the quota outright; otherwise every
// container must carry the corresponding field.
func allContainersSpecifyQuotaResource(input Input, quotaResource string) bool {
	// Pod-count quotas are always fully determined by the replica math.
	if quotaResource == "pods" || quotaResource == "count/pods" {
		return true
	}
	if strings.TrimSpace(podLevelQuotaValue(input, quotaResource)) != "" {
		return true
	}

	containers := input.ContainerResources
	if len(containers) == 0 {
		return false
	}
	for _, container := range containers {
		value, known := containerQuotaValue(container, quotaResource)
		if !known {
			// A quota kind this plan does not model cannot be reported as
			// under-specified.
			return true
		}
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

// podLevelQuotaValue returns the Pod-level (spec.resources) value backing a
// quota resource, or "" when the quota is not one of the modelled kinds.
func podLevelQuotaValue(input Input, quotaResource string) string {
	switch quotaResource {
	case "cpu", "requests.cpu":
		return input.PodLevelRequestCPU
	case "memory", "requests.memory":
		return input.PodLevelRequestMemory
	case "limits.cpu":
		return input.PodLevelLimitCPU
	case "limits.memory":
		return input.PodLevelLimitMemory
	default:
		return ""
	}
}

// containerQuotaValue returns a container's value for a quota resource. The
// second result is false for quota kinds this plan does not model.
func containerQuotaValue(container CapacityContainerResources, quotaResource string) (string, bool) {
	switch quotaResource {
	case "cpu", "requests.cpu":
		return container.CPU, true
	case "memory", "requests.memory":
		return container.Memory, true
	case "limits.cpu":
		return container.LimitCPU, true
	case "limits.memory":
		return container.LimitMemory, true
	default:
		return "", false
	}
}

func quotaResourceRequirement(
	name string,
	requiredCPU, requiredMemory, requiredLimitCPU, requiredLimitMemory, requiredPods resource.Quantity,
) (display string, required resource.Quantity, checkID CapacityCheckID, ok bool) {
	switch name {
	case "cpu", "requests.cpu":
		return "CPU", requiredCPU, CapacityCheckQuotaCPU, true
	case "memory", "requests.memory":
		return "memory", requiredMemory, CapacityCheckQuotaMemory, true
	case "pods", "count/pods":
		return "pods", requiredPods, CapacityCheckQuotaPods, true
	case "limits.cpu":
		return "CPU limits", requiredLimitCPU, CapacityCheckQuotaLimitCPU, true
	case "limits.memory":
		return "memory limits", requiredLimitMemory, CapacityCheckQuotaLimitMemory, true
	default:
		return "", resource.Quantity{}, "", false
	}
}

func checkLimitRanges(
	limitRanges []LimitRangeConstraint,
	containers []CapacityContainerResources,
	perPodCPU, perPodMemory, perPodLimitCPU, perPodLimitMemory resource.Quantity,
) []CapacityCheckResult {
	if len(limitRanges) == 0 {
		return []CapacityCheckResult{
			newCapacityCheckResult(CapacityCheckLimitRange, CapacityCheckPass, "no LimitRange constraints in namespace"),
		}
	}

	var results []CapacityCheckResult
	violated := false

	for _, c := range containers {
		for _, lr := range limitRanges {
			if lr.Type != "Container" {
				continue
			}
			newResults := checkLimitRangeResource(c, lr)
			if len(newResults) > 0 {
				violated = true
				results = append(results, newResults...)
			}
		}
	}
	for _, lr := range limitRanges {
		if lr.Type != "Pod" {
			continue
		}
		var request, limit resource.Quantity
		var display string
		switch lr.Resource {
		case "cpu":
			request = perPodCPU
			limit = perPodLimitCPU
			display = "CPU"
		case "memory":
			request = perPodMemory
			limit = perPodLimitMemory
			display = "memory"
		default:
			continue
		}
		newResults := limitRangeBoundsViolations(
			request,
			limit,
			"pod",
			request.String(),
			limit.String(),
			display,
			lr,
			!limit.IsZero(),
		)
		if len(newResults) > 0 {
			violated = true
			results = append(results, newResults...)
		}
	}

	if !violated {
		results = append(results, newCapacityCheckResult(
			CapacityCheckLimitRange,
			CapacityCheckPass,
			"all pod and container requests within LimitRange bounds",
		))
	}

	return results
}

// checkLimitRangeResource evaluates a single LimitRange constraint against a
// container's request and limit, returning any violations.
func checkLimitRangeResource(c CapacityContainerResources, lr LimitRangeConstraint) []CapacityCheckResult {
	if lr.Type != "Container" {
		return nil
	}
	requestValue, limitValue, display := limitRangeResourceValues(c, lr)
	if display == "" {
		return nil
	}

	return limitRangeBoundsViolations(
		parseQuantityOrZero(requestValue),
		parseQuantityOrZero(limitValue),
		fmt.Sprintf("container %q", c.Name),
		requestValue,
		limitValue,
		display,
		lr,
		strings.TrimSpace(limitValue) != "",
	)
}

// limitRangeResourceValues returns request/limit values and the display label
// ("CPU"/"memory") for a container and LimitRange pair.
func limitRangeResourceValues(c CapacityContainerResources, lr LimitRangeConstraint) (request, limit, display string) {
	switch lr.Resource {
	case "cpu":
		return c.CPU, c.LimitCPU, "CPU"
	case "memory":
		return c.Memory, c.LimitMemory, "memory"
	}
	return "", "", ""
}

// limitRangeBoundsViolations applies Kubernetes admission semantics: Min
// constrains requests, while Max constrains limits.
func limitRangeBoundsViolations(
	request, limit resource.Quantity,
	subject, requestValue, limitValue, display string,
	lr LimitRangeConstraint,
	limitObserved bool,
) []CapacityCheckResult {
	var violations []CapacityCheckResult
	if lr.Max != "" {
		maxQty := parseQuantityOrZero(lr.Max)
		if !limitObserved {
			violations = append(violations, newCapacityCheckResult(
				CapacityCheckLimitRangeMaximum,
				CapacityCheckFail,
				fmt.Sprintf("%s %s limit is not specified but LimitRange %q sets max %s", subject, display, lr.Name, lr.Max),
			))
		} else if limit.Cmp(maxQty) > 0 {
			violations = append(violations, newCapacityCheckResult(
				CapacityCheckLimitRangeMaximum,
				CapacityCheckFail,
				fmt.Sprintf("%s %s limit %s exceeds LimitRange %q max %s", subject, display, limitValue, lr.Name, lr.Max),
			))
		}
		if request.Cmp(maxQty) > 0 {
			violations = append(violations, newCapacityCheckResult(
				CapacityCheckLimitRangeMaximum,
				CapacityCheckFail,
				fmt.Sprintf("%s %s request %s exceeds LimitRange %q max %s", subject, display, requestValue, lr.Name, lr.Max),
			))
		}
	}
	if lr.Min != "" {
		minQty := parseQuantityOrZero(lr.Min)
		if request.Cmp(minQty) < 0 {
			if strings.TrimSpace(requestValue) == "" {
				requestValue = "0 (not specified)"
			}
			violations = append(violations, newCapacityCheckResult(
				CapacityCheckLimitRangeMinimum,
				CapacityCheckFail,
				fmt.Sprintf("%s %s request %s below LimitRange %q min %s", subject, display, requestValue, lr.Name, lr.Min),
			))
		}
	}
	if lr.MaxLimitRequestRatio != "" {
		switch {
		case !limitObserved:
			violations = append(violations, newCapacityCheckResult(
				CapacityCheckLimitRangeRatio,
				CapacityCheckFail,
				fmt.Sprintf("%s %s limit is required by LimitRange %q max limit/request ratio", subject, display, lr.Name),
			))
		case request.IsZero():
			violations = append(violations, newCapacityCheckResult(
				CapacityCheckLimitRangeRatio,
				CapacityCheckFail,
				fmt.Sprintf("%s %s request must be non-zero for LimitRange %q max limit/request ratio", subject, display, lr.Name),
			))
		default:
			ratioQuantity := parseQuantityOrZero(lr.MaxLimitRequestRatio)
			ratio := ratioQuantity.AsApproximateFloat64()
			actual := limit.AsApproximateFloat64() / request.AsApproximateFloat64()
			if actual > ratio {
				violations = append(violations, newCapacityCheckResult(
					CapacityCheckLimitRangeRatio,
					CapacityCheckFail,
					fmt.Sprintf(
						"%s %s limit/request ratio %.3g exceeds LimitRange %q max %s",
						subject,
						display,
						actual,
						lr.Name,
						lr.MaxLimitRequestRatio,
					),
				))
			}
		}
	}
	return violations
}
