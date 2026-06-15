package hpa

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

func TestAppendWarmupText(t *testing.T) {
	analysis := &WarmupAnalysis{
		Summary:                "capacity_warming_up",
		EffectiveCapacityRatio: 0.4,
		DesiredReplicas:        10,
		CurrentReplicas:        10,
		ReadyPods:              4,
		AvailablePods:          3,
		AvgTimeToReadySeconds:  142,
		P95TimeToReadySeconds:  210,
		Bottlenecks: []WarmupBottleneck{
			{Type: "readiness_probe", Severity: SeverityWarning, Confidence: ConfidenceHigh, Count: 4, Message: "4 pods are Running but not Ready"},
			{Type: "image_pull", Severity: SeverityError, Confidence: ConfidenceHigh, Count: 1, Message: "1 pod has image pull issues"},
		},
		Evidence: []string{
			"avg time-to-ready: 142s",
			"4 pods are NotReady due to readiness probe failures",
			"1 pod(s) waiting: ImagePullBackOff or ErrImagePull",
		},
		Impact:             "HPA has already requested 10 replicas, but effective capacity is only 40%.",
		RecommendedActions: []string{"Check readinessProbe and startupProbe", "Check image pull latency"},
	}

	var buf []byte
	theme := style.NewTheme(false)
	lbls := resolveLabels(nil)
	AppendWarmupText(&buf, analysis, theme, lbls)
	output := string(buf)

	expected := []string{
		"Warmup Analysis",
		"capacity_warming_up",
		"desiredReplicas: 10",
		"currentReplicas: 10",
		"readyPods: 4",
		"availablePods: 3",
		"Likely bottleneck",
		"readiness_probe",
		"image_pull",
		"Evidence",
		"avg time-to-ready: 142s",
		"Impact",
		"effective capacity: 40%",
		"Recommended actions",
		"Check readinessProbe",
	}

	for _, want := range expected {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, output)
		}
	}
}

func TestAppendWarmupTextNil(t *testing.T) {
	var buf []byte
	theme := style.NewTheme(false)
	lbls := resolveLabels(nil)
	AppendWarmupText(&buf, nil, theme, lbls)
	if len(buf) != 0 {
		t.Errorf("expected empty output for nil analysis, got: %q", string(buf))
	}
}

func TestWriteWarmupText(t *testing.T) {
	analysis := &WarmupAnalysis{
		Summary:                "capacity_warming_up",
		EffectiveCapacityRatio: 0.5,
		DesiredReplicas:        4,
		CurrentReplicas:        4,
		ReadyPods:              2,
		AvailablePods:          2,
		AvgTimeToReadySeconds:  60,
		P95TimeToReadySeconds:  90,
		RecommendedActions:     []string{"Check readinessProbe"},
	}

	var buf bytes.Buffer
	theme := style.NewTheme(false)
	if err := WriteWarmupText(&buf, analysis, theme); err != nil {
		t.Fatalf("WriteWarmupText() error = %v", err)
	}
	if !strings.Contains(buf.String(), "capacity_warming_up") {
		t.Errorf("output missing summary, got: %q", buf.String())
	}
}

func TestWriteWarmupMarkdown(t *testing.T) {
	analysis := &WarmupAnalysis{
		Summary:                "capacity_warming_up",
		EffectiveCapacityRatio: 0.4,
		DesiredReplicas:        10,
		ReadyPods:              4,
		AvgTimeToReadySeconds:  142,
		P95TimeToReadySeconds:  210,
		Bottlenecks: []WarmupBottleneck{
			{Type: "readiness_probe", Severity: SeverityWarning, Confidence: ConfidenceHigh, Message: "4 pods not ready"},
		},
		RecommendedActions: []string{"Check readinessProbe"},
	}

	var buf bytes.Buffer
	if err := WriteWarmupMarkdown(&buf, analysis); err != nil {
		t.Fatalf("WriteWarmupMarkdown() error = %v", err)
	}
	output := buf.String()

	expected := []string{
		"## Warmup Analysis",
		"capacity_warming_up",
		"40%",
		"readiness_probe",
		"### Bottlenecks",
		"### Recommended Actions",
	}
	for _, want := range expected {
		if !strings.Contains(output, want) {
			t.Errorf("markdown missing %q\nGot:\n%s", want, output)
		}
	}
}

func TestWriteWarmupHTML(t *testing.T) {
	analysis := &WarmupAnalysis{
		Summary:                "capacity_warming_up",
		EffectiveCapacityRatio: 0.4,
		DesiredReplicas:        10,
		ReadyPods:              4,
		Bottlenecks: []WarmupBottleneck{
			{Type: "image_pull", Severity: SeverityError, Confidence: ConfidenceHigh, Message: "1 pod has image issues"},
		},
	}

	var buf bytes.Buffer
	if err := WriteWarmupHTML(&buf, analysis); err != nil {
		t.Fatalf("WriteWarmupHTML() error = %v", err)
	}
	output := buf.String()

	expected := []string{
		`<div class="warmup-analysis">`,
		"<h3>Warmup Analysis</h3>",
		"capacity_warming_up",
		"image_pull",
	}
	for _, want := range expected {
		if !strings.Contains(output, want) {
			t.Errorf("html missing %q\nGot:\n%s", want, output)
		}
	}
}
