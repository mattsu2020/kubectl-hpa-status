package kube

import (
	"context"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EventInfo is a compact Kubernetes Event view for workload path analysis.
type EventInfo struct {
	Reason    string
	Message   string
	Timestamp time.Time
}

// FetchRecentEventsForObjects fetches namespace events and returns recent
// events whose involved object name is in objectNames.
func FetchRecentEventsForObjects(ctx context.Context, client kubernetes.Interface, namespace string, objectNames []string, limit int) []EventInfo {
	if len(objectNames) == 0 || limit <= 0 {
		return nil
	}
	names := make(map[string]struct{}, len(objectNames))
	for _, name := range objectNames {
		if name != "" {
			names[name] = struct{}{}
		}
	}
	if len(names) == 0 {
		return nil
	}

	events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	var result []EventInfo
	for _, event := range events.Items {
		if _, ok := names[event.InvolvedObject.Name]; !ok {
			continue
		}
		result = append(result, EventInfo{
			Reason:    event.Reason,
			Message:   strings.ReplaceAll(event.Message, "\n", " "),
			Timestamp: coreEventTimestamp(event),
		})
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
