package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/patch"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"sigs.k8s.io/yaml"
)

type hpaPatchExport struct {
	APIVersion string         `json:"apiVersion" yaml:"apiVersion"`
	Kind       string         `json:"kind" yaml:"kind"`
	Metadata   exportMetadata `json:"metadata" yaml:"metadata"`
	Spec       map[string]any `json:"spec" yaml:"spec"`
}

type exportMetadata struct {
	Name      string `json:"name" yaml:"name"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

func writeGitOpsExport(out io.Writer, format string, report hpaanalysis.StatusReport) error {
	format, err := normalizeGitOpsExportFormat(format)
	if err != nil {
		return err
	}
	spec, err := collectSuggestionSpec(report.Analysis.Suggestions)
	if err != nil {
		return fmt.Errorf("build GitOps export for HPA %s/%s: %w", report.Analysis.Namespace, report.Analysis.Name, err)
	}
	if len(spec) == 0 {
		_, err := fmt.Fprintln(out, "# no applicable HPA spec patch suggestions")
		return err
	}
	switch format {
	case "yaml":
		return writeYAMLExport(out, report, spec)
	case "kustomize":
		return writeKustomizeExport(out, report, spec)
	case "helm-values":
		return writeHelmValuesExport(out, report, spec)
	default:
		return fmt.Errorf("unsupported normalized GitOps export format %q", format)
	}
}

func normalizeGitOpsExportFormat(format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return "yaml", nil
	case "kustomize":
		return "kustomize", nil
	case "helm-values", "helm", "values":
		return "helm-values", nil
	default:
		return "", fmt.Errorf("unsupported --export format %q (use yaml, kustomize, or helm-values)", format)
	}
}

func collectSuggestionSpec(suggestions []hpaanalysis.Suggestion) (map[string]any, error) {
	applicable := collectApplicablePatches(suggestions)
	if len(applicable) == 0 {
		return map[string]any{}, nil
	}

	patches := make([]patch.Patch, 0, len(applicable))
	for _, suggestion := range applicable {
		patches = append(patches, patch.Patch{Title: suggestion.Title, JSON: suggestion.Patch})
	}
	mergedJSON, err := patch.MergePatches(patches)
	if err != nil {
		return nil, fmt.Errorf("merge suggestion patches: %w", err)
	}

	var merged map[string]any
	if err := json.Unmarshal([]byte(mergedJSON), &merged); err != nil {
		return nil, fmt.Errorf("decode merged suggestion patch: %w", err)
	}
	rawSpec, exists := merged["spec"]
	if !exists {
		return nil, fmt.Errorf("merged suggestion patch does not contain a spec object")
	}
	spec, ok := rawSpec.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("merged suggestion patch spec must be an object, got %T", rawSpec)
	}
	if len(spec) == 0 {
		return nil, fmt.Errorf("merged suggestion patch contains an empty spec object")
	}
	return spec, nil
}

func writeYAMLExport(out io.Writer, report hpaanalysis.StatusReport, spec map[string]any) error {
	doc := hpaPatchExport{
		APIVersion: "autoscaling/v2",
		Kind:       "HorizontalPodAutoscaler",
		Metadata: exportMetadata{
			Name:      report.Analysis.Name,
			Namespace: report.Analysis.Namespace,
		},
		Spec: spec,
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}

func writeKustomizeExport(out io.Writer, report hpaanalysis.StatusReport, spec map[string]any) error {
	if _, err := fmt.Fprintln(out, "# suggested-hpa-patch.yaml"); err != nil {
		return err
	}
	if err := writeYAMLExport(out, report, spec); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "\n# kustomization.yaml\npatchesStrategicMerge:\n  - suggested-hpa-patch.yaml")
	return err
}

func writeHelmValuesExport(out io.Writer, report hpaanalysis.StatusReport, spec map[string]any) error {
	values := map[string]any{
		"hpa": map[string]any{
			"name": report.Analysis.Name,
			"spec": spec,
		},
	}
	data, err := yaml.Marshal(values)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}
