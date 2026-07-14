package hpa

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func TestNewListItemHighlightsImplicitMaxReplicasLimit(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 10
	hpa.Status.DesiredReplicas = 10
	hpa.Spec.MaxReplicas = 10

	got := NewListItem(Analyze(hpa, false))
	if got.Health != "LIMITED" {
		t.Fatalf("expected LIMITED health, got %s", got.Health)
	}
	if got.Issue != "LIMITED: maxReplicas" {
		t.Fatalf("unexpected issue: %s", got.Issue)
	}
}

func TestWriteListTextVisuallyHighlightsProblems(t *testing.T) {
	report := ListReport{Items: []ListItem{
		{Namespace: "default", Name: "web", Current: 2, Desired: 2, Health: "OK", Summary: "steady"},
		{Namespace: "default", Name: "api", Current: 2, Desired: 2, Health: "ERROR", Issue: "ERROR: FailedGetResourceMetric", Summary: "broken"},
		{Namespace: "default", Name: "worker", Current: 5, Desired: 5, Health: "LIMITED", Issue: "LIMITED: TooManyReplicas", Summary: "capped"},
	}}

	var out bytes.Buffer
	if err := WriteListText(&out, report, ListTextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "ERROR") {
		t.Fatalf("expected ERROR marker in %q", text)
	}
	if !strings.Contains(text, "ScalingLimited") {
		t.Fatalf("expected LIMITED marker in %q", text)
	}
}

func TestWriteListTextColorizesHealthWhenEnabled(t *testing.T) {
	report := ListReport{Items: []ListItem{
		{Namespace: "default", Name: "api", Current: 2, Desired: 2, Health: "ERROR", Issue: "ERROR: FailedGetResourceMetric", Summary: "broken"},
	}}

	var out bytes.Buffer
	if err := WriteListText(&out, report, ListTextOptions{Theme: style.NewTheme(true)}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ERROR") {
		t.Fatalf("expected ERROR marker, got %q", out.String())
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("expected ANSI escape codes in colorized output, got %q", out.String())
	}
}

func TestWriteStatusDiff_NoChanges(t *testing.T) {
	analysis := Analyze(baseHPA(), false)
	prev := analysis // copy
	state := WatchState{Previous: &prev, Current: &analysis}

	var buf bytes.Buffer
	if err := WriteStatusDiff(&buf, state, style.NewTheme(false)); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "HPA default/web") {
		t.Errorf("expected HPA header, got:\n%s", output)
	}
	// When unchanged, replicas should show without emphasis
	if !strings.Contains(output, "current=2 desired=2") {
		t.Errorf("expected plain replicas, got:\n%s", output)
	}
}

func TestWriteStatusDiff_ReplicasChanged(t *testing.T) {
	prev := Analyze(baseHPA(), false)
	prev.Current = 3
	prev.Desired = 3

	curr := Analyze(baseHPA(), false)
	curr.Current = 5
	curr.Desired = 7

	state := WatchState{Previous: &prev, Current: &curr}
	var buf bytes.Buffer
	if err := WriteStatusDiff(&buf, state, style.NewTheme(false)); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "current=5") {
		t.Errorf("expected current=5, got:\n%s", output)
	}
	if !strings.Contains(output, "desired=7") {
		t.Errorf("expected desired=7, got:\n%s", output)
	}
}

func TestWriteStatusDiff_ConditionsChanged(t *testing.T) {
	hpa := baseHPA()
	prev := Analyze(hpa, false)

	// Modify HPA to have ScalingLimited
	hpa2 := baseHPA()
	hpa2.Status.Conditions = append(hpa2.Status.Conditions,
		autoscalingv2.HorizontalPodAutoscalerCondition{
			Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas",
		},
	)
	curr := Analyze(hpa2, false)

	state := WatchState{Previous: &prev, Current: &curr}
	var buf bytes.Buffer
	if err := WriteStatusDiff(&buf, state, style.NewTheme(false)); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "ScalingLimited") {
		t.Errorf("expected ScalingLimited in diff, got:\n%s", output)
	}
}

func TestWriteStatusDiff_ConditionMessageOnlyChange(t *testing.T) {
	previous := &Analysis{Conditions: []Condition{{Type: "AbleToScale", Status: "True", Reason: "Ready", Message: "old"}}}
	current := &Analysis{Conditions: []Condition{{Type: "AbleToScale", Status: "True", Reason: "Ready", Message: "new"}}}
	var buf bytes.Buffer
	if err := WriteStatusDiff(&buf, WatchState{Previous: previous, Current: current}, style.NewTheme(false)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "(changed)") {
		t.Fatalf("message-only condition change was not detected: %q", buf.String())
	}
}

func TestMetricMapKeepsEmptyNamesAndSelectorsDistinct(t *testing.T) {
	metrics := []Metric{
		{Type: "External", Name: "queue", Selector: "team=a", Current: "1"},
		{Type: "External", Name: "queue", Selector: "team=b", Current: "2"},
		{Type: "Resource", Current: "3"},
	}
	if got := len(metricMap(metrics)); got != 3 {
		t.Fatalf("metricMap retained %d metrics, want 3", got)
	}
}

func TestWriteStatusDiff_NilPrevious(t *testing.T) {
	// Diff with nil previous should not panic; the caller should use
	// WriteStatusText for the first iteration, but WriteStatusDiff
	// should handle nil gracefully.
	curr := Analyze(baseHPA(), false)
	state := WatchState{Previous: nil, Current: &curr}

	var buf bytes.Buffer
	// This should still work even without previous
	err := WriteStatusDiff(&buf, state, style.NewTheme(false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "HPA default/web") {
		t.Errorf("expected HPA header in diff output, got:\n%s", buf.String())
	}
}

func TestWriteStatusDashboardIncludesKeyPanels(t *testing.T) {
	report := StatusReport{Analysis: Analyze(baseHPA(), true)}
	var buf bytes.Buffer
	if err := WriteStatusDashboard(&buf, report, style.NewTheme(false)); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	for _, want := range []string{"kubectl-hpa-status dashboard", "Health", "Replicas", "Conditions", "Metrics"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in dashboard output:\n%s", want, output)
		}
	}
}
