package hpa

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// AnalyzeKEDA produces interpretation lines that cross-reference an HPA with its KEDA ScaledObject.
func AnalyzeKEDA(hpa *autoscalingv2.HorizontalPodAutoscaler, keda *KEDAAnalysis) []string {
	if hpa == nil || keda == nil {
		return nil
	}

	var lines []string

	lines = append(lines, fmt.Sprintf("[confidence: high] HPA is owned by KEDA ScaledObject %q in the same namespace.", keda.ScaledObjectName))

	// Trigger cross-reference with HPA external metrics.
	lines = append(lines, analyzeKEDATriggers(hpa, keda)...)

	// Polling interval vs HPA evaluation.
	lines = append(lines, analyzeKEDAPolling(hpa, keda)...)

	// KEDA min/max vs HPA min/max.
	lines = append(lines, analyzeKEDAReplicaBounds(hpa, keda)...)

	// Trigger status analysis (inactive triggers, fallback).
	lines = append(lines, analyzeKEDATriggerStatus(keda)...)

	// ScaledObject conditions from pre-populated lines.
	lines = append(lines, keda.Lines...)

	return lines
}

func analyzeKEDATriggers(hpa *autoscalingv2.HorizontalPodAutoscaler, keda *KEDAAnalysis) []string {
	if len(keda.Triggers) == 0 {
		return []string{"[confidence: medium] ScaledObject has no triggers defined; verify the ScaledObject spec."}
	}

	var lines []string

	// Check for external metrics without matching triggers and report explicit mappings.
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ExternalMetricSourceType && spec.External != nil {
			matched := false
			for _, t := range keda.Triggers {
				if strings.Contains(spec.External.Metric.Name, t.Name) || strings.Contains(spec.External.Metric.Name, strings.ToLower(t.Type)) {
					matched = true
					triggerDesc := fmt.Sprintf("KEDA trigger %q (type %s)", t.Name, t.Type)
					if t.Threshold != "" {
						triggerDesc += fmt.Sprintf(" threshold=%s", t.Threshold)
					}
					if t.CurrentValue != "" {
						triggerDesc += fmt.Sprintf(" current=%s", t.CurrentValue)
					}
					lines = append(lines, fmt.Sprintf("[confidence: high] %s produces external metric %q which matches HPA spec.metrics entry.", triggerDesc, spec.External.Metric.Name))
					break
				}
			}
			if !matched {
				lines = append(lines, fmt.Sprintf("[confidence: medium] HPA external metric %q has no matching KEDA trigger; the metric name may not align with the scaler output.", spec.External.Metric.Name))
			}
		}
	}

	// Report trigger count summary.
	names := make([]string, 0, len(keda.Triggers))
	for _, t := range keda.Triggers {
		names = append(names, t.Type)
	}
	lines = append(lines, fmt.Sprintf("[confidence: high] ScaledObject defines %d trigger(s): %s.", len(keda.Triggers), strings.Join(names, ", ")))

	return lines
}

func analyzeKEDAPolling(hpa *autoscalingv2.HorizontalPodAutoscaler, keda *KEDAAnalysis) []string {
	if keda.PollingInterval == nil {
		return nil
	}
	interval := *keda.PollingInterval

	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil {
		if window := hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds; window != nil && *window > interval {
			return []string{
				fmt.Sprintf("[confidence: medium] KEDA polling interval is %ds but HPA scaleDown stabilization is %ds; the stabilization window delays reaction to KEDA metric updates.", interval, *window),
			}
		}
	}
	return nil
}

func analyzeKEDAReplicaBounds(hpa *autoscalingv2.HorizontalPodAutoscaler, keda *KEDAAnalysis) []string {
	var lines []string
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	if keda.MinReplicaCount != nil && *keda.MinReplicaCount != minReplicas {
		lines = append(lines, fmt.Sprintf("[confidence: high] KEDA minReplicaCount=%d differs from HPA minReplicas=%d; KEDA reconciliation may override manual HPA changes.", *keda.MinReplicaCount, minReplicas))
	}
	if keda.MaxReplicaCount != nil && *keda.MaxReplicaCount != hpa.Spec.MaxReplicas {
		lines = append(lines, fmt.Sprintf("[confidence: high] KEDA maxReplicaCount=%d differs from HPA maxReplicas=%d; KEDA reconciliation may override manual HPA changes.", *keda.MaxReplicaCount, hpa.Spec.MaxReplicas))
	}

	return lines
}

// analyzeKEDATriggerStatus checks for inactive triggers and notes fallback configuration.
func analyzeKEDATriggerStatus(keda *KEDAAnalysis) []string {
	if keda == nil {
		return nil
	}
	var lines []string

	// Check for inactive triggers.
	for _, t := range keda.Triggers {
		if t.Status == "Inactive" {
			lines = append(lines, fmt.Sprintf("[confidence: high] KEDA trigger %q (type %s) is Inactive; the scaler may not be receiving events or the external source may be unavailable.", t.Name, t.Type))
		}
	}

	// Note fallback configuration.
	if keda.Fallback != nil {
		lines = append(lines, fmt.Sprintf("[confidence: high] ScaledObject has fallback configured: failureThreshold=%d, replicas=%d. KEDA will fall back to %d replicas if the scaler fails %d consecutive checks.", keda.Fallback.FailureThreshold, keda.Fallback.Replicas, keda.Fallback.Replicas, keda.Fallback.FailureThreshold))
	}

	return lines
}
