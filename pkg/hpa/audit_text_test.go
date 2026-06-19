package hpa

import (
	"bytes"
	"strings"
	"testing"
)

// 12. WriteAuditText
// ---------------------------------------------------------------------------

func TestWriteAuditText(t *testing.T) {
	t.Run("with findings outputs severity title and description", func(t *testing.T) {
		report := &AuditReport{
			Namespace: "default",
			Name:      "web-hpa",
			Target:    "Deployment/web",
			Score:     80,
			Summary:   "Found 0 critical, 1 warnings, 0 informational findings (score: 80/100)",
			Findings: []AuditFinding{
				{
					ID:          "stabilization-window",
					Title:       "Stabilization window not explicitly configured",
					Description: "scaleDown.stabilizationWindowSeconds is unset.",
					Severity:    AuditWarning,
					Category:    "stabilization",
					Current:     "unset (default 300s)",
					Recommended: "Set stabilizationWindowSeconds explicitly",
				},
			},
		}

		var buf bytes.Buffer
		if err := WriteAuditText(&buf, report, nil); err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		for _, want := range []string{
			"warning",
			"Stabilization window not explicitly configured",
			"scaleDown.stabilizationWindowSeconds is unset.",
			"stabilization-window",
			"unset (default 300s)",
			"Set stabilizationWindowSeconds explicitly",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("expected %q in output, got:\n%s", want, output)
			}
		}
	})

	t.Run("no findings outputs no findings message", func(t *testing.T) {
		report := &AuditReport{
			Namespace: "default",
			Name:      "perfect-hpa",
			Target:    "Deployment/web",
			Score:     100,
			Summary:   "No best-practice issues found.",
			Findings:  []AuditFinding{},
		}

		var buf bytes.Buffer
		if err := WriteAuditText(&buf, report, nil); err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, "No findings.") {
			t.Fatalf("expected 'No findings.' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "100/100") {
			t.Fatalf("expected score in output, got:\n%s", output)
		}
	})

	t.Run("nil provider uses English defaults", func(t *testing.T) {
		report := &AuditReport{
			Namespace: "default",
			Name:      "test-hpa",
			Target:    "Deployment/test",
			Score:     90,
			Summary:   "No best-practice issues found.",
			Findings:  []AuditFinding{},
		}

		var buf bytes.Buffer
		if err := WriteAuditText(&buf, report, nil); err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, "Target:") {
			t.Fatalf("expected English default label 'Target:' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "Compliance Score:") {
			t.Fatalf("expected English default label 'Compliance Score:' in output, got:\n%s", output)
		}
	})

	t.Run("finding with command outputs command line", func(t *testing.T) {
		report := &AuditReport{
			Namespace: "default",
			Name:      "web-hpa",
			Target:    "Deployment/web",
			Score:     80,
			Summary:   "Found 0 critical, 1 warnings, 0 informational findings (score: 80/100)",
			Findings: []AuditFinding{
				{
					ID:          "stabilization-window",
					Title:       "Stabilization window not explicitly configured",
					Description: "scaleDown.stabilizationWindowSeconds is unset.",
					Severity:    AuditWarning,
					Command:     "kubectl patch hpa web-hpa -n default --type=merge -p '{}' --dry-run=server",
				},
			},
		}

		var buf bytes.Buffer
		if err := WriteAuditText(&buf, report, nil); err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, "Command:") {
			t.Fatalf("expected 'Command:' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "kubectl patch") {
			t.Fatalf("expected kubectl command in output, got:\n%s", output)
		}
	})
}
