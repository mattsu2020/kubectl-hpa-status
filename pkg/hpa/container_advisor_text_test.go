package hpa

import (
	"strings"
	"testing"
)

func TestDominantSuffix(t *testing.T) {
	if got := dominantSuffix(true); got != " (dominant)" {
		t.Errorf("dominantSuffix(true) = %q", got)
	}
	if got := dominantSuffix(false); got != "" {
		t.Errorf("dominantSuffix(false) = %q", got)
	}
}

func TestPercentOrUnknown(t *testing.T) {
	tests := []struct {
		value int
		want  string
	}{
		{-1, "unknown"},
		{0, "0%"},
		{73, "73%"},
	}
	for _, tc := range tests {
		if got := percentOrUnknown(tc.value); got != tc.want {
			t.Errorf("percentOrUnknown(%d) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestAppendContainerAdvisorText_NilResult(t *testing.T) {
	var buf []byte
	AppendContainerAdvisorText(&buf, nil, resolveLabels(nil))
	if len(buf) != 0 {
		t.Fatalf("expected empty buffer for nil result, got %q", buf)
	}
}

func TestAppendContainerAdvisorText_FullResult(t *testing.T) {
	lbls := resolveLabels(nil)
	result := &ContainerAdvisorResult{
		Finding:         "2 containers share one cpu metric",
		Risk:            "noisy neighbor may starve the API container",
		SuggestedMetric: "- type: ContainerResource\n  name: cpu",
		Confidence:      ConfidenceHigh,
		NextAction:      "switch to ContainerResource metrics",
		ContainerUsageHints: []ContainerUsageHint{
			{Container: "api", CPUPercent: 82, MemoryPercent: -1, Dominant: true},
			{Container: "sidecar", CPUPercent: -1, MemoryPercent: 12},
		},
	}

	var buf []byte
	AppendContainerAdvisorText(&buf, result, lbls)
	out := string(buf)
	for _, want := range []string{
		lbls.ContainerAdvisor + ":",
		"2 containers share one cpu metric",
		"noisy neighbor may starve the API container",
		"- type: ContainerResource",
		"Confidence: " + string(ConfidenceHigh),
		"api (dominant): cpu=82%, memory=unknown",
		"sidecar: cpu=unknown, memory=12%",
		"switch to ContainerResource metrics",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
