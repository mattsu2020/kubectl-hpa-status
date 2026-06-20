package render

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func sampleStatusReport() hpaanalysis.StatusReport {
	return hpaanalysis.StatusReport{
		APIVersion: "hpa-status/v1",
		Analysis: hpaanalysis.Analysis{
			Namespace: "ns1", Name: "my-hpa",
			Current: 3, Desired: 5, Min: 1, Max: 10,
			Health:      "OK",
			HealthScore: 90,
		},
	}
}

func sampleListReport() hpaanalysis.ListReport {
	return hpaanalysis.ListReport{
		APIVersion: "hpa-status/v1",
		Items: []hpaanalysis.ListItem{
			{Namespace: "ns1", Name: "hpa-a", HealthScore: 80, Current: 2, Desired: 3, Min: 1, Max: 5},
			{Namespace: "ns1", Name: "hpa-b", HealthScore: 50, Current: 1, Desired: 1, Min: 1, Max: 3},
		},
	}
}

func TestFormat_TextVariantsInvokeWriteText(t *testing.T) {
	for _, fmtStr := range []string{"", "table", "wide", "ja"} {
		t.Run(fmtStr, func(t *testing.T) {
			var buf bytes.Buffer
			called := false
			err := Format(&buf, fmtStr, "", sampleStatusReport(), func() error {
				called = true
				_, werr := buf.WriteString("TEXT")
				return werr
			})
			if err != nil {
				t.Fatalf("Format(%q) error: %v", fmtStr, err)
			}
			if !called {
				t.Fatalf("writeText not invoked for %q", fmtStr)
			}
			if buf.String() != "TEXT" {
				t.Fatalf("output = %q, want TEXT", buf.String())
			}
		})
	}
}

func TestFormat_JSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Format(&buf, "json", "", sampleStatusReport(), func() error { return nil }); err != nil {
		t.Fatalf("Format(json): %v", err)
	}
	if !strings.Contains(buf.String(), `"apiVersion": "hpa-status/v1"`) {
		t.Fatalf("json output missing apiVersion: %s", buf.String())
	}
}

// TestFormat_JSONL_ListReport verifies that jsonl output is one compact JSON
// object per list item, newline-delimited, and NOT wrapped in a JSON array.
func TestFormat_JSONL_ListReport(t *testing.T) {
	var buf bytes.Buffer
	if err := Format(&buf, "jsonl", "", sampleListReport(), func() error { return nil }); err != nil {
		t.Fatalf("Format(jsonl): %v", err)
	}
	output := buf.String()
	// Two items => two lines.
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 jsonl lines, got %d:\n%s", len(lines), output)
	}
	// Must not be a JSON array (the json format contract).
	if strings.HasPrefix(strings.TrimSpace(output), "[") {
		t.Fatalf("jsonl output is a JSON array, expected newline-delimited objects:\n%s", output)
	}
	// Each line must contain one item name.
	for _, want := range []string{"hpa-a", "hpa-b"} {
		if !strings.Contains(output, want) {
			t.Fatalf("jsonl output missing item %q:\n%s", want, output)
		}
	}
}

// TestFormat_JSONL_StatusReport verifies a single StatusReport is emitted as
// one jsonl line.
func TestFormat_JSONL_StatusReport(t *testing.T) {
	var buf bytes.Buffer
	if err := Format(&buf, "jsonl", "", sampleStatusReport(), func() error { return nil }); err != nil {
		t.Fatalf("Format(jsonl): %v", err)
	}
	output := buf.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 jsonl line for a single StatusReport, got %d:\n%s", len(lines), output)
	}
	if !strings.Contains(output, "my-hpa") {
		t.Fatalf("jsonl output missing HPA name:\n%s", output)
	}
}

func TestFormat_YAML(t *testing.T) {
	var buf bytes.Buffer
	if err := Format(&buf, "yaml", "", sampleStatusReport(), func() error { return nil }); err != nil {
		t.Fatalf("Format(yaml): %v", err)
	}
	if !strings.Contains(buf.String(), "apiVersion: hpa-status/v1") {
		t.Fatalf("yaml output missing apiVersion: %s", buf.String())
	}
}

func TestFormat_JSONPath_Prefixed(t *testing.T) {
	var buf bytes.Buffer
	err := Format(&buf, "jsonpath={.apiVersion}", "", sampleStatusReport(), func() error { return nil })
	if err != nil {
		t.Fatalf("Format(jsonpath=): %v", err)
	}
	if !strings.Contains(buf.String(), "hpa-status/v1") {
		t.Fatalf("jsonpath output unexpected: %s", buf.String())
	}
}

func TestFormat_Template_Prefixed(t *testing.T) {
	var buf bytes.Buffer
	err := Format(&buf, "template={{.APIVersion}}", "", sampleStatusReport(), func() error { return nil })
	if err != nil {
		t.Fatalf("Format(template=): %v", err)
	}
	if strings.TrimSpace(buf.String()) != "hpa-status/v1" {
		t.Fatalf("template output = %q", buf.String())
	}
}

func TestFormat_Prometheus(t *testing.T) {
	t.Run("status report", func(t *testing.T) {
		var buf bytes.Buffer
		if err := Format(&buf, "prometheus", "", sampleStatusReport(), func() error { return nil }); err != nil {
			t.Fatalf("Format(prometheus): %v", err)
		}
		out := buf.String()
		for _, want := range []string{"hpa_health_score", "hpa_current_replicas", "namespace=\"ns1\"", "name=\"my-hpa\""} {
			if !strings.Contains(out, want) {
				t.Fatalf("prometheus output missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("list report", func(t *testing.T) {
		var buf bytes.Buffer
		if err := Format(&buf, "prometheus", "", sampleListReport(), func() error { return nil }); err != nil {
			t.Fatalf("Format(prometheus list): %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, `name="hpa-a"`) || !strings.Contains(out, `name="hpa-b"`) {
			t.Fatalf("prometheus list missing items:\n%s", out)
		}
	})
}

func TestFormat_Markdown(t *testing.T) {
	var buf bytes.Buffer
	if err := Format(&buf, "markdown", "", sampleStatusReport(), func() error { return nil }); err != nil {
		t.Fatalf("Format(markdown): %v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("markdown output empty")
	}
	// md alias should behave identically.
	var buf2 bytes.Buffer
	if err := Format(&buf2, "md", "", sampleStatusReport(), func() error { return nil }); err != nil {
		t.Fatalf("Format(md): %v", err)
	}
}

func TestFormat_HTML(t *testing.T) {
	var buf bytes.Buffer
	if err := Format(&buf, "html", "", sampleStatusReport(), func() error { return nil }); err != nil {
		t.Fatalf("Format(html): %v", err)
	}
	if !strings.Contains(buf.String(), "<") {
		t.Fatalf("html output does not look like markup: %s", buf.String())
	}
}

func TestFormat_Incident(t *testing.T) {
	var buf bytes.Buffer
	if err := Format(&buf, "incident", "", sampleStatusReport(), func() error { return nil }); err != nil {
		t.Fatalf("Format(incident): %v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("incident output empty")
	}
}

func TestFormat_Unknown(t *testing.T) {
	var buf bytes.Buffer
	err := Format(&buf, "csv", "", sampleStatusReport(), func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "unsupported output format") {
		t.Fatalf("expected unsupported-format error, got %v", err)
	}
}

func TestParsePrefixedFormat(t *testing.T) {
	cases := []struct {
		in     string
		wantOK bool
		kind   string
	}{
		{"jsonpath={.x}", true, "jsonpath"},
		{"jsonpath:{.x}", true, "jsonpath"},
		{"template={{.x}}", true, "go-template"},
		{"template:{{.x}}", true, "go-template"},
		{"go-template={{.x}}", true, "go-template"},
		{"go-template:{{.x}}", true, "go-template"},
		{"json", false, ""},
		{"plain", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			expr, kind, ok := ParsePrefixedFormat(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && kind != tc.kind {
				t.Fatalf("kind = %q, want %q", kind, tc.kind)
			}
			if ok && expr == "" {
				t.Fatalf("expr empty for matched prefix")
			}
		})
	}
}

func TestJSONPath_InvalidExpression(t *testing.T) {
	var buf bytes.Buffer
	err := JSONPath(&buf, "{invalid", sampleStatusReport())
	if err == nil || !strings.Contains(err.Error(), "invalid jsonpath expression") {
		t.Fatalf("expected invalid expression error, got %v", err)
	}
}

func TestTemplate_InvalidExpression(t *testing.T) {
	var buf bytes.Buffer
	err := Template(&buf, "{{ .Bogus", sampleStatusReport())
	if err == nil || !strings.Contains(err.Error(), "invalid template expression") {
		t.Fatalf("expected invalid template error, got %v", err)
	}
}

func TestPrometheus_RejectsUnknownType(t *testing.T) {
	var buf bytes.Buffer
	err := Prometheus(&buf, "not a report")
	if err == nil || !strings.Contains(err.Error(), "requires a StatusReport or ListReport") {
		t.Fatalf("expected type error, got %v", err)
	}
}

func TestMarkdown_RejectsUnknownType(t *testing.T) {
	var buf bytes.Buffer
	if err := Markdown(&buf, "bogus"); err == nil {
		t.Fatalf("expected error for unknown markdown type")
	}
}

func TestHTML_RejectsUnknownType(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, 42); err == nil {
		t.Fatalf("expected error for unknown html type")
	}
}

func TestIncident_RejectsUnknownType(t *testing.T) {
	var buf bytes.Buffer
	if err := Incident(&buf, 42); err == nil {
		t.Fatalf("expected error for unknown incident type")
	}
}

func TestEscapePrometheusLabelValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{`back\slash`, `back\\slash`},
		{`quote"`, `quote\"`},
		{`mix\"`, `mix\\\"`},
	}
	for _, tc := range cases {
		if got := EscapePrometheusLabelValue(tc.in); got != tc.want {
			t.Fatalf("EscapePrometheusLabelValue(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestError_FormatVariants(t *testing.T) {
	sentinel := errors.New("boom")

	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		Error(&buf, "json", sentinel)
		if !strings.Contains(buf.String(), `"error":"boom"`) {
			t.Fatalf("json error output: %s", buf.String())
		}
	})
	t.Run("yaml", func(t *testing.T) {
		var buf bytes.Buffer
		Error(&buf, "yaml", sentinel)
		if !strings.Contains(buf.String(), "boom") {
			t.Fatalf("yaml error output: %s", buf.String())
		}
	})
	t.Run("default", func(t *testing.T) {
		var buf bytes.Buffer
		Error(&buf, "", sentinel)
		if !strings.Contains(buf.String(), "Error: boom") {
			t.Fatalf("default error output: %s", buf.String())
		}
	})
}
