package cmd

import (
	"bytes"
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"sigs.k8s.io/yaml"
)

func exportTestReport(namespace, name string, suggestions ...hpaanalysis.Suggestion) hpaanalysis.StatusReport {
	return hpaanalysis.StatusReport{
		APIVersion: hpaanalysis.SchemaVersion,
		Analysis: hpaanalysis.Analysis{
			Namespace:   namespace,
			Name:        name,
			Suggestions: suggestions,
		},
	}
}

func TestCollectSuggestionSpecDeepMergesNestedPatches(t *testing.T) {
	suggestions := []hpaanalysis.Suggestion{
		{
			Title: "set stabilization",
			Apply: true,
			Patch: `{"spec":{"behavior":{"scaleDown":{"stabilizationWindowSeconds":300}}}}`,
		},
		{
			Title: "set select policy and maximum",
			Apply: true,
			Patch: `{"spec":{"behavior":{"scaleDown":{"selectPolicy":"Min"}},"maxReplicas":20}}`,
		},
	}

	spec, err := collectSuggestionSpec(suggestions)
	if err != nil {
		t.Fatalf("collectSuggestionSpec: %v", err)
	}
	if got := spec["maxReplicas"]; got != float64(20) {
		t.Fatalf("maxReplicas = %#v, want 20", got)
	}
	behavior, ok := spec["behavior"].(map[string]any)
	if !ok {
		t.Fatalf("behavior = %#v, want object", spec["behavior"])
	}
	scaleDown, ok := behavior["scaleDown"].(map[string]any)
	if !ok {
		t.Fatalf("scaleDown = %#v, want object", behavior["scaleDown"])
	}
	if got := scaleDown["stabilizationWindowSeconds"]; got != float64(300) {
		t.Errorf("stabilizationWindowSeconds = %#v, want 300", got)
	}
	if got := scaleDown["selectPolicy"]; got != "Min" {
		t.Errorf("selectPolicy = %#v, want Min", got)
	}
}

func TestWriteGitOpsExportRejectsInvalidApplicablePatch(t *testing.T) {
	report := exportTestReport("default", "web", hpaanalysis.Suggestion{
		Title: "broken",
		Apply: true,
		Patch: `{"spec":`,
	})
	var out bytes.Buffer
	err := writeGitOpsExport(&out, "yaml", report)
	if err == nil {
		t.Fatal("writeGitOpsExport returned nil for malformed JSON patch")
	}
	if !strings.Contains(err.Error(), "suggestion patch") {
		t.Fatalf("error = %v, want patch context", err)
	}
	if out.Len() != 0 {
		t.Fatalf("malformed patch produced partial output: %q", out.String())
	}
}

func TestWriteGitOpsExportRejectsNonObjectSpec(t *testing.T) {
	report := exportTestReport("default", "web", hpaanalysis.Suggestion{
		Title: "delete spec",
		Apply: true,
		Patch: `{"spec":null}`,
	})
	var out bytes.Buffer
	err := writeGitOpsExport(&out, "yaml", report)
	if err == nil || !strings.Contains(err.Error(), "spec must be an object") {
		t.Fatalf("error = %v, want invalid spec error", err)
	}
	if out.Len() != 0 {
		t.Fatalf("invalid spec produced partial output: %q", out.String())
	}
}

func TestWriteReportsGitOpsExportUsesYAMLDocumentSeparators(t *testing.T) {
	reports := []hpaanalysis.StatusReport{
		exportTestReport("team-a", "web", hpaanalysis.Suggestion{
			Title: "raise web maximum",
			Apply: true,
			Patch: `{"spec":{"maxReplicas":20}}`,
		}),
		exportTestReport("team-b", "worker", hpaanalysis.Suggestion{
			Title: "raise worker minimum",
			Apply: true,
			Patch: `{"spec":{"minReplicas":3}}`,
		}),
	}

	var out bytes.Buffer
	if err := writeReportsGitOpsExport(&out, "yaml", reports); err != nil {
		t.Fatalf("writeReportsGitOpsExport: %v", err)
	}
	documents := strings.Split(strings.TrimSpace(out.String()), "\n---\n")
	if len(documents) != 2 {
		t.Fatalf("YAML document count = %d, want 2:\n%s", len(documents), out.String())
	}
	for i, wantName := range []string{"web", "worker"} {
		var document hpaPatchExport
		if err := yaml.Unmarshal([]byte(documents[i]), &document); err != nil {
			t.Fatalf("decode YAML document %d: %v\n%s", i, err, documents[i])
		}
		if document.Metadata.Name != wantName {
			t.Errorf("document %d name = %q, want %q", i, document.Metadata.Name, wantName)
		}
	}
}

func TestWriteReportsGitOpsExportRejectsAmbiguousMultiTargetFormats(t *testing.T) {
	reports := []hpaanalysis.StatusReport{
		exportTestReport("default", "one", hpaanalysis.Suggestion{
			Title: "one",
			Apply: true,
			Patch: `{"spec":{"maxReplicas":10}}`,
		}),
		exportTestReport("default", "two", hpaanalysis.Suggestion{
			Title: "two",
			Apply: true,
			Patch: `{"spec":{"maxReplicas":20}}`,
		}),
	}

	for _, format := range []string{"kustomize", "helm-values"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			err := writeReportsGitOpsExport(&out, format, reports)
			if err == nil || !strings.Contains(err.Error(), "single HPA") {
				t.Fatalf("error = %v, want explicit multi-HPA rejection", err)
			}
			if out.Len() != 0 {
				t.Fatalf("rejected export produced partial output: %q", out.String())
			}
		})
	}
}

func TestWriteReportsGitOpsExportValidatesEveryYAMLDocumentBeforeWriting(t *testing.T) {
	reports := []hpaanalysis.StatusReport{
		exportTestReport("default", "valid", hpaanalysis.Suggestion{
			Title: "valid",
			Apply: true,
			Patch: `{"spec":{"maxReplicas":10}}`,
		}),
		exportTestReport("default", "invalid", hpaanalysis.Suggestion{
			Title: "invalid",
			Apply: true,
			Patch: `{"spec":`,
		}),
	}

	var out bytes.Buffer
	err := writeReportsGitOpsExport(&out, "yaml", reports)
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %v, want invalid second patch error", err)
	}
	if out.Len() != 0 {
		t.Fatalf("validation failure produced a partial YAML stream:\n%s", out.String())
	}
}
