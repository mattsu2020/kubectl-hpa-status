package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
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
// involvedObject.name field selector and paginates the filtered result so the
// API server filters server-side without truncating a newer event that happens
// to be on a later page. objectNames is small in practice (the HPA plus its
// workload chain), so per-name queries stay cheaper than one namespace-wide list.
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
		events, err := listCoreEventsBySelector(ctx, client, namespace,
			fields.OneTermEqualSelector("involvedObject.name", name).String())
		if err != nil {
			// Best-effort: an Events List failure (RBAC denial on events, API
			// server hiccup) is indistinguishable from "no events" to the
			// caller. The status report degrades to omitting the events
			// section rather than failing the whole command.
			continue
		}
		for _, event := range events {
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
	if event.Series != nil && !event.Series.LastObservedTime.IsZero() {
		return event.Series.LastObservedTime.Time
	}
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	if !event.FirstTimestamp.IsZero() {
		return event.FirstTimestamp.Time
	}
	if !event.CreationTimestamp.IsZero() {
		return event.CreationTimestamp.Time
	}
	return time.Time{}
}

// FetchRecentHPAEvents fetches recent Kubernetes events for the specified HPA,
// returning the raw corev1.Event values sorted by LastTimestamp descending
// (most recent first). Callers (typically in cmd/) convert to pkg/hpa.Event
// via hpaanalysis.EventFromCore.
func FetchRecentHPAEvents(ctx context.Context, client kubernetes.Interface, namespace, name string, limit int64) ([]corev1.Event, error) {
	return fetchRecentHPAEvents(ctx, client, namespace, name, "", limit)
}

// FetchRecentHPAEventsForObject fetches events for one concrete HPA identity.
// Including kind and UID prevents events from an older HPA incarnation or a
// different object with the same name from being mixed into the report.
func FetchRecentHPAEventsForObject(ctx context.Context, client kubernetes.Interface, hpa *autoscalingv2.HorizontalPodAutoscaler, limit int64) ([]corev1.Event, error) {
	if hpa == nil {
		return nil, fmt.Errorf("fetch HPA events: HPA is nil")
	}
	return fetchRecentHPAEvents(ctx, client, hpa.Namespace, hpa.Name, hpa.UID, limit)
}

func fetchRecentHPAEvents(ctx context.Context, client kubernetes.Interface, namespace, name string, uid types.UID, limit int64) ([]corev1.Event, error) {
	if limit <= 0 {
		return nil, nil
	}
	selectors := []fields.Selector{
		fields.OneTermEqualSelector("involvedObject.name", name),
		fields.OneTermEqualSelector("involvedObject.kind", "HorizontalPodAutoscaler"),
	}
	if uid != "" {
		selectors = append(selectors, fields.OneTermEqualSelector("involvedObject.uid", string(uid)))
	}
	selector := fields.AndSelectors(selectors...)
	events, err := listCoreEventsBySelector(ctx, client, namespace, selector.String())
	if err != nil {
		return nil, fmt.Errorf("list HPA events: %w", err)
	}

	filtered := events[:0]
	for i := range events {
		event := events[i]
		if event.InvolvedObject.Name != name || !strings.EqualFold(event.InvolvedObject.Kind, "HorizontalPodAutoscaler") {
			continue
		}
		if uid != "" && event.InvolvedObject.UID != uid {
			continue
		}
		filtered = append(filtered, event)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return coreEventTimestamp(filtered[i]).After(coreEventTimestamp(filtered[j]))
	})

	outLimit := len(filtered)
	if int64(outLimit) > limit {
		outLimit = int(limit)
	}
	return filtered[:outLimit], nil
}

func listCoreEventsBySelector(ctx context.Context, client kubernetes.Interface, namespace, selector string) ([]corev1.Event, error) {
	var result []corev1.Event
	continueToken := ""
	for {
		page, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
			FieldSelector: selector,
			Limit:         eventsSinceFetchLimit,
			Continue:      continueToken,
		})
		if err != nil {
			return nil, err
		}
		result = append(result, page.Items...)
		if page.Continue == "" {
			return result, nil
		}
		if page.Continue == continueToken {
			return nil, fmt.Errorf("events pagination returned repeated continue token %q", continueToken)
		}
		continueToken = page.Continue
	}
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
