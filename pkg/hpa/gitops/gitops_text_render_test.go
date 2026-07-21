package gitops

import (
	"bytes"
	"strings"
	"testing"
)

func populatedConflict() *Conflict {
	return &Conflict{
		Namespace: "default",
		Name:      "web",
		Target:    "Deployment/web",
		Summary:   "Found 1 conflict(s), 1 warning(s)",
		Conflicts: []ConflictEntry{
			{
				Kind:          "Deployment",
				Name:          "web",
				Field:         "spec.replicas",
				ManifestValue: "3",
				LiveValue:     "8",
				HPADesired:    "8",
				Severity:      "conflict",
				Detail:        "Next GitOps sync may reset replicas from 8 to 3",
				Remediation:   "Remove spec.replicas from Deployment manifest",
			},
			{
				Kind:     "Deployment",
				Name:     "web",
				Severity: "info",
				Detail:   "Argo CD managed",
			},
		},
		Warnings: []string{"Argo CD sync may override manual changes; commit HPA adjustments to Git"},
		Patches:  []string{"spec.replicas: null # remove from Deployment/web to allow HPA control"},
	}
}

func TestWriteConflictText(t *testing.T) {
	t.Run("nil report", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteConflictText(&buf, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "No data available") {
			t.Errorf("expected no-data message, got %q", buf.String())
		}
	})

	t.Run("populated report", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteConflictText(&buf, populatedConflict()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := buf.String()
		for _, want := range []string{
			"default/web", "spec.replicas conflict", "Manifest:", "HPA desired:", "Live:",
			"Impact:", "Remediation:", "Warnings:", "Suggested manifest patches:",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, out)
			}
		}
	})

	t.Run("no conflicts or warnings", func(t *testing.T) {
		var buf bytes.Buffer
		report := &Conflict{Namespace: "default", Name: "web", Target: "Deployment/web", Summary: "clean"}
		if err := WriteConflictText(&buf, report); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "No conflicts or warnings detected") {
			t.Errorf("expected clean message, got %q", buf.String())
		}
	})
}

func TestWriteConflictMarkdown(t *testing.T) {
	t.Run("nil report", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteConflictMarkdown(&buf, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "No GitOps conflict data available") {
			t.Errorf("expected no-data message, got %q", buf.String())
		}
	})

	t.Run("populated report", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteConflictMarkdown(&buf, populatedConflict()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := buf.String()
		for _, want := range []string{
			"## GitOps Conflict: default/web", "### Conflicts", "**conflict**",
			"### Warnings", "### Suggested Patches", "```yaml",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("expected markdown to contain %q, got:\n%s", want, out)
			}
		}
	})

	t.Run("no conflicts or warnings", func(t *testing.T) {
		var buf bytes.Buffer
		report := &Conflict{Namespace: "default", Name: "web", Target: "Deployment/web", Summary: "clean"}
		if err := WriteConflictMarkdown(&buf, report); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "No conflicts or warnings detected") {
			t.Errorf("expected clean message, got %q", buf.String())
		}
	})
}

func TestWriteConflictHTML(t *testing.T) {
	t.Run("nil report", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteConflictHTML(&buf, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "No GitOps conflict data available") {
			t.Errorf("expected no-data message, got %q", buf.String())
		}
	})

	t.Run("populated report", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteConflictHTML(&buf, populatedConflict()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := buf.String()
		for _, want := range []string{
			"<h2>GitOps Conflict: default/web", "<h3>Conflicts</h3>", `class="conflict-error"`,
			`class="conflict-info"`, "<h3>Warnings</h3>", "<h3>Suggested Patches</h3>",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("expected HTML to contain %q, got:\n%s", want, out)
			}
		}
	})

	t.Run("no conflicts or warnings", func(t *testing.T) {
		var buf bytes.Buffer
		report := &Conflict{Namespace: "default", Name: "web", Target: "Deployment/web", Summary: "clean"}
		if err := WriteConflictHTML(&buf, report); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "No conflicts or warnings detected") {
			t.Errorf("expected clean message, got %q", buf.String())
		}
	})
}

func TestGitOpsSeverityClass(t *testing.T) {
	tests := map[string]string{
		"conflict": "conflict-error",
		"warning":  "conflict-warning",
		"info":     "conflict-info",
		"":         "conflict-info",
	}
	for severity, want := range tests {
		if got := gitOpsSeverityClass(severity); got != want {
			t.Errorf("gitOpsSeverityClass(%q) = %q, want %q", severity, got, want)
		}
	}
}
