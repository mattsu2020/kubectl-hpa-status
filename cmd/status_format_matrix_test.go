package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	"sigs.k8s.io/yaml"
)

func TestStatusSingleAndMultipleOutputFormats(t *testing.T) {
	for _, count := range []int{1, 2} {
		for _, format := range []string{"table", "wide", "json", "yaml", "jsonl", "prometheus", "markdown", "html", "incident"} {
			t.Run(fmt.Sprintf("%d/%s", count, format), func(t *testing.T) {
				client := testutil.NewFakeClient(testutil.BuildHPA("default", "web"), testutil.BuildHPA("default", "api"))
				args := []string{"status", "web"}
				if count == 2 {
					args = append(args, "api")
				}
				args = append(args, "--no-enrich", "--output="+format)
				out := executeOutputSchemaV2CLI(t, client, args...)
				if strings.TrimSpace(out) == "" {
					t.Fatal("empty output")
				}
				switch format {
				case "json":
					if !json.Valid([]byte(out)) {
						t.Fatalf("invalid JSON: %s", out)
					}
				case "yaml":
					var decoded any
					if err := yaml.UnmarshalStrict([]byte(out), &decoded); err != nil {
						t.Fatalf("invalid YAML: %v", err)
					}
				case "jsonl":
					lines := strings.Split(strings.TrimSpace(out), "\n")
					if len(lines) != count {
						t.Fatalf("got %d records, want %d", len(lines), count)
					}
					for _, line := range lines {
						if !json.Valid([]byte(line)) {
							t.Fatalf("invalid JSONL: %s", line)
						}
					}
				case "prometheus":
					if strings.Count(out, "# HELP hpa_health_score ") != 1 || strings.Count(out, "# TYPE hpa_health_score gauge") != 1 || strings.Count(out, "\nhpa_health_score{") != count {
						t.Fatalf("wrong metadata/sample count: %s", out)
					}
				}
			})
		}
	}
}

func TestStatusReportFormatOverridesOutputForSingleAndMultiple(t *testing.T) {
	for _, count := range []int{1, 2} {
		for _, output := range []string{"json", "yaml", "jsonl"} {
			for _, report := range []string{"markdown", "html", "incident"} {
				t.Run(fmt.Sprintf("%d/%s/%s", count, output, report), func(t *testing.T) {
					client := testutil.NewFakeClient(testutil.BuildHPA("default", "web"), testutil.BuildHPA("default", "api"))
					args := []string{"status", "web"}
					if count == 2 {
						args = append(args, "api")
					}
					args = append(args, "--no-enrich", "--output="+output, "--report="+report)
					out := executeOutputSchemaV2CLI(t, client, args...)
					if !strings.Contains(out, "web") || (count == 2 && !strings.Contains(out, "api")) || json.Valid([]byte(out)) {
						t.Fatalf("incorrect report output: %s", out)
					}
				})
			}
		}
	}
}

func TestStatusReportOverrideKeepsPartialErrorsVisible(t *testing.T) {
	client := testutil.NewFakeClient(testutil.BuildHPA("default", "web"))
	root := NewRootCommandWithDeps(AppDeps{Kubernetes: client})
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"status", "web", "missing", "--no-enrich", "-o", "json", "--report", "markdown"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected partial failure exit")
	}
	if !strings.Contains(stdout.String(), "web") || !strings.Contains(stderr.String(), "default/missing") {
		t.Fatalf("partial results or errors lost: stdout=%s stderr=%s", &stdout, &stderr)
	}
}
