package hpa

import (
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Event is a simplified Kubernetes event with reason, message, and timestamp.
// pkg/hpa keeps this type (and the EventFromCore converter) so external
// consumers can construct Event values from data fetched at a higher layer.
// The Kubernetes client calls themselves live in internal/kube.
type Event struct {
	Reason    string    `json:"reason" yaml:"reason"`
	Message   string    `json:"message" yaml:"message"`
	Timestamp time.Time `json:"timestamp,omitempty" yaml:"timestamp,omitempty"`
}

// eventTimestamp extracts the timestamp from a corev1.Event, preferring
// LastTimestamp and falling back to EventTime.
func eventTimestamp(event corev1.Event) time.Time {
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	return time.Time{}
}

// EventFromCore converts a corev1.Event to a simplified Event struct.
func EventFromCore(event corev1.Event) Event {
	return Event{
		Reason:    event.Reason,
		Message:   strings.ReplaceAll(event.Message, "\n", " "),
		Timestamp: eventTimestamp(event),
	}
}

// EventsFromCore converts a slice of corev1.Event to a slice of Event. It
// preserves input order and allocates a fresh slice so callers never share
// backing arrays with the input.
func EventsFromCore(coreEvents []corev1.Event) []Event {
	events := make([]Event, 0, len(coreEvents))
	for _, ce := range coreEvents {
		events = append(events, EventFromCore(ce))
	}
	return events
}
