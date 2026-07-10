// Package render centralizes output format routing and serialization for the
// kubectl-hpa-status commands. The functions here translate a generic value
// into the requested output format (json, yaml, jsonpath, go-template,
// prometheus, markdown, html, incident). They are pure (no cobra, no options
// struct) so they can be reused by any caller and tested in isolation.
//
// cmd/ re-exports these under their historical unexported names via a thin
// facade in cmd/output.go; when the cmd/ sub-package split lands, callers
// migrate to render.* directly and the facade shrinks.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	hparender "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/render"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/yaml"
)

// Format is the top-level output-format dispatcher. writeText is invoked for
// the human-readable formats ("", "table", "wide", "ja"); every other format
// serializes value directly.
func Format(out io.Writer, format string, templateStr string, value any, writeText func() error) error {
	switch format {
	case "", "table", "wide", "ja":
		return writeText()
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	case "jsonl":
		return JSONLines(out, value)
	case "yaml":
		data, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "jsonpath":
		return JSONPath(out, templateStr, value)
	case "go-template", "template":
		return Template(out, templateStr, value)
	case "prometheus":
		return Prometheus(out, value)
	case "markdown", "md":
		return Markdown(out, value)
	case "html":
		return HTML(out, value)
	case "incident":
		return Incident(out, value)
	default:
		if expr, kind, ok := ParsePrefixedFormat(format); ok {
			switch kind {
			case "jsonpath":
				return JSONPath(out, expr, value)
			case "go-template":
				return Template(out, expr, value)
			}
		}
		return fmt.Errorf("unsupported output format %q", format)
	}
}

// ParsePrefixedFormat recognizes "jsonpath=", "jsonpath:", "template=",
// "template:", "go-template=", and "go-template:" prefixes. It returns the
// expression, the normalized format kind ("jsonpath" or "go-template"), and
// whether a known prefix was matched.
func ParsePrefixedFormat(format string) (expr string, kind string, ok bool) {
	prefixes := []struct {
		prefix string
		kind   string
	}{
		{"jsonpath=", "jsonpath"},
		{"jsonpath:", "jsonpath"},
		{"template=", "go-template"},
		{"template:", "go-template"},
		{"go-template=", "go-template"},
		{"go-template:", "go-template"},
	}
	for _, p := range prefixes {
		if expr, ok := strings.CutPrefix(format, p.prefix); ok {
			return expr, p.kind, true
		}
	}
	return "", "", false
}

// JSONLines writes value as newline-delimited JSON (JSON Lines / jsonl). For a
// ListReport each item is emitted on its own line; for a StatusReport the
// single report is one line. Other types produce one line for the whole value.
// JSONL is the streaming-friendly counterpart of "json": a large list can be
// produced and consumed one record at a time without buffering the whole array.
func JSONLines(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	switch report := value.(type) {
	case hpaanalysis.ListReport:
		for i := range report.Items {
			if err := encoder.Encode(report.Items[i]); err != nil {
				return fmt.Errorf("jsonl: encode list item %d: %w", i, err)
			}
		}
		return nil
	default:
		if err := encoder.Encode(value); err != nil {
			return fmt.Errorf("jsonl: encode value: %w", err)
		}
		return nil
	}
}

// JSONPath evaluates a jsonpath expression against value and writes the result.
func JSONPath(out io.Writer, expression string, value any) error {
	parser := jsonpath.New("output")
	parser.AllowMissingKeys(true)
	if err := parser.Parse(expression); err != nil {
		return fmt.Errorf("invalid jsonpath expression: %w", err)
	}
	if err := parser.Execute(out, value); err != nil {
		return fmt.Errorf("failed to execute jsonpath expression: %w", err)
	}
	_, err := fmt.Fprintln(out)
	return err
}

// Template evaluates a Go text/template against value and writes the result.
func Template(out io.Writer, expression string, value any) error {
	tmpl, err := template.New("output").Parse(expression)
	if err != nil {
		return fmt.Errorf("invalid template expression: %w", err)
	}
	if err := tmpl.Execute(out, value); err != nil {
		return fmt.Errorf("failed to execute template expression: %w", err)
	}
	_, err = fmt.Fprintln(out)
	return err
}

// Prometheus renders the value in Prometheus exposition format.
func Prometheus(w io.Writer, value any) error {
	switch report := value.(type) {
	case hpaanalysis.ListReport:
		for _, item := range report.Items {
			if err := PrometheusMetrics(w, item.Namespace, item.Name, item.HealthScore, item.Current, item.Desired, item.Min, item.Max); err != nil {
				return err
			}
		}
	case hpaanalysis.StatusReport:
		a := report.Analysis
		return PrometheusMetrics(w, a.Namespace, a.Name, a.HealthScore, a.Current, a.Desired, a.Min, a.Max)
	default:
		return fmt.Errorf("prometheus output requires a StatusReport or ListReport, got %T", value)
	}
	return nil
}

// PrometheusMetrics writes a minimal Prometheus exposition for a single HPA.
func PrometheusMetrics(w io.Writer, namespace, name string, healthScore int, current, desired, minR, maxR int32) error {
	type metric struct {
		name  string
		help  string
		value any
	}
	metrics := []metric{
		{name: "hpa_health_score", help: "Health score of an HPA (0-100)", value: healthScore},
		{name: "hpa_current_replicas", help: "Current replica count", value: current},
		{name: "hpa_desired_replicas", help: "Desired replica count", value: desired},
		{name: "hpa_min_replicas", help: "Minimum replica count", value: minR},
		{name: "hpa_max_replicas", help: "Maximum replica count", value: maxR},
	}
	labels := fmt.Sprintf(`namespace="%s",name="%s"`, EscapePrometheusLabelValue(namespace), EscapePrometheusLabelValue(name))
	for _, m := range metrics {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n", m.name, m.help); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "# TYPE %s gauge\n", m.name); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s{%s} %v\n", m.name, labels, m.value); err != nil {
			return err
		}
	}
	return nil
}

// EscapePrometheusLabelValue escapes a string for safe inclusion in a
// Prometheus label value (per the exposition format rules).
func EscapePrometheusLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// Markdown renders value as Markdown, dispatching on the concrete report type.
func Markdown(out io.Writer, value any) error {
	switch report := value.(type) {
	case hpaanalysis.StatusReport:
		return hparender.WriteMarkdownReport(out, report)
	case []hpaanalysis.StatusReport:
		for i, r := range report {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if err := hparender.WriteMarkdownReport(out, r); err != nil {
				return err
			}
		}
		return nil
	case hpaanalysis.ListReport:
		return hparender.WriteMarkdownListReport(out, report)
	default:
		return fmt.Errorf("markdown output requires a StatusReport or ListReport, got %T", value)
	}
}

// HTML renders value as HTML, dispatching on the concrete report type.
func HTML(out io.Writer, value any) error {
	switch report := value.(type) {
	case hpaanalysis.StatusReport:
		return hparender.WriteHTMLReport(out, report)
	case []hpaanalysis.StatusReport:
		return hparender.WriteHTMLReports(out, report)
	case hpaanalysis.ListReport:
		return hparender.WriteHTMLListReport(out, report)
	default:
		return fmt.Errorf("html output requires a StatusReport or ListReport, got %T", value)
	}
}

// Incident renders value in the incident-bundle shape, dispatching on type.
func Incident(out io.Writer, value any) error {
	switch report := value.(type) {
	case hpaanalysis.StatusReport:
		return hparender.WriteIncidentReport(out, report)
	case []hpaanalysis.StatusReport:
		for i, r := range report {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if err := hparender.WriteIncidentReport(out, r); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("incident report requires a StatusReport, got %T", value)
	}
}

// Error writes an error in the requested format to out. Write failures are
// intentionally ignored: we are already on the error-reporting path.
func Error(out io.Writer, format string, err error) {
	switch format {
	case "json":
		_ = json.NewEncoder(out).Encode(map[string]string{"error": err.Error()})
	case "yaml":
		data, marshalErr := yaml.Marshal(map[string]string{"error": err.Error()})
		if marshalErr != nil {
			_, _ = fmt.Fprintf(out, "Error: %v\n", err)
			return
		}
		_, _ = out.Write(data)
	default:
		_, _ = fmt.Fprintf(out, "Error: %v\n", err)
	}
}
