package tui

import (
	"strings"
	"testing"
)

// TestFormatBottleneckSummary covers the bottleneck summary formatter that was
// only exercised indirectly through Model.View() before.
func TestFormatBottleneckSummary(t *testing.T) {
	tests := []struct {
		name            string
		highCount       int
		medCount        int
		total           int
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:         "both high and med",
			highCount:    2,
			medCount:     3,
			total:        5,
			wantContains: []string{"Bottlenecks: 5", "HIGH", "MED"},
		},
		{
			name:            "only high",
			highCount:       1,
			medCount:        0,
			total:           1,
			wantContains:    []string{"Bottlenecks: 1", "HIGH"},
			wantNotContains: []string{"MED"},
		},
		{
			name:            "only med",
			highCount:       0,
			medCount:        2,
			total:           2,
			wantContains:    []string{"Bottlenecks: 2", "MED"},
			wantNotContains: []string{"HIGH"},
		},
		{
			name:            "none",
			highCount:       0,
			medCount:        0,
			total:           0,
			wantContains:    []string{"Bottlenecks: 0"},
			wantNotContains: []string{"HIGH", "MED"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatBottleneckSummary(tc.highCount, tc.medCount, tc.total)
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("expected %q in output, got %q", want, got)
				}
			}
			for _, notWant := range tc.wantNotContains {
				if strings.Contains(got, notWant) {
					t.Errorf("expected %q NOT in output, got %q", notWant, got)
				}
			}
		})
	}
}
