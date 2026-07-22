// Package retrospective reconstructs a best-effort timeline of past HPA
// scaling decisions from Kubernetes events, and performs deeper replay
// analysis (bottlenecks, control cycles, stabilization windows) on that
// timeline. It depends only on pkg/hpa/internal leaf packages and
// pkg/hpa/rendutil; the cmd/ and internal/tui layers call it directly
// (retrospective.BuildTimeline, retrospective.AnalyzeReplay, etc.).
package retrospective

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/clock"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/conditions"
	eventutil "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// metricReasonRegex extracts metric information from HPA rescale reason strings.
var metricReasonRegex = regexp.MustCompile(`(?i)reason:\s*(.+)$`)

// BuildTimeline reconstructs a best-effort timeline of past scaling
// decisions from Kubernetes events and the current HPA status. The result is an
// estimate because the HPA controller's internal decision history is not fully
// visible through the Kubernetes API.
//
// Precondition: events must be sorted in ascending chronological order.
func BuildTimeline(events []eventutil.Event, hpa *autoscalingv2.HorizontalPodAutoscaler, since time.Time) Timeline {
	tl := Timeline{
		HPAName:   hpa.Name,
		Namespace: hpa.Namespace,
		Since:     since,
		Until:     clock.Now(),
		Disclaimer: "Best-effort reconstruction from Kubernetes events and current HPA status. " +
			"Internal controller calculations, exact metric values at decision time, and " +
			"suppressed-but-not-logged decisions are not visible. Multi-metric winner is estimated.",
	}

	if len(events) == 0 {
		tl.Warnings = append(tl.Warnings,
			fmt.Sprintf("No HPA events found since %s. Events may have expired (Kubernetes typically retains events for ~1 hour).", since.Format(time.RFC3339)))
		return tl
	}

	prevDesired := hpa.Status.CurrentReplicas
	var entries []Entry

	for _, event := range events {
		entry := classifyEvent(event, prevDesired, hpa)
		if entry == nil {
			continue
		}
		entries = append(entries, *entry)
		if entry.Category == "rescale" {
			newSize := parseNewSize(event.Message)
			if newSize > 0 {
				prevDesired = newSize
			}
		}
	}

	// Insert estimated stabilization/policy suppression entries where possible.
	entries = insertSuppressionEntries(entries, hpa)

	tl.Entries = entries
	return tl
}

// classifyEvent maps a Kubernetes event to a Entry based on its
// reason and message content.
func classifyEvent(event eventutil.Event, prevDesired int32, hpa *autoscalingv2.HorizontalPodAutoscaler) *Entry {
	switch event.Reason {
	case "SuccessfulRescale":
		newSize := parseNewSize(event.Message)
		if newSize == 0 {
			// Fallback: cannot parse, emit raw message.
			return &Entry{
				Timestamp:  event.Timestamp,
				Category:   "rescale",
				Message:    event.Message,
				Source:     "event",
				Confidence: "low",
			}
		}

		metricCtx := formatMetricContext(event.Message, hpa)

		msg := fmt.Sprintf("desired %d -> %d", prevDesired, newSize)
		if metricCtx != "" {
			msg = fmt.Sprintf("%s     desired %d -> %d", metricCtx, prevDesired, newSize)
		}

		return &Entry{
			Timestamp:  event.Timestamp,
			Category:   "rescale",
			Message:    msg,
			Source:     "event",
			Confidence: "high",
		}

	case "FailedRescale":
		return &Entry{
			Timestamp:  event.Timestamp,
			Category:   "rescale",
			Message:    fmt.Sprintf("failed to rescale: %s", truncateMessageRetro(event.Message, 80)),
			Source:     "event",
			Confidence: "high",
		}

	case "FailedGetResourceMetric", "FailedGetExternalMetric", "FailedGetObjectMetric":
		return &Entry{
			Timestamp:  event.Timestamp,
			Category:   "metrics-unavailable",
			Message:    fmt.Sprintf("%s  metrics unavailable", event.Reason),
			Source:     "event",
			Confidence: "high",
		}

	case conditions.ScalingLimited, "TooManyReplicas", "TooFewReplicas":
		return &Entry{
			Timestamp:  event.Timestamp,
			Category:   "scaling-limited",
			Message:    fmt.Sprintf("ScalingLimited=True      capped by maxReplicas=%d", hpa.Spec.MaxReplicas),
			Source:     "event",
			Confidence: "medium",
		}

	case "ScaleDownStabilized":
		return &Entry{
			Timestamp:  event.Timestamp,
			Category:   "stabilized",
			Message:    formatScaleDownStabilizedTimelineMessage(hpa, event.Timestamp),
			Source:     "event",
			Confidence: "medium",
		}

	default:
		// Other event reasons (DesiredReplicasComputed, NewMetricValue, etc.)
		// are treated as informational metric-change entries.
		return &Entry{
			Timestamp:  event.Timestamp,
			Category:   "metric-change",
			Message:    truncateMessageRetro(event.Reason+": "+event.Message, 80),
			Source:     "event",
			Confidence: "medium",
		}
	}
}

// parseNewSize extracts the new replica count from an HPA event message.
func parseNewSize(message string) int32 {
	result, _ := eventutil.ParseNewSize(message)
	return result
}

// formatMetricContext attempts to extract the metric reason from a rescale
// event message and enrich it with current metric ratio data.
func formatMetricContext(message string, hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	match := metricReasonRegex.FindStringSubmatch(message)
	if len(match) < 2 {
		return ""
	}
	reason := strings.TrimSpace(match[1])

	// Try to match the reason text with a known metric name from the HPA spec.
	reasonLower := strings.ToLower(reason)
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType && metric.Resource != nil {
			name := strings.ToLower(string(metric.Resource.Name))
			if strings.Contains(reasonLower, name) {
				if metric.Resource.Current.AverageUtilization != nil {
					if target := resourceMetricTargetUtilization(hpa, metric.Resource.Name); target != nil {
						return fmt.Sprintf("%s %d%% %s target %d%%",
							strings.ToUpper(string(metric.Resource.Name)),
							*metric.Resource.Current.AverageUtilization,
							compareInt32(*metric.Resource.Current.AverageUtilization, *target),
							*target)
					}
					return fmt.Sprintf("%s %d%%", strings.ToUpper(string(metric.Resource.Name)), *metric.Resource.Current.AverageUtilization)
				}
			}
		}
	}

	// Could not correlate with a specific metric; return the raw reason.
	if len(reason) > 50 {
		reason = reason[:47] + "..."
	}
	return reason
}

func resourceMetricTargetUtilization(hpa *autoscalingv2.HorizontalPodAutoscaler, name corev1.ResourceName) *int32 {
	if hpa == nil {
		return nil
	}
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ResourceMetricSourceType || spec.Resource == nil {
			continue
		}
		if spec.Resource.Name == name {
			return spec.Resource.Target.AverageUtilization
		}
	}
	return nil
}

func compareInt32(current, target int32) string {
	switch {
	case current > target:
		return ">"
	case current < target:
		return "<"
	default:
		return "="
	}
}

func formatScaleDownStabilizedTimelineMessage(hpa *autoscalingv2.HorizontalPodAutoscaler, ts time.Time) string {
	remaining := scaleDownStabilizationWindowSeconds(hpa)
	cond := conditions.Find(hpa, conditions.AbleToScale)
	if cond != nil && !cond.LastTransitionTime.IsZero() && remaining > 0 {
		elapsed := ts.Sub(cond.LastTransitionTime.Time)
		left := time.Duration(remaining)*time.Second - elapsed
		if left > 0 {
			return fmt.Sprintf("ScaleDownStabilized      scale-down suppressed, ~%ds remaining", int(left.Seconds()))
		}
	}
	if remaining > 0 {
		return fmt.Sprintf("ScaleDownStabilized      scale-down suppressed, ~%ds remaining", remaining)
	}
	return "ScaleDownStabilized      scale-down suppressed"
}

// insertSuppressionEntries adds estimated stabilization and policy-limited
// entries between rescale events when the HPA spec and conditions suggest
// that scaling was deliberately held back.
func insertSuppressionEntries(entries []Entry, hpa *autoscalingv2.HorizontalPodAutoscaler) []Entry {
	if len(entries) == 0 {
		return entries
	}

	// Check for active scale-down stabilization.
	stabilizationWindow := scaleDownStabilizationWindowSeconds(hpa)
	isStabilized := hasScaleDownStabilizedCondition(hpa)

	// Check for scale-up policies that could limit rate.
	scaleUpPolicy := formatScaleUpPolicySummary(hpa)

	var result []Entry

	for i, entry := range entries {
		result = append(result, entry)

		// After a rescale event, check if suppression might have occurred
		// before the next event.
		if entry.Category != "rescale" || i >= len(entries)-1 {
			continue
		}
		nextEntry := entries[i+1]
		gap := nextEntry.Timestamp.Sub(entry.Timestamp)

		// Detect direction from the "desired A -> B" message format.
		isScaleUp := isScaleUpEntry(entry.Message)
		nextIsScaleDown := isScaleDownEntry(nextEntry.Message)

		// If the next entry is a scale-down and stabilization is active,
		// insert a stabilization suppression entry before it.
		if isStabilized && stabilizationWindow > 0 && nextIsScaleDown {
			remaining := gap.Seconds()
			if remaining > float64(stabilizationWindow) {
				suppressedAt := nextEntry.Timestamp.Add(-time.Duration(stabilizationWindow) * time.Second)
				result = append(result, Entry{
					Timestamp:  suppressedAt,
					Category:   "stabilized",
					Message:    fmt.Sprintf("scaleDown suppressed by stabilization window (%ds)", stabilizationWindow),
					Source:     "estimated",
					Confidence: "medium",
				})
			}
		}

		// If scale-up policies are limiting and the gap suggests policy delays.
		if scaleUpPolicy != "" && isScaleUp && gap > 30*time.Second {
			result = append(result, Entry{
				Timestamp:  entry.Timestamp.Add(gap / 2),
				Category:   "policy-limited",
				Message:    fmt.Sprintf("scaleUp limited by policy: %s", scaleUpPolicy),
				Source:     "estimated",
				Confidence: "low",
			})
		}
	}

	return result
}

// scaleDownStabilizationWindowSeconds returns the scale-down stabilization
// window in seconds, or 0 if not configured.
func scaleDownStabilizationWindowSeconds(hpa *autoscalingv2.HorizontalPodAutoscaler) int32 {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return 0
	}
	if hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil {
		return 0
	}
	return *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
}

// hasScaleDownStabilizedCondition checks if the HPA currently has an
// AbleToScale condition with the ScaleDownStabilized reason.
func hasScaleDownStabilizedCondition(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	cond := conditions.Find(hpa, conditions.AbleToScale)
	return cond != nil && cond.Reason == "ScaleDownStabilized"
}

// isScaleDownEntry detects whether the entry represents a scale-down
// by parsing the "desired A -> B" format and checking B < A.
func isScaleDownEntry(msg string) bool {
	from, to := parseDesiredRange(msg)
	return from > 0 && to > 0 && to < from
}

// isScaleUpEntry detects whether the entry represents a scale-up
// by parsing the "desired A -> B" format and checking B > A.
func isScaleUpEntry(msg string) bool {
	from, to := parseDesiredRange(msg)
	return from > 0 && to > from
}

// desiredRangeRegex extracts "desired A -> B" from a message.
var desiredRangeRegex = regexp.MustCompile(`desired (\d+) -> (\d+)`)

// parseDesiredRange extracts the from/to replica counts from a "desired A -> B" message.
func parseDesiredRange(msg string) (from, to int32) {
	match := desiredRangeRegex.FindStringSubmatch(msg)
	if len(match) < 3 {
		return 0, 0
	}
	if _, err := fmt.Sscanf(match[1], "%d", &from); err != nil {
		return 0, 0
	}
	if _, err := fmt.Sscanf(match[2], "%d", &to); err != nil {
		return 0, 0
	}
	return from, to
}

// formatScaleUpPolicySummary returns a compact summary of the first scale-up
// behavior policy, e.g. "+2 pods / 60s". Returns empty string if none configured.
func formatScaleUpPolicySummary(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleUp == nil {
		return ""
	}
	for _, policy := range hpa.Spec.Behavior.ScaleUp.Policies {
		if policy.Type == autoscalingv2.PodsScalingPolicy {
			return fmt.Sprintf("+%d pods / %ds", policy.Value, policy.PeriodSeconds)
		}
		if policy.Type == autoscalingv2.PercentScalingPolicy {
			return fmt.Sprintf("+%d%% / %ds", policy.Value, policy.PeriodSeconds)
		}
	}
	return ""
}

// truncateMessageRetro truncates a message to maxLen terminal columns.
func truncateMessageRetro(msg string, maxLen int) string {
	return rendutil.TruncateDisplayWidth(msg, maxLen, "...")
}
