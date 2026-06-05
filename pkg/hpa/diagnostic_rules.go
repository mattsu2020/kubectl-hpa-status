package hpa

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

type diagnosticContext struct {
	cases []interpretationCase
	stop  bool
}

type diagnosticRule func(*autoscalingv2.HorizontalPodAutoscaler, int32, *diagnosticContext)

func coreDiagnosticRules() []diagnosticRule {
	return []diagnosticRule{
		staleStatusRule,
		scalingActiveDiagnosticRule,
		ableToScaleDiagnosticRule,
		scalingLimitedDiagnosticRule,
		replicaDirectionRule,
		multiMetricRule,
		metricDisagreementRule,
		scaleToZeroRule,
	}
}

func staleStatusRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32, ctx *diagnosticContext) {
	if hpa.Status.ObservedGeneration == nil || *hpa.Status.ObservedGeneration >= hpa.Generation {
		return
	}
	ctx.cases = append(ctx.cases, interpretationCase{
		reason:     "StaleStatus",
		message:    fmt.Sprintf("[confidence: high] Warning: status.observedGeneration=%d is behind metadata.generation=%d; the status may not reflect the latest spec.", *hpa.Status.ObservedGeneration, hpa.Generation),
		nextStep:   "Wait for HPA controller to process latest spec",
		severity:   SeverityWarning,
		confidence: ConfidenceHigh,
	})
}

func scalingActiveDiagnosticRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32, ctx *diagnosticContext) {
	condition := FindCondition(hpa, "ScalingActive")
	if condition == nil || condition.Status == corev1.ConditionTrue {
		return
	}
	ctx.cases = append(ctx.cases,
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
	ctx.stop = true
}

func ableToScaleDiagnosticRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32, ctx *diagnosticContext) {
	condition := FindCondition(hpa, "AbleToScale")
	if condition == nil {
		return
	}
	if condition.Status != corev1.ConditionTrue {
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "UnableToScale",
			message:    fmt.Sprintf("[confidence: high] AbleToScale is %s: %s - %s", condition.Status, condition.Reason, condition.Message),
			nextStep:   "Check scaleTargetRef, RBAC, and scale subresource",
			severity:   SeverityError,
			confidence: ConfidenceHigh,
		})
		return
	}
	if condition.Reason != "ScaleDownStabilized" {
		return
	}
	if remaining := estimateStabilizationRemaining(hpa); remaining != nil && *remaining > 0 {
		message := fmt.Sprintf("[confidence: medium] Scale down appears stabilized: %s (estimated ~%d seconds remaining before scale-down is allowed).", condition.Message, *remaining)
		nextStep := fmt.Sprintf("Scale-down stabilized; estimated ~%d seconds remaining", *remaining)
		if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
			message += " Note: no spec.behavior.scaleDown is set; the controller-manager default (usually 300s) is used and may differ from this estimate."
		}
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "ScaleDownStabilized",
			message:    message,
			nextStep:   nextStep,
			severity:   SeverityInfo,
			confidence: ConfidenceMedium,
		})
		return
	}
	ctx.cases = append(ctx.cases, interpretationCase{
		reason:     "ScaleDownStabilized",
		message:    fmt.Sprintf("[confidence: medium] Scale down appears stabilized: %s", condition.Message),
		severity:   SeverityInfo,
		confidence: ConfidenceMedium,
	})
}

func scalingLimitedDiagnosticRule(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, ctx *diagnosticContext) {
	condition := FindCondition(hpa, "ScalingLimited")
	if condition == nil || condition.Status != corev1.ConditionTrue {
		return
	}
	switch hpa.Status.DesiredReplicas {
	case hpa.Spec.MaxReplicas:
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "LimitedByMaxReplicas",
			message:    "[confidence: high] ScalingLimited reports that the visible desired replica count is constrained by maxReplicas.",
			nextStep:   "Raise maxReplicas or reduce load/target utilization",
			severity:   SeverityWarning,
			confidence: ConfidenceHigh,
		})
	case minReplicas:
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "LimitedByMinReplicas",
			message:    "[confidence: high] ScalingLimited reports that the visible desired replica count is constrained by minReplicas.",
			nextStep:   "Lower minReplicas if scale-down below this point is expected",
			severity:   SeverityWarning,
			confidence: ConfidenceHigh,
		})
	default:
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "ScalingLimited",
			message:    "[confidence: high] The recommendation is reported as limited.",
			severity:   SeverityWarning,
			confidence: ConfidenceHigh,
		})
	}
}

func replicaDirectionRule(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, ctx *diagnosticContext) {
	switch {
	case hpa.Status.DesiredReplicas > hpa.Status.CurrentReplicas:
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "ScaleUpRecommended",
			message:    "[confidence: high] desiredReplicas is greater than currentReplicas, so the HPA is recommending scale up.",
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
	case hpa.Status.DesiredReplicas < hpa.Status.CurrentReplicas:
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "ScaleDownRecommended",
			message:    "[confidence: high] desiredReplicas is less than currentReplicas, so the HPA is recommending scale down.",
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
	default:
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "NoScaleVisible",
			message:    "[confidence: high] desiredReplicas equals currentReplicas, so no immediate replica change is visible from status.",
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
		toleranceDiagnosticRule(hpa, minReplicas, ctx)
	}
}

func toleranceDiagnosticRule(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, ctx *diagnosticContext) {
	if hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas || hpa.Status.DesiredReplicas == minReplicas {
		return
	}
	metric, ok := MetricOutsideTarget(hpa)
	if !ok {
		return
	}
	deviation := metric.Ratio - 1.0
	if deviation < 0 {
		deviation = -deviation
	}
	if deviation < 0.1 {
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "ToleranceNoScale",
			message:    fmt.Sprintf("[tolerance-confirmed] [confidence: high] %s metric ratio is %.3f (within ±10%% of target); the Kubernetes default tolerance band of 0.1 (10%%) explains why replicas are unchanged despite %s being %.1f%% %s target.", metric.Name, metric.Ratio, metric.Name, (metric.Ratio-1)*100, metric.Note),
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
	} else {
		ctx.cases = append(ctx.cases,
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
	ctx.cases = append(ctx.cases, interpretationCase{
		reason:     "ToleranceNoScale",
		message:    "[confidence: high] The plugin avoids claiming the exact internal reason because rounding, stabilization, or conservative metric handling may also affect the final result.",
		severity:   SeverityInfo,
		confidence: ConfidenceHigh,
	})
}

func multiMetricRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32, ctx *diagnosticContext) {
	if hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas && len(hpa.Status.CurrentMetrics) > 1 {
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "MaxReplicasWinnerHidden",
			message:    "[confidence: high] desiredReplicas == maxReplicas; the winning metric cannot be reliably determined because the replica cap may hide the true metric winner.",
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
		return
	}
	if guess, ok := MostInfluentialMetric(hpa); ok && len(hpa.Status.CurrentMetrics) > 1 {
		ctx.cases = append(ctx.cases,
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
		return
	}
	if len(hpa.Status.CurrentMetrics) > 1 {
		ctx.cases = append(ctx.cases,
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
}

func metricDisagreementRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32, ctx *diagnosticContext) {
	if len(hpa.Status.CurrentMetrics) <= 1 {
		return
	}
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
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "MetricDisagreement",
			message:    fmt.Sprintf("[confidence: medium] Metric disagreement detected: %s want scale-up (ratio > 1.0) while %s want scale-down (ratio < 1.0). The HPA controller will use its selectPolicy to resolve this, but consider whether the metric targets are well-tuned.", strings.Join(scaleUp, ", "), strings.Join(scaleDown, ", ")),
			severity:   SeverityWarning,
			confidence: ConfidenceMedium,
		})
	}
}

func scaleToZeroRule(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, ctx *diagnosticContext) {
	if minReplicas != 0 {
		return
	}
	if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas == 0 {
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "ScaleToZero",
			message:    "[confidence: high] Scale-to-zero is enabled (minReplicas=0) and the workload is currently at zero replicas. The next scale-up requires a cold start which may introduce additional latency.",
			nextStep:   "Next scale-up requires a cold start which may introduce additional latency",
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
	} else if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas > 0 {
		ctx.cases = append(ctx.cases, interpretationCase{
			reason:     "ScaleToZero",
			message:    "[confidence: high] Scale-to-zero is enabled (minReplicas=0) and the HPA wants to scale to zero. Note: scaling from 0 back to 1 requires a cold start.",
			nextStep:   "Scaling from 0 back to 1 requires a cold start",
			severity:   SeverityInfo,
			confidence: ConfidenceHigh,
		})
	}
}
