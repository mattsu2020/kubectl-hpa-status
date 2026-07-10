package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

// eventsSinceFetchLimit is the generous batch size used when fetching events
// for a time-range query. The Events API does not support time-range field
// selectors, so a broad batch is pulled and filtered client-side.
const eventsSinceFetchLimit = 500

// EventInfo is a compact Kubernetes Event view for workload path analysis.
type EventInfo struct {
	Reason    string
	Message   string
	Timestamp time.Time
}

// FetchRecentEventsForObjects fetches recent namespace events whose involved
// object name is in objectNames. Each name is queried with an
// involvedObject.name field selector plus a Limit so the API server filters
// server-side; busy namespaces are never listed in full. objectNames is small
// in practice (the HPA plus its workload chain), so per-name queries stay
// cheaper than one unbounded namespace-wide list.
func FetchRecentEventsForObjects(ctx context.Context, client kubernetes.Interface, namespace string, objectNames []string, limit int) []EventInfo {
	if len(objectNames) == 0 || limit <= 0 {
		return nil
	}
	names := make([]string, 0, len(objectNames))
	seen := make(map[string]struct{}, len(objectNames))
	for _, name := range objectNames {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)

	var result []EventInfo
	for _, name := range names {
		events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("involvedObject.name", name).String(),
			Limit:         eventsSinceFetchLimit,
		})
		if err != nil {
			// Best-effort: an Events List failure (RBAC denial on events, API
			// server hiccup) is indistinguishable from "no events" to the
			// caller. The status report degrades to omitting the events
			// section rather than failing the whole command.
			continue
		}
		for _, event := range events.Items {
			// Re-check the name client-side: the fake clientset used in tests
			// ignores field selectors and returns every event in the namespace.
			if event.InvolvedObject.Name != name {
				continue
			}
			result = append(result, EventInfo{
				Reason:    event.Reason,
				Message:   strings.ReplaceAll(event.Message, "\n", " "),
				Timestamp: coreEventTimestamp(event),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func coreEventTimestamp(event corev1.Event) time.Time {
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	return time.Time{}
}

// FetchRecentHPAEvents fetches recent Kubernetes events for the specified HPA,
// returning the raw corev1.Event values sorted by LastTimestamp descending
// (most recent first). Callers (typically in cmd/) convert to pkg/hpa.Event
// via hpaanalysis.EventFromCore.
func FetchRecentHPAEvents(ctx context.Context, client kubernetes.Interface, namespace, name string, limit int64) ([]corev1.Event, error) {
	selector := fields.OneTermEqualSelector("involvedObject.name", name)
	events, err := client.CoreV1().
		Events(namespace).
		List(ctx, metav1.ListOptions{
			FieldSelector: selector.String(),
			Limit:         limit,
		})
	if err != nil {
		return nil, fmt.Errorf("list HPA events: %w", err)
	}

	sort.Slice(events.Items, func(i, j int) bool {
		return events.Items[i].LastTimestamp.After(events.Items[j].LastTimestamp.Time)
	})

	outLimit := len(events.Items)
	if int64(outLimit) > limit {
		outLimit = int(limit)
	}
	return events.Items[:outLimit], nil
}

// FetchRecentHPAEventsSince fetches Kubernetes events for the specified HPA
// that occurred at or after the given time, returned in ascending chronological
// order (oldest first). The Events API does not support time-range field
// selectors, so a generous batch is fetched and filtered client-side.
func FetchRecentHPAEventsSince(ctx context.Context, client kubernetes.Interface, namespace, name string, since time.Time) ([]corev1.Event, error) {
	events, err := FetchRecentHPAEvents(ctx, client, namespace, name, eventsSinceFetchLimit)
	if err != nil {
		return nil, err
	}
	filtered := make([]corev1.Event, 0, len(events))
	for _, e := range events {
		ts := coreEventTimestamp(e)
		if !ts.IsZero() && !ts.Before(since) {
			filtered = append(filtered, e)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return coreEventTimestamp(filtered[i]).Before(coreEventTimestamp(filtered[j]))
	})
	return filtered, nil
}
