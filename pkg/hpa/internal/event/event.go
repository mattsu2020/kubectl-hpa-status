// Package event defines the simplified Event type used across the pkg/hpa
// analysis domains (flapping, churn, timeline, etc.). It was lifted out of
// pkg/hpa/events.go so leaf sub-packages can accept []Event without reaching
// back into the analysis core, which would create an import cycle.
package event

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

var newSizePattern = regexp.MustCompile(`(?i)new size:\s*(\d+)`)

// Event is a simplified Kubernetes event with reason, message, and timestamp.
// pkg/hpa re-exports it as hpaanalysis.Event; the Kubernetes client calls
// themselves live in internal/kube.
type Event struct {
	Reason    string    `json:"reason" yaml:"reason"`
	Message   string    `json:"message" yaml:"message"`
	Timestamp time.Time `json:"timestamp,omitempty" yaml:"timestamp,omitempty"`
}

// timestamp extracts the timestamp from a corev1.Event, preferring
// LastTimestamp and falling back to EventTime.
func timestamp(e corev1.Event) time.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	return time.Time{}
}

// FromCore converts a corev1.Event to a simplified Event struct.
func FromCore(e corev1.Event) Event {
	return Event{
		Reason:    e.Reason,
		Message:   strings.ReplaceAll(e.Message, "\n", " "),
		Timestamp: timestamp(e),
	}
}

// FromCoreSlice converts a slice of corev1.Event to a slice of Event. It
// preserves input order and allocates a fresh slice so callers never share
// backing arrays with the input.
func FromCoreSlice(coreEvents []corev1.Event) []Event {
	events := make([]Event, 0, len(coreEvents))
	for _, ce := range coreEvents {
		events = append(events, FromCore(ce))
	}
	return events
}

// RescaleData captures one HPA rescale event extracted from a
// SuccessfulRescale event message. Shared by the flapping and churn domains.
type RescaleData struct {
	Timestamp time.Time
	NewSize   int32
}

// ParseNewSize extracts the replica count from a SuccessfulRescale message.
// The boolean distinguishes a valid scale-to-zero event from malformed text.
func ParseNewSize(message string) (int32, bool) {
	match := newSizePattern.FindStringSubmatch(message)
	if len(match) < 2 {
		return 0, false
	}
	value, err := strconv.ParseInt(match[1], 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(value), true
}
