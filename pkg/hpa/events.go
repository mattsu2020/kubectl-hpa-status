package hpa

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
)

// This file re-exports the Event type and converters from
// pkg/hpa/internal/event so existing call sites in pkg/hpa and cmd/ keep
// working. Sub-packages that need Event import the event package directly.

// Event is a simplified Kubernetes event with reason, message, and timestamp.
// Aliased from event.Event (canonical definition).
type Event = event.Event

// EventFromCore converts a corev1.Event to a simplified Event struct.
// Delegates to event.FromCore.
func EventFromCore(e corev1.Event) Event {
	return event.FromCore(e)
}

// EventsFromCore converts a slice of corev1.Event to a slice of Event.
// Delegates to event.FromCoreSlice.
func EventsFromCore(coreEvents []corev1.Event) []Event {
	return event.FromCoreSlice(coreEvents)
}
