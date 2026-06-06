package hpa

import (
	"context"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

// Event is a simplified Kubernetes event with reason, message, and timestamp.
type Event struct {
	Reason    string    `json:"reason" yaml:"reason"`
	Message   string    `json:"message" yaml:"message"`
	Timestamp time.Time `json:"timestamp,omitempty" yaml:"timestamp,omitempty"`
}

// RecentEvents fetches recent Kubernetes events for the specified HPA.
func RecentEvents(ctx context.Context, client kubernetes.Interface, namespace, name string, limit int64) ([]Event, error) {
	selector := fields.AndSelectors(
		fields.OneTermEqualSelector("involvedObject.kind", "HorizontalPodAutoscaler"),
		fields.OneTermEqualSelector("involvedObject.name", name),
		fields.OneTermEqualSelector("involvedObject.namespace", namespace),
	)

	events, err := client.CoreV1().
		Events(namespace).
		List(ctx, metav1.ListOptions{
			FieldSelector: selector.String(),
			Limit:         limit,
		})
	if err != nil {
		return nil, err
	}

	sort.Slice(events.Items, func(i, j int) bool {
		return events.Items[i].LastTimestamp.After(events.Items[j].LastTimestamp.Time)
	})

	outLimit := len(events.Items)
	if outLimit > int(limit) {
		outLimit = int(limit)
	}

	out := make([]Event, 0, outLimit)
	for _, event := range events.Items[:outLimit] {
		ts := eventTimestamp(event)
		out = append(out, Event{
			Reason:    event.Reason,
			Message:   strings.ReplaceAll(event.Message, "\n", " "),
			Timestamp: ts,
		})
	}
	return out, nil
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

// RecentEventsSince fetches Kubernetes events for the specified HPA that
// occurred after the given time. Events are returned in ascending
// chronological order (oldest first). The Kubernetes Events API does not
// support time-range field selectors, so a generous batch is fetched and
// filtered client-side.
func RecentEventsSince(ctx context.Context, client kubernetes.Interface, namespace, name string, since time.Time) ([]Event, error) {
	events, err := RecentEvents(ctx, client, namespace, name, 500)
	if err != nil {
		return nil, err
	}
	filtered := make([]Event, 0, len(events))
	for _, e := range events {
		if !e.Timestamp.IsZero() && !e.Timestamp.Before(since) {
			filtered = append(filtered, e)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.Before(filtered[j].Timestamp)
	})
	return filtered, nil
}
