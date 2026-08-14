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

func TestNormalizeRescales(t *testing.T) {
	t0 := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	t2 := t0.Add(2 * time.Minute)

	tests := []struct {
		name      string
		input     []RescaleData
		want      []RescaleData
		wantEmpty bool
	}{
		{name: "nil input", input: nil, wantEmpty: true},
		{name: "empty input", input: []RescaleData{}, wantEmpty: true},
		{
			name:  "already ordered preserved",
			input: []RescaleData{{Timestamp: t0, NewSize: 2}, {Timestamp: t1, NewSize: 5}},
			want:  []RescaleData{{Timestamp: t0, NewSize: 2}, {Timestamp: t1, NewSize: 5}},
		},
		{
			name:  "out of order sorted by timestamp",
			input: []RescaleData{{Timestamp: t2, NewSize: 9}, {Timestamp: t0, NewSize: 2}, {Timestamp: t1, NewSize: 5}},
			want:  []RescaleData{{Timestamp: t0, NewSize: 2}, {Timestamp: t1, NewSize: 5}, {Timestamp: t2, NewSize: 9}},
		},
		{
			name:  "exact duplicate collapsed to one",
			input: []RescaleData{{Timestamp: t0, NewSize: 3}, {Timestamp: t0, NewSize: 3}},
			want:  []RescaleData{{Timestamp: t0, NewSize: 3}},
		},
		{
			name:  "same timestamp equal size keeps one even with other entries",
			input: []RescaleData{{Timestamp: t0, NewSize: 3}, {Timestamp: t0, NewSize: 3}, {Timestamp: t1, NewSize: 7}},
			want:  []RescaleData{{Timestamp: t0, NewSize: 3}, {Timestamp: t1, NewSize: 7}},
		},
		{
			name:      "same timestamp different size drops all ambiguous",
			input:     []RescaleData{{Timestamp: t0, NewSize: 3}, {Timestamp: t0, NewSize: 5}},
			wantEmpty: true,
		},
		{
			name: "ambiguous group dropped while others kept",
			input: []RescaleData{
				{Timestamp: t0, NewSize: 3}, {Timestamp: t0, NewSize: 5},
				{Timestamp: t1, NewSize: 9},
			},
			want: []RescaleData{{Timestamp: t1, NewSize: 9}},
		},
		{
			name:      "descending size tie-break is deterministic",
			input:     []RescaleData{{Timestamp: t0, NewSize: 8}, {Timestamp: t0, NewSize: 2}},
			wantEmpty: true, // same timestamp, differing size -> ambiguous, both dropped
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := append([]RescaleData(nil), tt.input...)
			got := NormalizeRescales(tt.input)
			if !sameRescaleSlice(tt.input, snapshot) {
				t.Fatalf("NormalizeRescales mutated its input")
			}
			if tt.wantEmpty {
				if got == nil || len(got) != 0 {
					t.Fatalf("NormalizeRescales() = %v, want empty", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("NormalizeRescales() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if !got[i].Timestamp.Equal(tt.want[i].Timestamp) || got[i].NewSize != tt.want[i].NewSize {
					t.Fatalf("NormalizeRescales()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func sameRescaleSlice(a, b []RescaleData) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Timestamp.Equal(b[i].Timestamp) || a[i].NewSize != b[i].NewSize {
			return false
		}
	}
	return true
}
