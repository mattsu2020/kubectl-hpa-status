package cmd

import (
	"bytes"
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubehpa_cli/pkg/hpa"
)

func TestWriteOutputJSONPath(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			Namespace: "default",
			Name:      "web",
			Summary:   "HPA currently keeps the replica count unchanged.",
		},
	}

	var out bytes.Buffer
	if err := writeOutput(&out, "jsonpath={.analysis.summary}", report, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "HPA currently keeps the replica count unchanged." {
		t.Fatalf("unexpected jsonpath output: %q", out.String())
	}
}

func TestWriteOutputTemplate(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			Namespace: "default",
			Name:      "web",
		},
	}

	var out bytes.Buffer
	if err := writeOutput(&out, "template={{ .Analysis.Namespace }}/{{ .Analysis.Name }}", report, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "default/web" {
		t.Fatalf("unexpected template output: %q", out.String())
	}
}
