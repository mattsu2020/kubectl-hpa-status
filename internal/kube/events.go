package kube

import (
	"context"
	"errors"
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

// EventObject identifies an observed object. UID prevents mixing events from
// earlier incarnations; an empty UID permits kind-and-name matching only.
type EventObject struct {
	Name string
	Kind string
	UID  types.UID
}

// FetchRecentEventsForObjects is the legacy name-only collector.
func FetchRecentEventsForObjects(ctx context.Context, client kubernetes.Interface, namespace string, names []string, limit int) ([]EventInfo, error) {
	refs := make([]EventObject, 0, len(names))
	for _, name := range names {
		refs = append(refs, EventObject{Name: name})
	}
	return FetchRecentEventsForIdentities(ctx, client, namespace, refs, limit)
}

// FetchRecentEventsForIdentities returns recent events for observed identities.
// It retains at most limit matching events while paging through the API.
func FetchRecentEventsForIdentities(ctx context.Context, client kubernetes.Interface, namespace string, refs []EventObject, limit int) ([]EventInfo, error) {
	if limit <= 0 {
		return nil, nil
	}
	seen := make(map[EventObject]bool)
	var result []EventInfo
	var failures []error
	for _, ref := range refs {
		if ref.Name == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		selectors := []fields.Selector{fields.OneTermEqualSelector("involvedObject.name", ref.Name)}
		if ref.Kind != "" {
			selectors = append(selectors, fields.OneTermEqualSelector("involvedObject.kind", ref.Kind))
		}
		if ref.UID != "" {
			selectors = append(selectors, fields.OneTermEqualSelector("involvedObject.uid", string(ref.UID)))
		}
		err := visitCoreEvents(ctx, client, namespace, fields.AndSelectors(selectors...).String(), func(events []corev1.Event) error {
			for _, event := range events {
				obj := event.InvolvedObject
				if obj.Name != ref.Name || (ref.Kind != "" && obj.Kind != ref.Kind) || (ref.UID != "" && obj.UID != ref.UID) {
					continue
				}
				item := EventInfo{Reason: event.Reason, Message: strings.ReplaceAll(event.Message, "\n", " "), Timestamp: coreEventTimestamp(event)}
				pos := sort.Search(len(result), func(i int) bool { return item.Timestamp.After(result[i].Timestamp) })
				if pos >= limit {
					continue
				}
				result = append(result, EventInfo{})
				copy(result[pos+1:], result[pos:])
				result[pos] = item
				if len(result) > limit {
					result = result[:limit]
				}
			}
			return nil
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("list events for %s/%s: %w", namespace, ref.Name, err))
		}
	}
	return result, errors.Join(failures...)
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
	var filtered []corev1.Event
	err := visitCoreEvents(ctx, client, namespace, selector.String(), func(events []corev1.Event) error {
		for _, event := range events {
			if event.InvolvedObject.Name != name || !strings.EqualFold(event.InvolvedObject.Kind, "HorizontalPodAutoscaler") || (uid != "" && event.InvolvedObject.UID != uid) {
				continue
			}
			pos := sort.Search(len(filtered), func(i int) bool { return coreEventTimestamp(event).After(coreEventTimestamp(filtered[i])) })
			if int64(pos) >= limit {
				continue
			}
			filtered = append(filtered, corev1.Event{})
			copy(filtered[pos+1:], filtered[pos:])
			filtered[pos] = event
			if int64(len(filtered)) > limit {
				filtered = filtered[:len(filtered)-1]
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list HPA events: %w", err)
	}
	return filtered, nil
}

func visitCoreEvents(ctx context.Context, client kubernetes.Interface, namespace, selector string, visit func([]corev1.Event) error) error {
	return visitListPages(ctx, metav1.ListOptions{FieldSelector: selector, Limit: eventsSinceFetchLimit}, func(ctx context.Context, page metav1.ListOptions) ([]corev1.Event, string, error) {
		list, err := client.CoreV1().Events(namespace).List(ctx, page)
		if err != nil {
			return nil, "", err
		}
		return list.Items, list.Continue, nil
	}, visit)
}

// FetchRecentHPAEventsSince fetches Kubernetes events for the specified HPA
// that occurred at or after the given time, returned in ascending chronological
// order (oldest first). The Events API does not support time-range field
// selectors, so every page of matching events is fetched and the time filter is
// applied client-side BEFORE any cap. Limiting first would silently drop the
// oldest in-window events once the fetch limit was exceeded; here the caller
// gets every event in the requested window.
func FetchRecentHPAEventsSince(ctx context.Context, client kubernetes.Interface, namespace, name string, since time.Time) ([]corev1.Event, error) {
	return fetchRecentHPAEventsSince(ctx, client, namespace, name, "", since)
}

// FetchRecentHPAEventsForObjectSince limits history to the current HPA identity.
func FetchRecentHPAEventsForObjectSince(ctx context.Context, client kubernetes.Interface, hpa *autoscalingv2.HorizontalPodAutoscaler, since time.Time) ([]corev1.Event, error) {
	if hpa == nil {
		return nil, fmt.Errorf("fetch HPA events: HPA is nil")
	}
	return fetchRecentHPAEventsSince(ctx, client, hpa.Namespace, hpa.Name, hpa.UID, since)
}

func fetchRecentHPAEventsSince(ctx context.Context, client kubernetes.Interface, namespace, name string, uid types.UID, since time.Time) ([]corev1.Event, error) {
	selectors := []fields.Selector{fields.OneTermEqualSelector("involvedObject.name", name), fields.OneTermEqualSelector("involvedObject.kind", "HorizontalPodAutoscaler")}
	if uid != "" {
		selectors = append(selectors, fields.OneTermEqualSelector("involvedObject.uid", string(uid)))
	}
	var filtered []corev1.Event
	err := visitCoreEvents(ctx, client, namespace, fields.AndSelectors(selectors...).String(), func(events []corev1.Event) error {
		for _, event := range events {
			obj := event.InvolvedObject
			ts := coreEventTimestamp(event)
			if obj.Name == name && strings.EqualFold(obj.Kind, "HorizontalPodAutoscaler") && (uid == "" || obj.UID == uid) && !ts.IsZero() && !ts.Before(since) {
				filtered = append(filtered, event)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list HPA events: %w", err)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return coreEventTimestamp(filtered[i]).Before(coreEventTimestamp(filtered[j])) })
	return filtered, nil
}
