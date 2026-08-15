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
	"strconv"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/clock"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/conditions"
	eventutil "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
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
		Disclaimer: "Best-effort reconstruction from Kubernetes events. " +
			"Current HPA configuration is shown only when explicitly labeled and is not projected backward. " +
			"Internal controller calculations, exact metric values at decision time, and " +
			"suppressed-but-not-logged decisions are not visible. Multi-metric winner is estimated.",
	}

	if len(events) == 0 {
		tl.Warnings = append(tl.Warnings,
			fmt.Sprintf("No HPA events found since %s. Events may have expired (Kubernetes typically retains events for ~1 hour).", since.Format(time.RFC3339)))
		return tl
	}

	var prevDesired *int32
	var entries []Entry

	for _, event := range events {
		entry := classifyEvent(event, prevDesired, hpa)
		if entry == nil {
			continue
		}
		entries = append(entries, *entry)
		if event.Reason == "SuccessfulRescale" {
			newSize, ok := parseNewSize(event.Message)
			if ok {
				prevDesired = int32Pointer(newSize)
			}
		}
	}

	tl.Entries = entries
	return tl
}

// classifyEvent maps a Kubernetes event to a Entry based on its
// reason and message content.
func classifyEvent(event eventutil.Event, prevDesired *int32, hpa *autoscalingv2.HorizontalPodAutoscaler) *Entry {
	switch event.Reason {
	case "SuccessfulRescale":
		newSize, ok := parseNewSize(event.Message)
		if !ok {
			// Fallback: cannot parse, emit raw message.
			return &Entry{
				Timestamp:  event.Timestamp,
				Category:   "rescale",
				Message:    event.Message,
				Source:     "event",
				Confidence: "low",
			}
		}

		metricCtx := formatMetricContext(event.Message)

		if prevDesired == nil {
			msg := fmt.Sprintf("desired <unknown> -> %d", newSize)
			if metricCtx != "" {
				msg = fmt.Sprintf("%s     desired <unknown> -> %d", metricCtx, newSize)
			}
			return &Entry{
				Timestamp:     event.Timestamp,
				Category:      "rescale",
				Message:       msg,
				Source:        "event",
				Confidence:    "low",
				ToReplicas:    int32Pointer(newSize),
				MetricContext: metricCtx,
			}
		}

		msg := fmt.Sprintf("desired %d -> %d", *prevDesired, newSize)
		if metricCtx != "" {
			msg = fmt.Sprintf("%s     desired %d -> %d", metricCtx, *prevDesired, newSize)
		}

		return &Entry{
			Timestamp:     event.Timestamp,
			Category:      "rescale",
			Message:       msg,
			Source:        "event",
			Confidence:    "high",
			FromReplicas:  int32Pointer(*prevDesired),
			ToReplicas:    int32Pointer(newSize),
			ParseValid:    true,
			MetricContext: metricCtx,
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

	case conditions.ScalingLimited:
		message := "ScalingLimited      scaling constraint recorded; exact historical limit unavailable"
		if strings.TrimSpace(event.Message) != "" {
			message = "ScalingLimited      " + truncateMessageRetro(event.Message, 80)
		}
		return &Entry{
			Timestamp:  event.Timestamp,
			Category:   "scaling-limited",
			Message:    message,
			Source:     "event",
			Confidence: "medium",
		}

	case "TooManyReplicas":
		return &Entry{
			Timestamp:  event.Timestamp,
			Category:   "scaling-limited",
			Message:    "TooManyReplicas      constrained by maxReplicas; historical value unavailable",
			Source:     "event",
			Confidence: "medium",
		}

	case "TooFewReplicas":
		return &Entry{
			Timestamp:  event.Timestamp,
			Category:   "scaling-limited",
			Message:    "TooFewReplicas      constrained by minReplicas; historical value unavailable",
			Source:     "event",
			Confidence: "medium",
		}

	case conditions.ReasonScaleDownStabilized:
		return &Entry{
			Timestamp:  event.Timestamp,
			Category:   "stabilized",
			Message:    formatScaleDownStabilizedTimelineMessage(hpa),
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
func parseNewSize(message string) (int32, bool) {
	return eventutil.ParseNewSize(message)
}

// formatMetricContext extracts only the reason recorded on the historical
// event. Current HPA metric values must not be projected backwards onto an
// earlier controller decision.
func formatMetricContext(message string) string {
	match := metricReasonRegex.FindStringSubmatch(message)
	if len(match) < 2 {
		return ""
	}
	reason := strings.TrimSpace(match[1])
	if len(reason) > 50 {
		reason = reason[:47] + "..."
	}
	return reason
}

func formatScaleDownStabilizedTimelineMessage(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	currentWindow := scaleDownStabilizationWindowSeconds(hpa)
	return fmt.Sprintf(
		"ScaleDownStabilized      suppression recorded; current effective window=%ds; historical duration unavailable",
		currentWindow,
	)
}

// scaleDownStabilizationWindowSeconds returns the current effective
// scale-down stabilization window. Kubernetes defaults an unspecified value
// to conditions.DefaultScaleDownStabilizationWindowSeconds. This current
// setting must not be used to reconstruct a past event's remaining duration.
func scaleDownStabilizationWindowSeconds(hpa *autoscalingv2.HorizontalPodAutoscaler) int32 {
	if hpa == nil || hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return conditions.DefaultScaleDownStabilizationWindowSeconds
	}
	if hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil {
		return conditions.DefaultScaleDownStabilizationWindowSeconds
	}
	return *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
}

func entryReplicaRange(entry Entry) (int32, int32, bool) {
	if entry.ParseValid && entry.FromReplicas != nil && entry.ToReplicas != nil {
		return *entry.FromReplicas, *entry.ToReplicas, true
	}
	return parseDesiredRange(entry.Message)
}

func int32Pointer(value int32) *int32 {
	return &value
}

// desiredRangeRegex extracts "desired A -> B" from a message.
var desiredRangeRegex = regexp.MustCompile(`desired (\d+) -> (\d+)`)

// parseDesiredRange extracts bounded, non-negative replica counts from a
// "desired A -> B" message.
func parseDesiredRange(msg string) (from, to int32, ok bool) {
	match := desiredRangeRegex.FindStringSubmatch(msg)
	if len(match) < 3 {
		return 0, 0, false
	}
	parsedFrom, err := strconv.ParseInt(match[1], 10, 32)
	if err != nil {
		return 0, 0, false
	}
	parsedTo, err := strconv.ParseInt(match[2], 10, 32)
	if err != nil {
		return 0, 0, false
	}
	return int32(parsedFrom), int32(parsedTo), true
}

// truncateMessageRetro truncates a message to maxLen terminal columns.
func truncateMessageRetro(msg string, maxLen int) string {
	return rendutil.TruncateDisplayWidth(msg, maxLen, "...")
}
