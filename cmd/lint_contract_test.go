package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func validLintHPA(name string) string {
	return "apiVersion: autoscaling/v2\n" +
		"kind: HorizontalPodAutoscaler\n" +
		"metadata:\n" +
		"  name: " + name + "\n" +
		"  namespace: default\n" +
		"spec:\n" +
		"  scaleTargetRef:\n" +
		"    apiVersion: apps/v1\n" +
		"    kind: Deployment\n" +
		"    name: " + name + "\n" +
		"  minReplicas: 1\n" +
		"  maxReplicas: 10\n" +
		"  metrics:\n" +
		"  - type: Resource\n" +
		"    resource:\n" +
		"      name: cpu\n" +
		"      target:\n" +
		"        type: Utilization\n" +
		"        averageUtilization: 70\n"
}

func writeLintFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifests.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLintStructuredOutputIsSingleEnvelope(t *testing.T) {
	content := validLintHPA("web") + "--- # second document\r\n" + validLintHPA("api")
	path := writeLintFixture(t, content)

	t.Run("json", func(t *testing.T) {
		var out bytes.Buffer
		if err := runLint(context.Background(), &out, &options{}, path, "json", false, false, "error"); err != nil {
			t.Fatalf("runLint JSON: %v", err)
		}
		var report lintReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("JSON is not one valid document: %v\n%s", err, out.String())
		}
		if len(report.Results) != 2 || report.Summary.Documents != 2 {
			t.Fatalf("report results = %d summary=%+v, want two documents", len(report.Results), report.Summary)
		}
	})

	t.Run("yaml", func(t *testing.T) {
		var out bytes.Buffer
		if err := runLint(context.Background(), &out, &options{}, path, "yaml", false, false, "error"); err != nil {
			t.Fatalf("runLint YAML: %v", err)
		}
		var report lintReport
		if err := yaml.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("YAML is not one valid document: %v\n%s", err, out.String())
		}
		if len(report.Results) != 2 {
			t.Fatalf("YAML results = %d, want two", len(report.Results))
		}
	})
}

func TestLintFailOnAppliesToEveryOutputMode(t *testing.T) {
	path := writeLintFixture(t, validLintHPA("web"))
	for _, format := range []string{"github", "sarif", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			err := runLint(context.Background(), &out, &options{}, path, format, false, false, "warning")
			if err == nil {
				t.Fatalf("format %s: expected warning threshold failure", format)
			}
			if _, ok := err.(*exitCodeError); !ok {
				t.Fatalf("format %s: unexpected error %T: %v", format, err, err)
			}
			if out.Len() == 0 {
				t.Fatalf("format %s: expected output before threshold error", format)
			}
		})
	}
}

func TestLintRejectsInvalidFailOn(t *testing.T) {
	path := writeLintFixture(t, validLintHPA("web"))
	var out bytes.Buffer
	err := runLint(context.Background(), &out, &options{}, path, "json", false, false, "definitely-invalid")
	if err == nil || !strings.Contains(err.Error(), "--fail-on") {
		t.Fatalf("error = %v, want --fail-on validation", err)
	}
	if out.Len() != 0 {
		t.Fatalf("validation should run before output, got %q", out.String())
	}
}

func TestLintSurfacesMalformedHPADocument(t *testing.T) {
	content := "apiVersion: autoscaling/v2\n" +
		"kind: HorizontalPodAutoscaler\n" +
		"metadata:\n" +
		"  name: broken\n" +
		"spec:\n" +
		"  minReplicas: [not-valid\n"
	path := writeLintFixture(t, content)

	var out bytes.Buffer
	err := runLint(context.Background(), &out, &options{}, path, "json", false, false, "error")
	if err == nil {
		t.Fatal("malformed HPA should fail lint")
	}
	var report lintReport
	if decodeErr := json.Unmarshal(out.Bytes(), &report); decodeErr != nil {
		t.Fatalf("decode lint envelope: %v\n%s", decodeErr, out.String())
	}
	if len(report.Results) != 1 || report.Results[0].Result == nil {
		t.Fatalf("expected one decode result, got %+v", report)
	}
	findings := report.Results[0].Result.Findings
	if len(findings) != 1 || findings[0].Rule != "manifest-decode" {
		t.Fatalf("expected manifest-decode finding, got %+v", findings)
	}
}

func TestSplitYAMLDocumentsAcceptsSeparatorVariants(t *testing.T) {
	data := []byte(validLintHPA("web") + "--- # comment\r\n" + validLintHPA("api"))
	docs, err := readYAMLDocuments(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("documents = %d, want 2", len(docs))
	}
}
