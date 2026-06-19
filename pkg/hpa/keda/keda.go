// Package keda analyzes the relationship between an HPA and the KEDA
// ScaledObject that owns it. It is a self-contained leaf domain: it depends
// only on the autoscaling/v2 API types and produces interpretation lines plus
// a typed Analysis summary. The cmd/ layer reaches it through the pkg/hpa
// re-export facade (hpaanalysis.KEDAAnalysis, hpaanalysis.AnalyzeKEDA) so
// existing import paths keep working.
package keda

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DefaultMinReplicas mirrors pkg/hpa.DefaultMinReplicas. It is duplicated here
// rather than imported because keda is a leaf sub-package that must not reach
// back into the analysis core (which would create an import cycle). Keep the
// two values in sync.
const DefaultMinReplicas int32 = 1

// Analysis holds KEDA-specific information attached to an HPA Analysis.
// Populated only when --keda is enabled and the HPA is KEDA-managed.
// This is the canonical definition; pkg/hpa re-exports it as
// hpaanalysis.KEDAAnalysis via a type alias.
type Analysis struct {
	ScaledObjectName string           `json:"scaledObjectName" yaml:"scaledObjectName"`
	Triggers         []TriggerSummary `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	PollingInterval  *int32           `json:"pollingInterval,omitempty" yaml:"pollingInterval,omitempty"`
	CooldownPeriod   *int32           `json:"cooldownPeriod,omitempty" yaml:"cooldownPeriod,omitempty"`
	MinReplicaCount  *int32           `json:"minReplicaCount,omitempty" yaml:"minReplicaCount,omitempty"`
	MaxReplicaCount  *int32           `json:"maxReplicaCount,omitempty" yaml:"maxReplicaCount,omitempty"`
	Lines            []string         `json:"lines,omitempty" yaml:"lines,omitempty"`
	Fallback         *FallbackInfo    `json:"fallback,omitempty" yaml:"fallback,omitempty"`
}

// TriggerSummary is a display-oriented summary of a KEDA trigger.
type TriggerSummary struct {
	Type         string `json:"type" yaml:"type"`
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
	Status       string `json:"status,omitempty" yaml:"status,omitempty"`
	Message      string `json:"message,omitempty" yaml:"message,omitempty"`
	MetricName   string `json:"metricName,omitempty" yaml:"metricName,omitempty"`
	Threshold    string `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	CurrentValue string `json:"currentValue,omitempty" yaml:"currentValue,omitempty"`
	AuthRef      string `json:"authRef,omitempty" yaml:"authRef,omitempty"`
}

// FallbackInfo holds fallback information for display.
type FallbackInfo struct {
	FailureThreshold int32 `json:"failureThreshold" yaml:"failureThreshold"`
	Replicas         int32 `json:"replicas" yaml:"replicas"`
}

// Analyze produces interpretation lines that cross-reference an HPA with its
// KEDA ScaledObject.
func Analyze(hpa *autoscalingv2.HorizontalPodAutoscaler, k *Analysis) []string {
	if hpa == nil || k == nil {
		return nil
	}

	var lines []string

	lines = append(lines, fmt.Sprintf("[observed] HPA is owned by KEDA ScaledObject %q in the same namespace.", k.ScaledObjectName))

	// Trigger cross-reference with HPA external metrics.
	lines = append(lines, analyzeTriggers(hpa, k)...)

	// Polling interval vs HPA evaluation.
	lines = append(lines, analyzePolling(hpa, k)...)

	// KEDA min/max vs HPA min/max.
	lines = append(lines, analyzeReplicaBounds(hpa, k)...)

	// Trigger status analysis (inactive triggers, fallback).
	lines = append(lines, analyzeTriggerStatus(k)...)

	// ScaledObject conditions from pre-populated lines.
	lines = append(lines, k.Lines...)

	return lines
}

func analyzeTriggers(hpa *autoscalingv2.HorizontalPodAutoscaler, k *Analysis) []string {
	if len(k.Triggers) == 0 {
		return []string{"[estimated] ScaledObject has no triggers defined; verify the ScaledObject spec."}
	}

	var lines []string
	names := make([]string, 0, len(k.Triggers))

	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ExternalMetricSourceType && spec.External != nil {
			matched := false
			for _, t := range k.Triggers {
				if strings.Contains(spec.External.Metric.Name, t.Name) || strings.Contains(spec.External.Metric.Name, strings.ToLower(t.Type)) {
					matched = true
					triggerDesc := fmt.Sprintf("KEDA trigger %q (type %s)", t.Name, t.Type)
					if t.Threshold != "" {
						triggerDesc += fmt.Sprintf(" threshold=%s", t.Threshold)
					}
					if t.CurrentValue != "" {
						triggerDesc += fmt.Sprintf(" current=%s", t.CurrentValue)
					}
					lines = append(lines, fmt.Sprintf("[observed] %s produces external metric %q which matches HPA spec.metrics entry.", triggerDesc, spec.External.Metric.Name))
					break
				}
			}
			if !matched {
				lines = append(lines, fmt.Sprintf("[estimated] HPA external metric %q has no matching KEDA trigger; the metric name may not align with the scaler output.", spec.External.Metric.Name))
			}
		}
	}

	for _, t := range k.Triggers {
		names = append(names, t.Name)
	}
	lines = append(lines, fmt.Sprintf("[observed] ScaledObject defines %d trigger(s): %s.", len(k.Triggers), strings.Join(names, ", ")))

	return lines
}

func analyzePolling(hpa *autoscalingv2.HorizontalPodAutoscaler, k *Analysis) []string {
	if k.PollingInterval == nil || *k.PollingInterval <= 0 {
		return nil
	}
	interval := *k.PollingInterval

	var lines []string

	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil {
		if window := hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds; window != nil && *window > interval {
			lines = append(lines,
				fmt.Sprintf("[estimated] KEDA polling interval is %ds but HPA scaleDown stabilization is %ds; the stabilization window delays reaction to KEDA metric updates.", interval, *window),
			)
		}
	}

	return lines
}

func analyzeReplicaBounds(hpa *autoscalingv2.HorizontalPodAutoscaler, k *Analysis) []string {
	var lines []string
	minReplicas := DefaultMinReplicas
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	if k.MinReplicaCount != nil && *k.MinReplicaCount != minReplicas {
		lines = append(lines, fmt.Sprintf("[observed] KEDA minReplicaCount=%d differs from HPA minReplicas=%d; KEDA reconciliation may override manual HPA changes.", *k.MinReplicaCount, minReplicas))
	}
	if k.MaxReplicaCount != nil && *k.MaxReplicaCount != hpa.Spec.MaxReplicas {
		lines = append(lines, fmt.Sprintf("[observed] KEDA maxReplicaCount=%d differs from HPA maxReplicas=%d; KEDA reconciliation may override manual HPA changes.", *k.MaxReplicaCount, hpa.Spec.MaxReplicas))
	}

	return lines
}

// analyzeTriggerStatus checks for inactive triggers and notes fallback configuration.
func analyzeTriggerStatus(k *Analysis) []string {
	if k == nil {
		return nil
	}
	var lines []string

	// Check for inactive triggers.
	for _, t := range k.Triggers {
		if t.Status == "Inactive" {
			lines = append(lines, fmt.Sprintf("[observed] KEDA trigger %q (type %s) is Inactive; the scaler may not be receiving events or the external source may be unavailable.", t.Name, t.Type))
		}
	}

	// Note fallback configuration.
	if k.Fallback != nil {
		lines = append(lines, fmt.Sprintf("[observed] ScaledObject has fallback configured: failureThreshold=%d, replicas=%d. KEDA will fall back to %d replicas if the scaler fails %d consecutive checks.", k.Fallback.FailureThreshold, k.Fallback.Replicas, k.Fallback.Replicas, k.Fallback.FailureThreshold))
	}

	return lines
}
