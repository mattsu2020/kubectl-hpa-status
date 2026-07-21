package hpa

import (
	"strings"
	"testing"
)

func float64Ptr(v float64) *float64 { return &v }

func TestAppendStructuredDecisionTraceText_Nil(t *testing.T) {
	var buf []byte
	AppendStructuredDecisionTraceText(&buf, nil, nil)
	if len(buf) != 0 {
		t.Fatalf("expected empty buffer for nil trace, got %q", buf)
	}
}

func TestAppendStructuredDecisionTraceText_Full(t *testing.T) {
	trace := &StructuredDecisionTrace{
		SchemaVersion:          "1.0",
		Namespace:              "default",
		Name:                   "web",
		CurrentReplicas:        3,
		VisibleDesiredReplicas: 4,
		EstimatedRawDesired:    5,
		MinReplicas:            2,
		MaxReplicas:            10,
		Metrics: []StructuredMetricTrace{{
			Name:               "cpu",
			Type:               "Resource",
			Current:            "90",
			Target:             "70",
			Ratio:              float64Ptr(1.286),
			DesiredDirection:   "up",
			EffectiveTolerance: 0.1,
		}},
		WinnerMetric:     "cpu",
		WinnerConfidence: ConfidenceHigh,
		LimitClamp:       "none",
		Summary:          "cpu above target",
	}

	var buf []byte
	AppendStructuredDecisionTraceText(&buf, trace, nil)
	out := string(buf)
	for _, want := range []string{
		"schema: 1.0",
		"HPA: default/web",
		"replicas: current=3 desired=4 min=2 max=10",
		"estimated raw desired: 5",
		"cpu",
		"ratio=1.286",
		"Winner metric: cpu",
		"Limit clamp: none",
		"Summary: cpu above target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
