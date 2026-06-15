package hpa

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEventFromCore_WithTimestamp(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	coreEvent := corev1.Event{
		Reason:        "SuccessfulRescale",
		Message:       "New size: 5",
		LastTimestamp: metav1.NewTime(ts),
	}

	event := EventFromCore(coreEvent)
	if event.Timestamp.IsZero() {
		t.Error("expected Timestamp to be set from LastTimestamp")
	}
	if !event.Timestamp.Equal(ts) {
		t.Errorf("expected Timestamp=%v, got %v", ts, event.Timestamp)
	}
}
