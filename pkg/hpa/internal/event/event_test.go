package event

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFromCore_PrefersLastTimestamp(t *testing.T) {
	last := metav1.Date(2024, time.February, 3, 10, 0, 0, 0, time.UTC)
	eventTime := metav1.NewMicroTime(time.Date(2024, time.February, 3, 9, 0, 0, 0, time.UTC))
	ce := corev1.Event{
		Reason:        "SuccessfulRescale",
		Message:       "new size: 5\nsecond line",
		LastTimestamp: last,
		EventTime:     eventTime,
	}

	got := FromCore(ce)
	if got.Reason != "SuccessfulRescale" {
		t.Fatalf("Reason = %q, want SuccessfulRescale", got.Reason)
	}
	if got.Message != "new size: 5 second line" {
		t.Fatalf("Message = %q, want newlines replaced with spaces", got.Message)
	}
	if !got.Timestamp.Equal(last.Time) {
		t.Fatalf("Timestamp = %v, want LastTimestamp %v", got.Timestamp, last.Time)
	}
}

func TestFromCore_FallsBackToEventTime(t *testing.T) {
	eventTime := metav1.NewMicroTime(time.Date(2024, time.February, 3, 9, 0, 0, 0, time.UTC))
	ce := corev1.Event{EventTime: eventTime}

	got := FromCore(ce)
	if !got.Timestamp.Equal(eventTime.Time) {
		t.Fatalf("Timestamp = %v, want EventTime %v", got.Timestamp, eventTime.Time)
	}
}

func TestFromCore_ZeroTimestampWhenAbsent(t *testing.T) {
	got := FromCore(corev1.Event{})
	if !got.Timestamp.IsZero() {
		t.Fatalf("Timestamp = %v, want zero", got.Timestamp)
	}
}

func TestFromCoreSlice(t *testing.T) {
	t.Run("empty input returns empty slice", func(t *testing.T) {
		got := FromCoreSlice(nil)
		if got == nil {
			t.Fatalf("expected non-nil empty slice")
		}
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})
	t.Run("preserves order and content", func(t *testing.T) {
		ts := metav1.Date(2024, time.March, 4, 5, 6, 7, 0, time.UTC)
		ets := metav1.NewMicroTime(time.Date(2024, time.March, 4, 5, 6, 7, 0, time.UTC))
		in := []corev1.Event{
			{Reason: "a", LastTimestamp: ts},
			{Reason: "b", EventTime: ets},
		}
		got := FromCoreSlice(in)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0].Reason != "a" || got[1].Reason != "b" {
			t.Fatalf("order not preserved: %v", got)
		}
		// Mutating the result must not affect a re-conversion of the input.
		got[0].Reason = "mutated"
		if in[0].Reason != "a" {
			t.Fatalf("input slice was mutated through shared backing array")
		}
	})
}
