package hpa

import (
	"context"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

type Event struct {
	Reason  string `json:"reason" yaml:"reason"`
	Message string `json:"message" yaml:"message"`
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
		out = append(out, Event{
			Reason:  event.Reason,
			Message: strings.ReplaceAll(event.Message, "\n", " "),
		})
	}
	return out, nil
}

// EventFromCore converts a corev1.Event to a simplified Event struct.
func EventFromCore(event corev1.Event) Event {
	return Event{
		Reason:  event.Reason,
		Message: strings.ReplaceAll(event.Message, "\n", " "),
	}
}
