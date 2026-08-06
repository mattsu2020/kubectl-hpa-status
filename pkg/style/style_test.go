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
		got := theme.HealthLabel(label, 100)
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
	got := theme.HealthLabel("ERROR", 50)
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("expected ANSI escape codes in HealthLabel(ERROR), got %q", got)
	}
	if !strings.Contains(got, "ERROR") {
		t.Errorf("expected ERROR label, got %q", got)
	}
}

func TestHealthLabel_Markers(t *testing.T) {
	theme := NewTheme(false)
	if got := theme.HealthLabel("OK", 100); got != "🟢 Healthy (Excellent)" {
		t.Errorf("expected healthy label, got %q", got)
	}
	if got := theme.HealthLabel("ERROR", 50); got != "🔴 ERROR (Critical)" {
		t.Errorf("expected error label, got %q", got)
	}
	if got := theme.HealthLabel("LIMITED", 75); got != "🔴 ScalingLimited (Warning)" {
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

func TestSummaryColorForKeyStylesTranslatedText(t *testing.T) {
	theme := NewTheme(true)
	got := theme.SummaryColorForKey("現在スケールアップを要求しています", "dir_scale_up")
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("translated summary was not styled: %q", got)
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
	estimated := theme.InterpretationLine("[estimated] something")
	if !strings.Contains(estimated, "\x1b[") {
		t.Error("expected ANSI for estimated classification")
	}
	observed := theme.InterpretationLine("[observed] something")
	if strings.Contains(observed, "\x1b[") {
		t.Error("expected no ANSI for observed classification")
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

func TestIssue_EnabledColorsByHealth(t *testing.T) {
	theme := NewTheme(true)
	// ERROR and LIMITED render styled output.
	if got := theme.Issue("something failed", "ERROR"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("Issue(ERROR) expected ANSI, got %q", got)
	}
	if got := theme.Issue("scaling limited", "LIMITED"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("Issue(LIMITED) expected ANSI, got %q", got)
	}
	// Unknown health leaves the string unstyled.
	if got := theme.Issue("plain note", "OK"); got != "plain note" {
		t.Fatalf("Issue(OK) = %q, want unchanged", got)
	}
}

func TestConditionStatus_EnabledBranches(t *testing.T) {
	theme := NewTheme(true)
	// ScalingActive True -> success style; not True -> error style.
	if got := theme.ConditionStatus("ScalingActive", "True"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("ConditionStatus(ScalingActive, True) expected ANSI, got %q", got)
	}
	if got := theme.ConditionStatus("ScalingActive", "False"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("ConditionStatus(ScalingActive, False) expected ANSI, got %q", got)
	}
	// ScalingLimited True -> warning style; otherwise success.
	if got := theme.ConditionStatus("ScalingLimited", "True"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("ConditionStatus(ScalingLimited, True) expected ANSI, got %q", got)
	}
	if got := theme.ConditionStatus("ScalingLimited", "False"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("ConditionStatus(ScalingLimited, False) expected ANSI, got %q", got)
	}
	// Unknown condition type is returned unchanged.
	if got := theme.ConditionStatus("SomeOther", "True"); got != "True" {
		t.Fatalf("ConditionStatus(other) = %q, want unchanged", got)
	}
}

func TestSummaryColorForKey_Enabled(t *testing.T) {
	theme := NewTheme(true)
	errorKeys := []string{"dir_unavailable", "dir_inactive", "dir_no_recommendation"}
	for _, k := range errorKeys {
		if got := theme.SummaryColorForKey("msg", k); !strings.Contains(got, "\x1b[") {
			t.Fatalf("SummaryColorForKey(%q) expected ANSI, got %q", k, got)
		}
	}
	warningKeys := []string{"dir_scale_up", "dir_scale_to_zero", "dir_scaled_to_zero", "dir_at_max", "dir_at_min", "dir_at_min_scale_to_zero"}
	for _, k := range warningKeys {
		if got := theme.SummaryColorForKey("msg", k); !strings.Contains(got, "\x1b[") {
			t.Fatalf("SummaryColorForKey(%q) expected ANSI, got %q", k, got)
		}
	}
	// Neutral keys return the plain summary.
	if got := theme.SummaryColorForKey("unchanged", "dir_unchanged"); got != "unchanged" {
		t.Fatalf("SummaryColorForKey(dir_unchanged) = %q, want unchanged", got)
	}
	// Unknown key falls through to SummaryColor (empty summary -> plain).
	if got := theme.SummaryColorForKey("plain", "unknown-key"); got != "plain" {
		t.Fatalf("SummaryColorForKey(unknown) = %q, want plain", got)
	}
}

func TestActionLine_EnabledReturnsWarningStyled(t *testing.T) {
	// Disabled theme returns the line unchanged.
	if got := NewTheme(false).ActionLine("run kubectl apply"); got != "run kubectl apply" {
		t.Fatalf("ActionLine disabled = %q, want unchanged", got)
	}
	// Enabled theme styles the line.
	if got := NewTheme(true).ActionLine("apply this"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("ActionLine enabled expected ANSI, got %q", got)
	}
}
