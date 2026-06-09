package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

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
	spec := collectSuggestionSpec(report.Analysis.Suggestions)
	if len(spec) == 0 {
		_, err := fmt.Fprintln(out, "# no applicable HPA spec patch suggestions")
		return err
	}
	switch strings.ToLower(format) {
	case "yaml", "yml", "":
		return writeYAMLExport(out, report, spec)
	case "kustomize":
		return writeKustomizeExport(out, report, spec)
	case "helm-values", "helm", "values":
		return writeHelmValuesExport(out, report, spec)
	default:
		return fmt.Errorf("unsupported --export format %q (use yaml, kustomize, or helm-values)", format)
	}
}

func collectSuggestionSpec(suggestions []hpaanalysis.Suggestion) map[string]any {
	spec := make(map[string]any)
	for _, suggestion := range collectApplicablePatches(suggestions) {
		var patch map[string]any
		if err := json.Unmarshal([]byte(suggestion.Patch), &patch); err != nil {
			continue
		}
		rawSpec, ok := patch["spec"].(map[string]any)
		if !ok {
			continue
		}
		for key, value := range rawSpec {
			spec[key] = value
		}
	}
	return spec
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
