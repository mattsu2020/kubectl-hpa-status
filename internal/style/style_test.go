package style

import (
	"strings"
	"testing"
)

func TestNewTheme_DisabledProducesPlain(t *testing.T) {
	theme := NewTheme(false)
	if theme.Enabled() {
		t.Fatal("disabled theme should not be enabled")
	}
	for _, label := range []string{"OK", "ERROR", "LIMITED"} {
		got := theme.HealthLabel(label)
		if strings.Contains(got, "\x1b[") {
			t.Errorf("expected no ANSI in HealthLabel(%q), got %q", label, got)
		}
	}
}

func TestNewTheme_EnabledProducesANSI(t *testing.T) {
	theme := NewTheme(true)
	if !theme.Enabled() {
		t.Fatal("enabled theme should be enabled")
	}
	got := theme.HealthLabel("ERROR")
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("expected ANSI escape codes in HealthLabel(ERROR), got %q", got)
	}
	if !strings.Contains(got, "ERROR") {
		t.Errorf("expected ERROR label, got %q", got)
	}
}

func TestHealthLabel_Markers(t *testing.T) {
	theme := NewTheme(false)
	if got := theme.HealthLabel("OK"); got != "🟢 Healthy" {
		t.Errorf("expected healthy label, got %q", got)
	}
	if got := theme.HealthLabel("ERROR"); got != "🔴 ERROR" {
		t.Errorf("expected error label, got %q", got)
	}
	if got := theme.HealthLabel("LIMITED"); got != "🔴 ScalingLimited" {
		t.Errorf("expected limited label, got %q", got)
	}
}

func TestIssue_DisabledReturnsPlain(t *testing.T) {
	theme := NewTheme(false)
	got := theme.Issue("ERROR: FailedGetResourceMetric", "ERROR")
	if got != "ERROR: FailedGetResourceMetric" {
		t.Errorf("expected plain issue, got %q", got)
	}
}

func TestIssue_EmptyReturnsEmpty(t *testing.T) {
	theme := NewTheme(true)
	got := theme.Issue("", "ERROR")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestConditionStatus_ScalingActive(t *testing.T) {
	theme := NewTheme(true)
	got := theme.ConditionStatus("ScalingActive", "False")
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("expected ANSI for ScalingActive=False, got %q", got)
	}

	got = theme.ConditionStatus("ScalingActive", "True")
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("expected ANSI for ScalingActive=True, got %q", got)
	}
}

func TestConditionStatus_Disabled(t *testing.T) {
	theme := NewTheme(false)
	got := theme.ConditionStatus("ScalingActive", "False")
	if got != "False" {
		t.Errorf("expected plain text, got %q", got)
	}
}

func TestSummaryColor(t *testing.T) {
	theme := NewTheme(true)
	tests := []struct {
		summary  string
		wantANSI bool
	}{
		{"HPA cannot currently compute a scaling recommendation.", true},
		{"HPA currently wants to scale up.", true},
		{"HPA is at maxReplicas.", true},
		{"HPA currently keeps the replica count unchanged.", false},
	}
	for _, tt := range tests {
		got := theme.SummaryColor(tt.summary)
		if strings.Contains(got, "\x1b[") != tt.wantANSI {
			t.Errorf("SummaryColor(%q) ANSI=%v, want %v", tt.summary, strings.Contains(got, "\x1b["), tt.wantANSI)
		}
	}
}

func TestMetricNote(t *testing.T) {
	theme := NewTheme(true)
	above := theme.MetricNote("current value is above target")
	if !strings.Contains(above, "\x1b[") {
		t.Error("expected ANSI for 'above target'")
	}
	below := theme.MetricNote("current value is below target")
	if !strings.Contains(below, "\x1b[") {
		t.Error("expected ANSI for 'below target'")
	}
	plain := theme.MetricNote("")
	if plain != "" {
		t.Errorf("expected empty, got %q", plain)
	}
}

func TestInterpretationLine(t *testing.T) {
	theme := NewTheme(true)
	medium := theme.InterpretationLine("[confidence: medium] something")
	if !strings.Contains(medium, "\x1b[") {
		t.Error("expected ANSI for medium confidence")
	}
	high := theme.InterpretationLine("[confidence: high] something")
	if strings.Contains(high, "\x1b[") {
		t.Error("expected no ANSI for high confidence")
	}
}

func TestScreenClear(t *testing.T) {
	enabled := NewTheme(true)
	if got := enabled.ScreenClear(); got != "\x1b[2J\x1b[H" {
		t.Errorf("expected clear sequence, got %q", got)
	}
	disabled := NewTheme(false)
	if got := disabled.ScreenClear(); got != "" {
		t.Errorf("expected empty for disabled, got %q", got)
	}
}

func TestReplicaHighlight(t *testing.T) {
	theme := NewTheme(true)
	got := theme.ReplicaHighlight(5, true)
	if !strings.Contains(got, "\x1b[") {
		t.Error("expected ANSI when highlighted")
	}
	got = theme.ReplicaHighlight(5, false)
	if strings.Contains(got, "\x1b[") {
		t.Error("expected no ANSI when not highlighted")
	}
}
