package hpa

import (
	"math"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/resourceutil"
	"k8s.io/apimachinery/pkg/api/resource"
)

// parseQuantityOrZero is the calculation fallback after
// validateCapacityQuantityInputs has converted malformed values into explicit
// unknown observations. A malformed value must never silently produce a Safe
// recommendation.
func parseQuantityOrZero(s string) resource.Quantity {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return resource.Quantity{}
	}
	return q
}

func effectivePerPodResources(input CapacityPlanInput) (resource.Quantity, resource.Quantity) {
	cpu, memory := sumContainerResources(input.ContainerResources)
	if input.PodRequestCPU != "" {
		cpu = parseQuantityOrZero(input.PodRequestCPU)
	}
	if input.PodRequestMemory != "" {
		memory = parseQuantityOrZero(input.PodRequestMemory)
	}
	return cpu, memory
}

func effectivePerPodLimits(input CapacityPlanInput) (resource.Quantity, resource.Quantity) {
	var cpu, memory resource.Quantity
	for _, container := range input.ContainerResources {
		if container.LimitCPU != "" {
			cpu.Add(parseQuantityOrZero(container.LimitCPU))
		}
		if container.LimitMemory != "" {
			memory.Add(parseQuantityOrZero(container.LimitMemory))
		}
	}
	if input.PodLimitCPU != "" {
		cpu = parseQuantityOrZero(input.PodLimitCPU)
	}
	if input.PodLimitMemory != "" {
		memory = parseQuantityOrZero(input.PodLimitMemory)
	}
	return cpu, memory
}

// sumContainerResources sums CPU and memory requests across all containers
// into per-pod totals.
func sumContainerResources(containers []CapacityContainerResources) (resource.Quantity, resource.Quantity) {
	var totalCPU, totalMemory resource.Quantity
	for _, c := range containers {
		if c.CPU != "" && c.CPU != "0" {
			q := parseQuantityOrZero(c.CPU)
			totalCPU.Add(q)
		}
		if c.Memory != "" && c.Memory != "0" {
			q := parseQuantityOrZero(c.Memory)
			totalMemory.Add(q)
		}
	}
	return totalCPU, totalMemory
}

// computeSchedulableNow estimates how many additional pods can be scheduled
// with current node capacity. It subtracts resources used by already-running
// pods (ReadyPods * per-pod resources) from total allocatable, then divides
// the remainder by per-pod resources. Returns 0 if node capacity is unavailable
// or per-pod resources cannot be determined.
func computeSchedulableNow(nc *blocker.NodeCapacitySummary, perPodCPU, perPodMemory resource.Quantity, readyPods int32) int32 {
	if nc == nil || nc.TotalNodes == 0 {
		return 0
	}

	if len(nc.NodeHeadrooms) > 0 {
		var total int64
		for _, node := range nc.NodeHeadrooms {
			cpu := parseQuantityOrZero(node.AvailableCPU)
			memory := parseQuantityOrZero(node.AvailableMemory)
			nodeFit := int64(podsFitInResources(cpu, memory, perPodCPU, perPodMemory))
			if node.PodCapacityKnown && nodeFit > node.AvailablePods {
				nodeFit = node.AvailablePods
			}
			if nodeFit > 0 {
				total += nodeFit
			}
			if total >= math.MaxInt32 {
				return math.MaxInt32
			}
		}
		return int32(total)
	}

	var remainingCPU, remainingMem resource.Quantity
	if nc.AvailableCPU != "" || nc.AvailableMemory != "" {
		remainingCPU = parseQuantityOrZero(nc.AvailableCPU)
		remainingMem = parseQuantityOrZero(nc.AvailableMemory)
	} else {
		// Compatibility fallback for callers that only supply aggregate
		// allocatable capacity. Live collection always supplies Available*,
		// based on all scheduled Pods.
		remainingCPU = parseQuantityOrZero(nc.AllocCPU)
		remainingMem = parseQuantityOrZero(nc.AllocMemory)
		remainingCPU.Sub(resourceutil.Multiply(perPodCPU, int64(readyPods)))
		remainingMem.Sub(resourceutil.Multiply(perPodMemory, int64(readyPods)))
	}

	fit := podsFitInResources(remainingCPU, remainingMem, perPodCPU, perPodMemory)
	if nc.PodCapacityKnown && int64(fit) > nc.AvailablePods {
		if nc.AvailablePods <= 0 {
			return 0
		}
		return int32(min(nc.AvailablePods, int64(math.MaxInt32)))
	}
	return fit
}

func podsFitInResources(availableCPU, availableMemory, perPodCPU, perPodMemory resource.Quantity) int32 {
	if perPodCPU.IsZero() && perPodMemory.IsZero() {
		// CPU/memory impose no bound for a BestEffort Pod. The caller caps
		// this sentinel with allocatable Pod slots when that observation is
		// available.
		return math.MaxInt32
	}
	fits := func(count int64) bool {
		if !perPodCPU.IsZero() && availableCPU.Cmp(resourceutil.Multiply(perPodCPU, count)) < 0 {
			return false
		}
		return perPodMemory.IsZero() || availableMemory.Cmp(resourceutil.Multiply(perPodMemory, count)) >= 0
	}
	var low int64
	high := int64(math.MaxInt32) + 1
	for low+1 < high {
		mid := low + (high-low)/2
		if fits(mid) {
			low = mid
		} else {
			high = mid
		}
	}
	return int32(low)
}
