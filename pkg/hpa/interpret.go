package hpa

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// interpretationCase is a shared representation used by both Interpret() and
// buildStructuredInterpretation() to avoid duplicating the condition branching logic.
type interpretationCase struct {
	reason     string     // machine-readable reason (e.g., "StaleStatus", "ScalingInactive")
	message    string     // human-readable message with confidence labels
	nextStep   string     // suggested next step (empty for Interpret output)
	severity   Severity   // typed severity level
	confidence Confidence // typed confidence level
}

// collectInterpretationCases walks the HPA status and returns a flat list of
// interpretation cases. Both Interpret and buildStructuredInterpretation
// consume this list to produce their respective output formats.
//
//nolint:gocyclo // Sequential condition inspection is inherent to HPA status interpretation; splitting would obscure the logic.
func collectInterpretationCases(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []interpretationCase {
	var cases []interpretationCase

	// Stale status (observedGeneration lag)
	if hpa.Status.ObservedGeneration != nil && *hpa.Status.ObservedGeneration < hpa.Generation {
		cases = append(cases, interpretationCase{
			reason:     "StaleStatus",
			message:    fmt.Sprintf("[confidence: high] Warning: status.observedGeneration=%d is behind metadata.generation=%d; the status may not reflect the latest spec.", *hpa.Status.ObservedGeneration, hpa.Generation),
			nextStep:   "Wait for HPA controller to process latest spec",
			severity:   SeverityWarning,
			confidence: ConfidenceHigh,
		})
	}

	// ScalingActive not True → early return
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		cases = append(cases,
			interpretationCase{
				reason:     "ScalingInactive",
				message:    fmt.Sprintf("[confidence: high] ScalingActive is %s: %s - %s", condition.Status, condition.Reason, condition.Message),
				nextStep:   "Check metrics-server or custom metrics adapters",
				severity:   SeverityError,
				confidence: ConfidenceHigh,
			},
			interpretationCase{
				reason:     "ScalingInactive",
				message:    "[confidence: high] The HPA is not reporting a reliable scale direction while metric evaluation is inactive.",
				nextStep:   "Restore metric availability before tuning HPA parameters",
				severity:   SeverityError,
				confidence: ConfidenceHigh,
			},
			interpretationCase{
				reason:     "ScalingInactive",
				message:    "[confidence: high] This plugin avoids treating desiredReplicas=0 as a scale-down recommendation in this state.",
				nextStep:   "Do not rely on desiredReplicas=0 as a scale-down signal",
				severity:   SeverityError,
				confidence: ConfidenceHigh,
			},
		)
		return cases
	}

	// AbleToScale
	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Status != corev1.ConditionTrue {
		cases = append(cases, interpretationCase{
			reason:     "UnableToScale",
			message:    fmt.Sprintf("[confidence: high] AbleToScale is %s: %s - %s", condition.Status, condition.Reason, condition.Message),
			nextStep:   "Check scaleTargetRef, RBAC, and scale subresource",
			severity:   SeverityError,
			confidence: ConfidenceHigh,
		})
	} else if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		if remaining := estimateStabilizationRemaining(hpa); remaining != nil && *remaining > 0 {
			message := fmt.Sprintf("[confidence: medium] Scale down appears stabilized: %s (estimated ~%d seconds remaining before scale-down is allowed).", condition.Message, *remaining)
			nextStep := fmt.Sprintf("Scale-down stabilized; estimated ~%d seconds remaining", *remaining)
			if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
				message += " Note: no spec.behavior.scaleDown is set; the controller-manager default (usually 300s) is used and may differ from this estimate."
			}
			cases = append(cases, interpretationCase{
				reason:     "ScaleDownStabilized",
				message:    message,
				nextStep:   nextStep,
				severity:   SeverityInfo,
				confidence: ConfidenceMedium,
			})
		} else {
			cases = append(cases, interpretationCase{
				reason:     "ScaleDownStabilized",
				message:    fmt.Sprintf("[confidence: medium] Scale down appears stabilized: %s", condition.Message),
				severity:   SeverityInfo,
				confidence: ConfidenceMedium,
			})
		}
	}

	// ScalingLimited
	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			cases = append(cases, interpretationCase{
				reason:     "LimitedByMaxReplicas",
				message:    "[confidence: high] ScalingLimited reports that the visible desired replica count is constrained by maxReplicas.",
				nextStep:   "Raise maxReplicas or reduce load/target utilization",
				severity:   SeverityWarning,
				confidence: ConfidenceHigh,
			})
		case minReplicas:
			cases = append(cases, interpretationCase{
				reason:     "LimitedByMinReplicas",
				message:    "[confidence: high] ScalingLimited reports that the visible desired replica count is constrained by minReplicas.",
				nextStep:   "Lower minReplicas if scale-down below this point is expected",
				severity:   SeverityWarning,
				confidence: ConfidenceHigh,
			})
		default:
			cases = append(cases, interpretationCase{
				reason:     "ScalingLimited",
				message:    "[confidence: high] The recommendation is reported as limited.",
				severity:   SeverityWarning,
				confidence: ConfidenceHigh,
			})
		}
	}

	// Desired vs current comparison
	switch {
	case hpa.Status.DesiredReplicas > hpa.Status.CurrentReplicas:
		cases = append(cases, interpretationCase{
			reason:     "ScaleUpRecommended",
			message:    "[confidence: high] desiredReplicas is greater than currentReplicas, so the HPA is recommending scale up.",
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
	case hpa.Status.DesiredReplicas < hpa.Status.CurrentReplicas:
		cases = append(cases, interpretationCase{
			reason:     "ScaleDownRecommended",
			message:    "[confidence: high] desiredReplicas is less than currentReplicas, so the HPA is recommending scale down.",
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
	default:
		cases = append(cases, interpretationCase{
			reason:     "NoScaleVisible",
			message:    "[confidence: high] desiredReplicas equals currentReplicas, so no immediate replica change is visible from status.",
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
		// Tolerance detection
		if hpa.Status.DesiredReplicas != hpa.Spec.MaxReplicas && hpa.Status.DesiredReplicas != minReplicas {
			if metric, ok := MetricOutsideTarget(hpa); ok {
				deviation := metric.Ratio - 1.0
				if deviation < 0 {
					deviation = -deviation
				}
				if deviation < 0.1 {
					cases = append(cases, interpretationCase{
						reason:     "ToleranceNoScale",
						message:    fmt.Sprintf("[tolerance-confirmed] [confidence: high] %s metric ratio is %.3f (within ±10%% of target); the Kubernetes default tolerance band of 0.1 (10%%) explains why replicas are unchanged despite %s being %.1f%% %s target.", metric.Name, metric.Ratio, metric.Name, (metric.Ratio-1)*100, metric.Note),
						severity:   SeverityInfo,
						confidence: ConfidenceHigh,
					})
				} else {
					cases = append(cases,
						interpretationCase{
							reason:     "ToleranceNoScale",
							message:    fmt.Sprintf("[confidence: medium] %s metric ratio is approximately %.3f, which is close to the target.", metric.Name, metric.Ratio),
							severity:   SeverityInfo,
							confidence: ConfidenceMedium,
						},
						interpretationCase{
							reason:     "ToleranceNoScale",
							message:    "[confidence: medium] This is consistent with tolerance-based no-scale. Kubernetes commonly uses a tolerance band around the target, but HPA status does not expose tolerance as an explicit reason.",
							severity:   SeverityInfo,
							confidence: ConfidenceMedium,
						},
					)
				}
				cases = append(cases, interpretationCase{
					reason:     "ToleranceNoScale",
					message:    "[confidence: high] The plugin avoids claiming the exact internal reason because rounding, stabilization, or conservative metric handling may also affect the final result.",
					severity:   SeverityInfo,
					confidence: ConfidenceHigh,
				})
			}
		}
	}

	// Multi-metric analysis
	if hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas && len(hpa.Status.CurrentMetrics) > 1 {
		cases = append(cases, interpretationCase{
			reason:     "MaxReplicasWinnerHidden",
			message:    "[confidence: high] desiredReplicas == maxReplicas; the winning metric cannot be reliably determined because the replica cap may hide the true metric winner.",
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
	} else if guess, ok := MostInfluentialMetric(hpa); ok && len(hpa.Status.CurrentMetrics) > 1 {
		cases = append(cases,
			interpretationCase{
				reason:     "MetricImpactEstimate",
				message:    fmt.Sprintf("[confidence: medium] Among visible metrics, %s has the largest distance from target (ratio %.3f).", guess.Name, guess.Ratio),
				severity:   SeverityInfo,
				confidence: ConfidenceMedium,
			},
			interpretationCase{
				reason:     "MetricImpactEstimate",
				message:    "[confidence: high] This is only an impact estimate; the API does not expose per-metric replica recommendations or the final metric winner.",
				severity:   SeverityInfo,
				confidence: ConfidenceHigh,
			},
		)
	} else if len(hpa.Status.CurrentMetrics) > 1 {
		cases = append(cases,
			interpretationCase{
				reason:     "MultiMetricNoWinner",
				message:    "[confidence: high] Multiple current metrics are reported, but the API does not expose per-metric replica recommendations or which metric would have selected the recommendation before replica limits were applied.",
				severity:   SeverityInfo,
				confidence: ConfidenceHigh,
			},
			interpretationCase{
				reason:     "MultiMetricNoWinner",
				message:    "[confidence: high] Events and human-readable messages can hint at the contributing metric, but they are not a stable decision record.",
				severity:   SeverityInfo,
				confidence: ConfidenceHigh,
			},
		)
	}

	// Metric disagreement detection: when metrics pull in opposite directions.
	if len(hpa.Status.CurrentMetrics) > 1 {
		var scaleUp, scaleDown []string
		for _, metric := range hpa.Status.CurrentMetrics {
			_, ratio := metricImpactRatio(hpa, metric)
			if ratio == nil {
				continue
			}
			name := metricDisplayName(metric)
			if *ratio > 1.0 {
				scaleUp = append(scaleUp, name)
			} else if *ratio < 1.0 {
				scaleDown = append(scaleDown, name)
			}
		}
		if len(scaleUp) > 0 && len(scaleDown) > 0 {
			cases = append(cases, interpretationCase{
				reason:     "MetricDisagreement",
				message:    fmt.Sprintf("[confidence: medium] Metric disagreement detected: %s want scale-up (ratio > 1.0) while %s want scale-down (ratio < 1.0). The HPA controller will use its selectPolicy to resolve this, but consider whether the metric targets are well-tuned.", strings.Join(scaleUp, ", "), strings.Join(scaleDown, ", ")),
				severity:   SeverityWarning,
				confidence: ConfidenceMedium,
			})
		}
	}

	// Scale-to-zero interpretation
	if minReplicas == 0 {
		if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas == 0 {
			cases = append(cases, interpretationCase{
				reason:     "ScaleToZero",
				message:    "[confidence: high] Scale-to-zero is enabled (minReplicas=0) and the workload is currently at zero replicas. The next scale-up requires a cold start which may introduce additional latency.",
				nextStep:   "Next scale-up requires a cold start which may introduce additional latency",
				severity:   SeverityInfo,
				confidence: ConfidenceHigh,
			})
		} else if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas > 0 {
			cases = append(cases, interpretationCase{
				reason:     "ScaleToZero",
				message:    "[confidence: high] Scale-to-zero is enabled (minReplicas=0) and the HPA wants to scale to zero. Note: scaling from 0 back to 1 requires a cold start.",
				nextStep:   "Scaling from 0 back to 1 requires a cold start",
				severity:   SeverityInfo,
				confidence: ConfidenceHigh,
			})
		}
	}

	return cases
}

// DiagnosticEntry is a single collected diagnostic event. Both text and
// structured output are derived from this unified representation.
type DiagnosticEntry struct {
	Reason     string
	Message    string
	NextStep   string
	Severity   Severity
	Confidence Confidence
}

// CollectDiagnostics gathers all diagnostic entries for an HPA in a single
// pass. Core interpretation cases, external/object metric diagnostics, KEDA
// diagnostics, and the limitation disclaimer are collected once and returned
// as a flat slice. Both Interpret() and buildStructuredInterpretation()
// consume this slice to produce their respective output formats, eliminating
// the risk of divergence between text and JSON/YAML output.
func CollectDiagnostics(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []DiagnosticEntry {
	var entries []DiagnosticEntry

	// Phase 1: Core interpretation cases from condition analysis.
	for _, c := range collectInterpretationCases(hpa, minReplicas) {
		entries = append(entries, DiagnosticEntry{
			Reason:     c.reason,
			Message:    c.message,
			NextStep:   c.nextStep,
			Severity:   c.severity,
			Confidence: c.confidence,
		})
	}

	// Phase 2: Metric-specific diagnostics.
	entries = append(entries, ExternalMetricDiagnostics(hpa)...)
	entries = append(entries, ObjectMetricDiagnostics(hpa)...)

	// Phase 3: KEDA diagnostics.
	entries = append(entries, KEDADiagnostics(hpa)...)

	// Phase 4: Limitation disclaimer.
	entries = append(entries, DiagnosticEntry{
		Reason:     "Limitation",
		Message:    limitation,
		Severity:   SeverityInfo,
		Confidence: ConfidenceHigh,
	})

	return entries
}

// Interpret generates detailed interpretation lines with confidence labels.
func Interpret(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []string {
	entries := CollectDiagnostics(hpa, minReplicas)
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, e.Message)
	}
	return lines
}

// buildStructuredInterpretation converts collected diagnostics into
// machine-readable StructuredMessage entries for JSON/YAML output.
func buildStructuredInterpretation(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []StructuredMessage {
	entries := CollectDiagnostics(hpa, minReplicas)
	msgs := make([]StructuredMessage, 0, len(entries))
	for _, e := range entries {
		msgs = append(msgs, StructuredMessage{
			Reason:     e.Reason,
			Message:    e.Message,
			NextStep:   e.NextStep,
			Severity:   e.Severity,
			Confidence: e.Confidence,
		})
	}
	return msgs
}

// ExternalMetricDiagnostics generates diagnostic entries for external metric issues.
func ExternalMetricDiagnostics(hpa *autoscalingv2.HorizontalPodAutoscaler) []DiagnosticEntry {
	var entries []DiagnosticEntry
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ExternalMetricSourceType || spec.External == nil {
			continue
		}
		if !hasCurrentExternalMetric(hpa, spec.External.Metric.Name, spec.External.Metric.Selector) {
			entries = append(entries, DiagnosticEntry{
				Reason:     "ExternalMetricDiagnostic",
				Message:    fmt.Sprintf("[confidence: high] External metric %q%s is configured but no matching current metric status is reported; check the external metrics adapter, selector, and metric freshness.", spec.External.Metric.Name, selectorSuffix(spec.External.Metric.Selector)),
				Severity:   SeverityWarning,
				Confidence: ConfidenceHigh,
			})
			continue
		}
		if metric, ok := currentExternalMetric(hpa, spec.External.Metric.Name, spec.External.Metric.Selector); ok {
			formatted := FormatMetricStatus(hpa, metric)
			if formatted.Ratio != nil {
				entries = append(entries, DiagnosticEntry{
					Reason:     "ExternalMetricDiagnostic",
					Message:    fmt.Sprintf("[confidence: medium] External metric %q%s is %.3fx its target; stale or delayed adapter data can make HPA decisions lag behind workload demand.", spec.External.Metric.Name, selectorSuffix(spec.External.Metric.Selector), *formatted.Ratio),
					Severity:   SeverityInfo,
					Confidence: ConfidenceMedium,
				})
			}
		}
	}
	return entries
}

// ObjectMetricDiagnostics generates diagnostic entries for object metric issues.
func ObjectMetricDiagnostics(hpa *autoscalingv2.HorizontalPodAutoscaler) []DiagnosticEntry {
	var entries []DiagnosticEntry
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ObjectMetricSourceType || spec.Object == nil {
			continue
		}
		describedObject := autoscalingv2.CrossVersionObjectReference{
			Kind: spec.Object.DescribedObject.Kind,
			Name: spec.Object.DescribedObject.Name,
		}
		if metric, ok := currentObjectMetric(hpa, spec.Object.Metric.Name, spec.Object.Metric.Selector, describedObject); ok {
			formatted := FormatMetricStatus(hpa, metric)
			object := fmt.Sprintf("%s/%s", spec.Object.DescribedObject.Kind, spec.Object.DescribedObject.Name)
			if formatted.Ratio != nil {
				entries = append(entries, DiagnosticEntry{
					Reason:     "ObjectMetricDiagnostic",
					Message:    fmt.Sprintf("[confidence: medium] Object metric %q%s on %s is %.3fx its target; compare this object-level load with per-pod load before changing replica limits.", spec.Object.Metric.Name, selectorSuffix(spec.Object.Metric.Selector), object, *formatted.Ratio),
					Severity:   SeverityInfo,
					Confidence: ConfidenceMedium,
				})
			}
		} else {
			entries = append(entries, DiagnosticEntry{
				Reason:     "ObjectMetricDiagnostic",
				Message:    fmt.Sprintf("[confidence: high] Object metric %q%s is configured but no matching current metric status is reported; verify the described object and metric adapter output.", spec.Object.Metric.Name, selectorSuffix(spec.Object.Metric.Selector)),
				Severity:   SeverityWarning,
				Confidence: ConfidenceHigh,
			})
		}
	}
	return entries
}

// KEDADiagnostics generates diagnostic entries when the HPA appears KEDA-managed.
func KEDADiagnostics(hpa *autoscalingv2.HorizontalPodAutoscaler) []DiagnosticEntry {
	if !looksLikeKEDAManaged(hpa) {
		return nil
	}
	entries := []DiagnosticEntry{
		{
			Reason:     "KEDADiagnostic",
			Message:    "[confidence: medium] This HPA appears to be managed by KEDA. HPA status explains the final autoscaling object, but KEDA ScaledObject, TriggerAuthentication, and scaler errors may explain missing external metrics.",
			Severity:   SeverityInfo,
			Confidence: ConfidenceMedium,
		},
	}
	if len(hpa.Spec.Metrics) == 0 {
		entries = append(entries, DiagnosticEntry{
			Reason:     "KEDADiagnostic",
			Message:    "[confidence: high] KEDA-style HPA has no visible spec.metrics; check whether KEDA has reconciled the ScaledObject successfully.",
			Severity:   SeverityWarning,
			Confidence: ConfidenceHigh,
		})
	}
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ExternalMetricSourceType && spec.External != nil {
			entries = append(entries, DiagnosticEntry{
				Reason:     "KEDADiagnostic",
				Message:    fmt.Sprintf("[confidence: medium] For KEDA external metric %q, inspect the ScaledObject status.conditions and keda-operator logs if HPA currentMetrics is missing or stale.", spec.External.Metric.Name),
				Severity:   SeverityInfo,
				Confidence: ConfidenceMedium,
			})
		}
	}
	return entries
}

// looksLikeKEDAManaged uses heuristic signals to detect KEDA-managed HPAs.
//
// Detection signals (all heuristic, no CRD lookup):
//   - HPA label key or value containing "keda.sh" or "keda" (case-insensitive)
//   - HPA annotation key or value containing "keda.sh" or "keda" (case-insensitive)
//   - HPA name prefixed with "keda-hpa-"
//
// Limitations: This heuristic may produce false positives (HPA named "keda-hpa-..."
// but not managed by KEDA) or false negatives (KEDA-managed HPAs with custom names
// and no KEDA labels/annotations). For authoritative detection, use internal/kube/keda.go
// DetectKEDA() which performs real ScaledObject CRD lookups when the KEDA API is available.
func looksLikeKEDAManaged(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for key, value := range hpa.Labels {
		if strings.Contains(strings.ToLower(key), "keda.sh") || strings.Contains(strings.ToLower(value), "keda") {
			return true
		}
	}
	for key, value := range hpa.Annotations {
		if strings.Contains(strings.ToLower(key), "keda.sh") || strings.Contains(strings.ToLower(value), "keda") {
			return true
		}
	}
	return strings.HasPrefix(hpa.Name, "keda-hpa-")
}

// RecommendedActions generates actionable recommendation strings.
func RecommendedActions(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []string {
	var actions []string
	if hpa.Status.ObservedGeneration != nil && *hpa.Status.ObservedGeneration < hpa.Generation {
		actions = append(actions, "Wait for the HPA controller to observe the latest spec generation before trusting this status.")
	}
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		actions = append(actions, "Check metrics-server or custom/external metrics adapters; ScalingActive is not True.")
		actions = append(actions, staleMetricActions(hpa)...)
		return actions
	}
	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		if window := scaleDownStabilizationWindow(hpa); window != nil {
			actions = append(actions, fmt.Sprintf("CPU or memory may already be low, but scale-down is stabilized; estimated wait up to ~%ds or review spec.behavior.scaleDown.stabilizationWindowSeconds.", *window))
		} else {
			actions = append(actions, "CPU or memory may already be low, but scale-down is stabilized; review HPA behavior and recent recommendations.")
		}
	}
	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			actions = append(actions, "HPA is capped at maxReplicas; raise maxReplicas or reduce load/target utilization if more capacity is expected.")
		case minReplicas:
			actions = append(actions, "HPA is capped at minReplicas; lower minReplicas if scale-down below this point is expected.")
		}
	}
	if len(actions) == 0 && hpa.Status.DesiredReplicas == hpa.Status.CurrentReplicas {
		actions = append(actions, "No immediate action is visible from HPA status; inspect metrics and recent Events if behavior is unexpected.")
	}
	return actions
}

func staleMetricActions(hpa *autoscalingv2.HorizontalPodAutoscaler) []string {
	var actions []string
	for _, spec := range hpa.Spec.Metrics {
		switch {
		case spec.Type == autoscalingv2.ExternalMetricSourceType && spec.External != nil:
			actions = append(actions, fmt.Sprintf("Verify external metric %q in the external metrics API; if it is retired, remove it from spec.metrics so it no longer blocks scaling.", spec.External.Metric.Name))
		case spec.Type == autoscalingv2.ObjectMetricSourceType && spec.Object != nil:
			actions = append(actions, fmt.Sprintf("Verify object metric %q and its described object %s/%s before changing replica bounds.", spec.Object.Metric.Name, spec.Object.DescribedObject.Kind, spec.Object.DescribedObject.Name))
		}
	}
	return actions
}

// buildStructuredActions mirrors the key cases from RecommendedActions() and
// returns machine-readable StructuredMessage entries.
func buildStructuredActions(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []StructuredMessage {
	var msgs []StructuredMessage

	// Wait for generation
	if hpa.Status.ObservedGeneration != nil && *hpa.Status.ObservedGeneration < hpa.Generation {
		msgs = append(msgs, StructuredMessage{
			Reason:     "WaitForGeneration",
			Message:    "Status does not reflect the latest spec",
			NextStep:   "Wait for controller reconciliation",
			Severity:   SeverityWarning,
			Confidence: ConfidenceHigh,
		})
	}

	// ScalingActive not True → check metrics
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		msgs = append(msgs, StructuredMessage{
			Reason:     "RestoreMetrics",
			Message:    "ScalingActive is not True",
			NextStep:   "Check metrics-server or custom/external metrics adapters",
			Severity:   SeverityError,
			Confidence: ConfidenceHigh,
		})
		return msgs
	}

	// ScaleDownStabilized
	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		nextStep := "Review HPA behavior and recent recommendations"
		if window := scaleDownStabilizationWindow(hpa); window != nil {
			nextStep = fmt.Sprintf("Estimated wait up to ~%ds or review spec.behavior.scaleDown.stabilizationWindowSeconds", *window)
		}
		msgs = append(msgs, StructuredMessage{
			Reason:     "WaitForStabilization",
			Message:    "Scale-down is stabilized",
			NextStep:   nextStep,
			Severity:   SeverityInfo,
			Confidence: ConfidenceMedium,
		})
	}

	// ScalingLimited
	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			msgs = append(msgs, StructuredMessage{
				Reason:     "RaiseMaxReplicas",
				Message:    "HPA is capped at maxReplicas",
				NextStep:   "Raise maxReplicas or reduce load/target utilization if more capacity is expected",
				Severity:   SeverityWarning,
				Confidence: ConfidenceHigh,
			})
		case minReplicas:
			msgs = append(msgs, StructuredMessage{
				Reason:     "LowerMinReplicas",
				Message:    "HPA is capped at minReplicas",
				NextStep:   "Lower minReplicas if scale-down below this point is expected",
				Severity:   SeverityWarning,
				Confidence: ConfidenceHigh,
			})
		}
	}

	return msgs
}

// DebugLines generates verbose debug information lines.
func DebugLines(hpa *autoscalingv2.HorizontalPodAutoscaler, analysis Analysis) []string {
	var lines []string
	lines = append(lines, fmt.Sprintf("replicas: current=%d desired=%d min=%d max=%d diff=%+d", analysis.Current, analysis.Desired, analysis.Min, analysis.Max, analysis.Desired-analysis.Current))
	lines = append(lines, fmt.Sprintf("health: state=%s score=%d", analysis.Health, analysis.HealthScore))
	for _, metric := range analysis.Metrics {
		if metric.Ratio == nil {
			lines = append(lines, fmt.Sprintf("metric %s/%s: current=%s target=%s ratio=<unknown> note=%q", metric.Type, metric.Name, metric.Current, metric.Target, metric.Note))
			continue
		}
		lines = append(lines, fmt.Sprintf("metric %s/%s: current=%s target=%s ratio=%.3f note=%q", metric.Type, metric.Name, metric.Current, metric.Target, *metric.Ratio, metric.Note))
	}
	for _, condition := range hpa.Status.Conditions {
		lines = append(lines, fmt.Sprintf("condition %s=%s reason=%s", condition.Type, condition.Status, condition.Reason))
	}
	if analysis.ImpactMetric != nil {
		lines = append(lines, fmt.Sprintf("impactEstimate: metric=%s ratio=%.3f confidence=medium", analysis.ImpactMetric.Name, analysis.ImpactMetric.Ratio))
	}
	return lines
}

// FormatBehavior extracts and formats HPA behavior rules.
func FormatBehavior(hpa *autoscalingv2.HorizontalPodAutoscaler) []BehaviorRule {
	if hpa.Spec.Behavior == nil {
		return nil
	}

	var out []BehaviorRule
	if rule := FormatBehaviorRule("scaleUp", hpa.Spec.Behavior.ScaleUp); rule != nil {
		out = append(out, *rule)
	}
	if rule := FormatBehaviorRule("scaleDown", hpa.Spec.Behavior.ScaleDown); rule != nil {
		out = append(out, *rule)
	}
	return out
}

// FormatBehaviorRule formats a single behavior rule (scaleUp or scaleDown).
func FormatBehaviorRule(direction string, rules *autoscalingv2.HPAScalingRules) *BehaviorRule {
	if rules == nil {
		return nil
	}

	rule := BehaviorRule{
		Direction:                  direction,
		StabilizationWindowSeconds: rules.StabilizationWindowSeconds,
	}
	if rules.SelectPolicy != nil {
		rule.SelectPolicy = string(*rules.SelectPolicy)
	}
	if rules.Tolerance != nil && !rules.Tolerance.IsZero() {
		rule.Policies = append(rule.Policies, "tolerance "+rules.Tolerance.String())
	}
	for _, policy := range rules.Policies {
		rule.Policies = append(rule.Policies, fmt.Sprintf("%s %d per %ds", policy.Type, policy.Value, policy.PeriodSeconds))
	}

	var parts []string
	if rule.StabilizationWindowSeconds != nil {
		parts = append(parts, fmt.Sprintf("stabilizationWindow=%ds", *rule.StabilizationWindowSeconds))
	}
	if rule.SelectPolicy != "" {
		parts = append(parts, "selectPolicy="+rule.SelectPolicy)
	}
	if len(rule.Policies) > 0 {
		parts = append(parts, "policies="+strings.Join(rule.Policies, ", "))
	}
	if len(parts) == 0 {
		parts = append(parts, "custom behavior is present")
	}
	rule.Text = direction + ": " + strings.Join(parts, "; ")
	return &rule
}
