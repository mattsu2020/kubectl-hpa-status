// Package containeradvisor evaluates whether an HPA should use
// ContainerResource metrics instead of pod-level Resource metrics for
// multi-container workloads. It is a pure, dependency-free analysis package;
// the container_advisor_text.go renderer stays in pkg/hpa because it shares
// the labels machinery.
package containeradvisor

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/confidence"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// Input aggregates signals for ContainerResource advisor analysis.
type Input struct {
	// ContainerCount is the number of containers in the target pod template.
	ContainerCount int
	// ContainerNames lists the container names from the pod template.
	ContainerNames []string
	// UsesResourceMetric is true when HPA has at least one Resource metric type.
	UsesResourceMetric bool
	// UsesContainerResourceMetric is true when HPA already uses ContainerResource.
	UsesContainerResourceMetric bool
	// ResourceMetrics lists the Resource-type metric specs.
	ResourceMetrics []autoscalingv2.MetricSpec
}

// Result holds the ContainerResource advisor analysis.
type Result struct {
	// Finding describes the observation.
	Finding string `json:"finding" yaml:"finding"`
	// Risk describes the operational risk of the current configuration.
	Risk string `json:"risk,omitempty" yaml:"risk,omitempty"`
	// SuggestedMetric shows a suggested ContainerResource metric YAML fragment.
	SuggestedMetric string `json:"suggestedMetric,omitempty" yaml:"suggestedMetric,omitempty"`
	// Confidence indicates the confidence level of the recommendation.
	Confidence confidence.Confidence `json:"confidence" yaml:"confidence"`
	// NextAction suggests what the operator should do next.
	NextAction string `json:"nextAction,omitempty" yaml:"nextAction,omitempty"`
	// ContainerUsageHints provides per-container usage hints when metrics are available.
	ContainerUsageHints []UsageHint `json:"containerUsageHints,omitempty" yaml:"containerUsageHints,omitempty"`
}

// UsageHint provides per-container resource usage estimation.
type UsageHint struct {
	// Container is the container name.
	Container string `json:"container" yaml:"container"`
	// CPUPercent is the estimated CPU usage percentage (0-100), -1 if unknown.
	CPUPercent int `json:"cpuPercent,omitempty" yaml:"cpuPercent,omitempty"`
	// MemoryPercent is the estimated memory usage percentage (0-100), -1 if unknown.
	MemoryPercent int `json:"memoryPercent,omitempty" yaml:"memoryPercent,omitempty"`
	// Dominant indicates this container appears to be the scaling-critical container.
	Dominant bool `json:"dominant,omitempty" yaml:"dominant,omitempty"`
}

// Analyze evaluates whether an HPA should use ContainerResource
// metrics instead of pod-level Resource metrics for multi-container workloads.
// This is a pure function with no Kubernetes API dependencies.
func Analyze(hpa *autoscalingv2.HorizontalPodAutoscaler, input Input) *Result {
	if hpa == nil {
		return nil
	}

	// Only relevant for multi-container pods.
	if input.ContainerCount <= 1 {
		return nil
	}

	// Only relevant when HPA uses Resource metric type.
	if !input.UsesResourceMetric {
		return nil
	}

	// Already using ContainerResource — no action needed.
	if input.UsesContainerResourceMetric {
		return nil
	}

	// Build finding message.
	finding := fmt.Sprintf(
		"HPA uses pod-level Resource metrics, but target pods have %d containers (%v). "+
			"Pod-level utilization averages all containers, so a single hot container may be hidden.",
		input.ContainerCount, input.ContainerNames,
	)

	risk := "Pod-level CPU/memory averages app + sidecar containers. " +
		"If one container dominates resource usage, HPA scaling decisions may be delayed or inaccurate."

	// Build suggested metric using the first container name as a starting point.
	targetContainer := input.ContainerNames[0]
	if len(input.ContainerNames) > 1 {
		// Try to pick "app" or "main" if available, otherwise use the first.
		for _, name := range input.ContainerNames {
			if name == "app" || name == "main" || name == "application" {
				targetContainer = name
				break
			}
		}
	}

	suggested := fmt.Sprintf(`type: ContainerResource
containerResource:
  name: cpu
  container: %s
  target:
    type: Utilization
    averageUtilization: 60`, targetContainer)

	nextAction := "Review container names and add ContainerResource metric during a safe rollout. " +
		"Use 'kubectl get --raw /apis/metrics.k8s.io/v1beta1/namespaces/<ns>/pods' to check per-container usage."

	return &Result{
		Finding:         finding,
		Risk:            risk,
		SuggestedMetric: suggested,
		Confidence:      confidence.Medium,
		NextAction:      nextAction,
	}
}

// AnalyzeWithMetrics enriches the advisor result with per-container
// usage hints when PodMetrics data is available.
func AnalyzeWithMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler, input Input, containerMetrics []UsageHint) *Result {
	result := Analyze(hpa, input)
	if result == nil {
		return nil
	}

	if len(containerMetrics) > 0 {
		result.ContainerUsageHints = containerMetrics

		// Find dominant container.
		maxCPU := -1
		dominantIdx := -1
		for i, m := range containerMetrics {
			if m.CPUPercent > maxCPU {
				maxCPU = m.CPUPercent
				dominantIdx = i
			}
		}
		if dominantIdx >= 0 {
			result.ContainerUsageHints[dominantIdx].Dominant = true
			name := result.ContainerUsageHints[dominantIdx].Container
			result.Finding += fmt.Sprintf(" The %q container appears to be the scaling-critical container.", name)
			result.SuggestedMetric = fmt.Sprintf(`type: ContainerResource
containerResource:
  name: cpu
  container: %s
  target:
    type: Utilization
    averageUtilization: 60`, name)
			result.Confidence = confidence.High
		}
	}

	return result
}
