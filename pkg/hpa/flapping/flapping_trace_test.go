package flapping

import (
	"reflect"
	"testing"
)

// desiredSeries builds a TraceInput whose desired-replica series follows
// values in order, so tests can express flapping patterns compactly.
func desiredSeries(namespace, name string, values ...int32) TraceInput {
	return TraceInput{Namespace: namespace, Name: name, Desired: values}
}

func TestAnalyzeTrace_EmptyInput(t *testing.T) {
	report := AnalyzeTrace(TraceInput{})
	if report.Level != "LOW" || report.Snapshots != 0 || report.DesiredChanges != 0 || report.DirectionFlips != 0 {
		t.Fatalf("empty input report = %+v, want zeroed LOW report", report)
	}
	if report.Suggestions != nil {
		t.Fatalf("empty input should carry no suggestions, got %v", report.Suggestions)
	}
}

func TestAnalyzeTrace_SteadySeriesIsLow(t *testing.T) {
	report := AnalyzeTrace(desiredSeries("ns", "web", 3, 3, 3, 3))
	if report.Level != "LOW" {
		t.Fatalf("steady series level = %s, want LOW", report.Level)
	}
	if report.DesiredChanges != 0 || report.DirectionFlips != 0 {
		t.Fatalf("steady series counts = changes %d flips %d, want 0/0", report.DesiredChanges, report.DirectionFlips)
	}
	if report.ReplicaMin != 3 || report.ReplicaMax != 3 {
		t.Fatalf("replica range = (%d, %d), want (3, 3)", report.ReplicaMin, report.ReplicaMax)
	}
}

func TestAnalyzeTrace_LevelBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		desired []int32
		want    string
	}{
		// A single monotonic change has no direction flip and fewer than
		// flappingMediumDesiredChanges changes, so it stays LOW.
		{name: "single change stays low", desired: []int32{2, 4}, want: "LOW"},
		// Four monotonic changes reach flappingMediumDesiredChanges.
		{name: "medium by changes", desired: []int32{1, 2, 3, 4, 5}, want: "MEDIUM"},
		// One direction reversal is enough for MEDIUM (flips > 0).
		{name: "medium by flip", desired: []int32{2, 6, 3}, want: "MEDIUM"},
		// Three direction flips reach flappingHighDirectionFlips.
		{name: "high by flips", desired: []int32{2, 6, 3, 7, 4}, want: "HIGH"},
		// Eight monotonic changes reach flappingHighDesiredChanges.
		{name: "high by changes", desired: []int32{1, 2, 3, 4, 5, 6, 7, 8, 9}, want: "HIGH"},
		// Six flips reach flappingCriticalDirectionFlips.
		{name: "critical by flips", desired: []int32{2, 6, 2, 6, 2, 6, 2, 6}, want: "CRITICAL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := AnalyzeTrace(desiredSeries("ns", "web", tc.desired...))
			if report.Level != tc.want {
				t.Fatalf("level = %s (changes %d, flips %d), want %s",
					report.Level, report.DesiredChanges, report.DirectionFlips, tc.want)
			}
		})
	}
}

func TestAnalyzeTrace_SuggestionsOnlyWhenFlapping(t *testing.T) {
	low := AnalyzeTrace(desiredSeries("ns", "web", 3, 3, 3))
	if len(low.Suggestions) != 0 {
		t.Fatalf("LOW report should have no suggestions, got %v", low.Suggestions)
	}
	high := AnalyzeTrace(desiredSeries("ns", "web", 2, 6, 3, 7, 4))
	if len(high.Suggestions) == 0 {
		t.Fatal("non-LOW report should carry remediation suggestions")
	}
}

func TestAnalyzeTrace_DoesNotMutateInput(t *testing.T) {
	in := desiredSeries("ns", "web", 4, 9, 2)
	original := append([]int32(nil), in.Desired...)
	_ = AnalyzeTrace(in)
	if !reflect.DeepEqual(in.Desired, original) {
		t.Fatalf("AnalyzeTrace mutated input: %v -> %v", original, in.Desired)
	}
}
