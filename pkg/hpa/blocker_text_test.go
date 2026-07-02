package hpa

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

func TestWriteBlockerText(t *testing.T) {
	report := &blocker.Report{
		Namespace:       "default",
		Name:            "web",
		Target:          "Deployment/web",
		HPAWantsScale:   true,
		DesiredReplicas: 12,
		ReadyReplicas:   8,
		Summary:         "HPA wants 12 replicas, but only 8 pods are Ready.",
		Blockers: []blocker.Finding{
			{ID: "pending-pods", Severity: blocker.BlockerHigh, Category: "scheduling", Message: "4 pods are Pending"},
			{ID: "failed-scheduling", Severity: blocker.BlockerHigh, Category: "scheduling", Message: "FailedScheduling: 3 pods cannot fit due to Insufficient cpu"},
			{ID: "quota-near-limit", Severity: blocker.BlockerMedium, Category: "quota", Message: `ResourceQuota "compute" requests.cpu is 95% used`},
			{ID: "metrics-healthy", Severity: blocker.BlockerInfo, Category: "info", Message: "No recent metrics retrieval errors found"},
		},
		Interpretation: "HPA appears to be working. The scale-out is blocked after the HPA decision, likely by cluster capacity or namespace quota.",
		NextCommands: []string{
			"kubectl get events --field-selector reason=FailedScheduling",
			"kubectl describe quota compute",
		},
	}

	t.Run("renders header", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteBlockerText(&buf, report, style.NewTheme(false))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		output := buf.String()
		if !strings.Contains(output, "HPA default/web") {
			t.Errorf("expected header, got:\n%s", output)
		}
		if !strings.Contains(output, "Deployment/web") {
			t.Errorf("expected target, got:\n%s", output)
		}
	})

	t.Run("renders blockers with severity", func(t *testing.T) {
		var buf bytes.Buffer
		_ = WriteBlockerText(&buf, report, style.NewTheme(false))
		output := buf.String()
		if !strings.Contains(output, "Scale-out blockers") {
			t.Errorf("expected blockers section header, got:\n%s", output)
		}
		if !strings.Contains(output, "[HIGH]") {
			t.Errorf("expected HIGH severity badge, got:\n%s", output)
		}
		if !strings.Contains(output, "[MEDIUM]") {
			t.Errorf("expected MEDIUM severity badge, got:\n%s", output)
		}
		if !strings.Contains(output, "[INFO]") {
			t.Errorf("expected INFO severity badge, got:\n%s", output)
		}
	})

	t.Run("renders interpretation", func(t *testing.T) {
		var buf bytes.Buffer
		_ = WriteBlockerText(&buf, report, style.NewTheme(false))
		output := buf.String()
		if !strings.Contains(output, "HPA appears to be working") {
			t.Errorf("expected interpretation, got:\n%s", output)
		}
	})

	t.Run("renders next commands", func(t *testing.T) {
		var buf bytes.Buffer
		_ = WriteBlockerText(&buf, report, style.NewTheme(false))
		output := buf.String()
		if !strings.Contains(output, "kubectl get events") {
			t.Errorf("expected next commands, got:\n%s", output)
		}
		if !strings.Contains(output, "kubectl describe quota") {
			t.Errorf("expected describe quota command, got:\n%s", output)
		}
	})

	t.Run("nil report", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteBlockerText(&buf, nil, style.NewTheme(false))
		if err != nil {
			t.Fatalf("unexpected error for nil report: %v", err)
		}
		if buf.String() != "" {
			t.Errorf("expected empty output for nil report, got:\n%s", buf.String())
		}
	})
}

func TestAppendBlockerText_NoBlockers(t *testing.T) {
	report := &blocker.Report{
		Namespace:       "default",
		Name:            "web",
		Summary:         "HPA has 5 replicas and is not requesting scale-out. No blockers detected.",
		HPAWantsScale:   false,
		DesiredReplicas: 5,
		ReadyReplicas:   5,
		Blockers:        nil,
	}

	var out []byte
	lbls := defaultBlockerLabels()
	AppendBlockerText(&out, report, style.NewTheme(false), lbls)

	output := string(out)
	if !strings.Contains(output, "No scale-out blockers detected") {
		t.Errorf("expected 'No scale-out blockers detected', got:\n%s", output)
	}
}

func TestWrapLines(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		maxLen int
		want   int // expected number of lines
	}{
		{"short text", "hello world", 80, 1},
		{"exact wrap", "word1 word2 word3", 12, 2},
		{"empty", "", 80, 0},
		{"single long word", "abcdefghijklmnopqrstuvwxyz", 10, 1}, // word-boundary wrap does not split words
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := wrapLines(tt.text, tt.maxLen)
			if len(lines) != tt.want {
				t.Errorf("expected %d lines, got %d: %v", tt.want, len(lines), lines)
			}
			for _, line := range lines {
				// Word-boundary wrapping does not split individual words,
				// so a single long word may exceed maxLen.
				if len(lines) > 1 && len(line) > tt.maxLen {
					t.Errorf("line exceeds maxLen: %q (%d > %d)", line, len(line), tt.maxLen)
				}
			}
		})
	}
}

func TestDefaultBlockerLabels(t *testing.T) {
	lbls := defaultBlockerLabels()
	if lbls.Blockers != "Scale-out blockers" {
		t.Errorf("expected 'Scale-out blockers', got %q", lbls.Blockers)
	}
	if lbls.Interpretation != "Interpretation" {
		t.Errorf("expected 'Interpretation', got %q", lbls.Interpretation)
	}
	if lbls.NextCommands != "Next commands" {
		t.Errorf("expected 'Next commands', got %q", lbls.NextCommands)
	}
}
