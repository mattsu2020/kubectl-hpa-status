// Package keda analyzes the relationship between an HPA and the KEDA
// ScaledObject that owns it. It is a self-contained leaf domain: it depends
// only on the autoscaling/v2 API types and produces interpretation lines plus
// a typed Analysis summary. The cmd/ and internal/ layers import this
// package directly (keda.Analyze).
package keda

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/confidence"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/model"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DefaultMinReplicas is the canonical shared default from pkg/hpa/model.
const DefaultMinReplicas = model.DefaultMinReplicas

// Analysis holds KEDA-specific information attached to an HPA Analysis.
// Populated only when --keda is enabled and the HPA is KEDA-managed.
// This is the canonical definition; the historical pkg/hpa
// hpaanalysis.KEDAAnalysis alias was removed in v3.0.0.
type Analysis struct {
	Health           map[string]MetricHealth `json:"health,omitempty" yaml:"health,omitempty"`
	ScaledObjectName string                  `json:"scaledObjectName" yaml:"scaledObjectName"`
	Triggers         []TriggerSummary        `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	PollingInterval  *int32                  `json:"pollingInterval,omitempty" yaml:"pollingInterval,omitempty"`
	CooldownPeriod   *int32                  `json:"cooldownPeriod,omitempty" yaml:"cooldownPeriod,omitempty"`
	MinReplicaCount  *int32                  `json:"minReplicaCount,omitempty" yaml:"minReplicaCount,omitempty"`
	MaxReplicaCount  *int32                  `json:"maxReplicaCount,omitempty" yaml:"maxReplicaCount,omitempty"`
	// IdleReplicaCount is the replica count KEDA scales the workload down to
	// when the triggers are idle (scale-to-zero fallback). Populated from
	// spec.idleReplicaCount; nil when unset.
	IdleReplicaCount *int32        `json:"idleReplicaCount,omitempty" yaml:"idleReplicaCount,omitempty"`
	Lines            []string      `json:"lines,omitempty" yaml:"lines,omitempty"`
	Fallback         *FallbackInfo `json:"fallback,omitempty" yaml:"fallback,omitempty"`
}

// MetricHealth separates collection health from trigger activity.
type MetricHealth struct {
	Status           string `json:"status,omitempty" yaml:"status,omitempty"`
	NumberOfFailures *int32 `json:"numberOfFailures,omitempty" yaml:"numberOfFailures,omitempty"`
}

// TriggerSummary is a display-oriented summary of a KEDA trigger.
type TriggerSummary struct {
	MetricType       string `json:"metricType,omitempty" yaml:"metricType,omitempty"`
	HealthStatus     string `json:"healthStatus,omitempty" yaml:"healthStatus,omitempty"`
	NumberOfFailures *int32 `json:"numberOfFailures,omitempty" yaml:"numberOfFailures,omitempty"`
	Type             string `json:"type" yaml:"type"`
	Name             string `json:"name,omitempty" yaml:"name,omitempty"`
	Status           string `json:"status,omitempty" yaml:"status,omitempty"`
	Message          string `json:"message,omitempty" yaml:"message,omitempty"`
	MetricName       string `json:"metricName,omitempty" yaml:"metricName,omitempty"`
	Threshold        string `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	CurrentValue     string `json:"currentValue,omitempty" yaml:"currentValue,omitempty"`
	AuthRef          string `json:"authRef,omitempty" yaml:"authRef,omitempty"`
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

	lines = append(lines, fmt.Sprintf(confidence.BadgeObserved+" HPA is owned by KEDA ScaledObject %q in the same namespace.", k.ScaledObjectName))

	// Trigger cross-reference with HPA external metrics.
	lines = append(lines, analyzeTriggers(hpa, k)...)

	// Polling interval vs HPA evaluation.
	lines = append(lines, analyzePolling(hpa, k)...)

	// KEDA min/max vs HPA min/max.
	lines = append(lines, analyzeReplicaBounds(hpa, k)...)

	// Metric health and fallback configuration.
	lines = append(lines, analyzeTriggerStatus(k)...)

	// ScaledObject conditions from pre-populated lines.
	lines = append(lines, k.Lines...)

	return lines
}

func analyzeTriggers(hpa *autoscalingv2.HorizontalPodAutoscaler, k *Analysis) []string {
	if len(k.Triggers) == 0 {
		return []string{confidence.BadgeEstimated + " ScaledObject has no triggers defined; verify the ScaledObject spec."}
	}
	var lines []string
	names := make([]string, 0, len(k.Triggers))
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ExternalMetricSourceType || spec.External == nil {
			continue
		}
		metricName := spec.External.Metric.Name
		index, observed := matchTriggerMetric(metricName, k.Triggers)
		if index < 0 {
			lines = append(lines, fmt.Sprintf(confidence.BadgeEstimated+" HPA external metric %q has no matching KEDA trigger; the metric name may not align with the scaler output.", metricName))
			continue
		}
		lines = append(lines, triggerMetricLine(k.Triggers[index], index, metricName, observed))
	}
	for i, t := range k.Triggers {
		names = append(names, triggerName(t, i))
	}
	lines = append(lines, fmt.Sprintf(confidence.BadgeObserved+" ScaledObject defines %d trigger(s): %s.", len(k.Triggers), strings.Join(names, ", ")))
	return lines
}

func triggerName(t TriggerSummary, index int) string {
	if t.Name != "" {
		return t.Name
	}
	return fmt.Sprintf("#%d (%s)", index, t.Type)
}

func matchTriggerMetric(metric string, triggers []TriggerSummary) (int, bool) {
	index := -1
	for i, t := range triggers {
		if t.MetricName != "" && t.MetricName == metric {
			if index >= 0 {
				return -1, false
			}
			index = i
		}
	}
	if index >= 0 {
		return index, true
	}
	for i, t := range triggers {
		if t.MetricName != "" {
			continue
		}
		nameMatches := t.Name != "" && strings.Contains(metric, t.Name)
		typeMatches := t.Type != "" && strings.Contains(metric, strings.ToLower(t.Type))
		if nameMatches || typeMatches {
			if index >= 0 {
				return -1, false
			}
			index = i
		}
	}
	return index, false
}

func triggerMetricLine(t TriggerSummary, index int, metric string, observed bool) string {
	label := t.Name
	if label == "" {
		label = fmt.Sprintf("#%d", index)
	}
	desc := fmt.Sprintf("KEDA trigger %q (type %s)", label, t.Type)
	if t.Threshold != "" {
		desc += fmt.Sprintf(" threshold=%s", t.Threshold)
	}
	if t.CurrentValue != "" {
		desc += fmt.Sprintf(" current=%s", t.CurrentValue)
	}
	badge := confidence.BadgeEstimated
	if observed {
		badge = confidence.BadgeObserved
	}
	return fmt.Sprintf(badge+" %s produces external metric %q which matches HPA spec.metrics entry.", desc, metric)
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
				fmt.Sprintf(confidence.BadgeEstimated+" KEDA polling interval is %ds but HPA scaleDown stabilization is %ds; the stabilization window delays reaction to KEDA metric updates.", interval, *window),
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

	effectiveMin := DefaultMinReplicas
	if k.MinReplicaCount != nil && *k.MinReplicaCount > 0 {
		effectiveMin = *k.MinReplicaCount
	}
	if k.MinReplicaCount != nil && effectiveMin != minReplicas {
		lines = append(lines, fmt.Sprintf(confidence.BadgeObserved+" KEDA minReplicaCount=%d differs from HPA minReplicas=%d; KEDA reconciliation may override manual HPA changes.", *k.MinReplicaCount, minReplicas))
	}
	if k.MaxReplicaCount != nil && *k.MaxReplicaCount != hpa.Spec.MaxReplicas {
		lines = append(lines, fmt.Sprintf(confidence.BadgeObserved+" KEDA maxReplicaCount=%d differs from HPA maxReplicas=%d; KEDA reconciliation may override manual HPA changes.", *k.MaxReplicaCount, hpa.Spec.MaxReplicas))
	}
	if k.IdleReplicaCount != nil {
		lines = append(lines, fmt.Sprintf(confidence.BadgeObserved+" KEDA idleReplicaCount=%d is set; KEDA will scale the workload down to this many replicas when all triggers are idle (scale-to-zero fallback).", *k.IdleReplicaCount))
	}

	return lines
}

// analyzeTriggerStatus checks scaler health and notes fallback configuration.
func analyzeTriggerStatus(k *Analysis) []string {
	if k == nil {
		return nil
	}
	var lines []string

	// Activity can be idle while metric collection is healthy.
	for _, t := range k.Triggers {
		if strings.EqualFold(t.HealthStatus, "Failing") {
			lines = append(lines, fmt.Sprintf(confidence.BadgeObserved+" KEDA trigger %q (type %s) has Failing metric health; the external source may be unavailable.", t.Name, t.Type))
		}
	}

	for _, name := range sortedHealthNames(k.Health) {
		metric := k.Health[name]
		if strings.EqualFold(metric.Status, "Failing") {
			lines = append(lines, fmt.Sprintf(confidence.BadgeObserved+" KEDA metric %q has Failing health.", name))
		}
	}

	// Note fallback configuration.
	if k.Fallback != nil {
		lines = append(lines, fmt.Sprintf(confidence.BadgeObserved+" ScaledObject has fallback configured: failureThreshold=%d, replicas=%d. KEDA will fall back to %d replicas if the scaler fails %d consecutive checks.", k.Fallback.FailureThreshold, k.Fallback.Replicas, k.Fallback.Replicas, k.Fallback.FailureThreshold))
	}

	return lines
}

func sortedHealthNames(health map[string]MetricHealth) []string {
	names := make([]string, 0, len(health))
	for name := range health {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
