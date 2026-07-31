// Package render centralizes output format routing and serialization for the
// kubectl-hpa-status commands. The functions here translate a generic value
// into the requested output format (json, yaml, jsonpath, go-template,
// prometheus, markdown, html, incident). They are pure (no cobra, no options
// struct) so they can be reused by any caller and tested in isolation.
//
// Command callers use this package directly; cmd/output.go contains only
// command-specific selection and localization policy.
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

// formatRenderers maps each non-textual output format to its renderer. The
// template argument is only consulted by the jsonpath/template entries.
var formatRenderers = map[string]func(out io.Writer, templateStr string, value any) error{
	"json": func(out io.Writer, _ string, value any) error {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	},
	"jsonl": func(out io.Writer, _ string, value any) error { return JSONLines(out, value) },
	"yaml": func(out io.Writer, _ string, value any) error {
		data, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	},
	"jsonpath":    JSONPath,
	"go-template": Template,
	"template":    Template,
	"prometheus":  func(out io.Writer, _ string, value any) error { return Prometheus(out, value) },
	"markdown":    func(out io.Writer, _ string, value any) error { return Markdown(out, value) },
	"md":          func(out io.Writer, _ string, value any) error { return Markdown(out, value) },
	"html":        func(out io.Writer, _ string, value any) error { return HTML(out, value) },
	"incident":    func(out io.Writer, _ string, value any) error { return Incident(out, value) },
}

// Format is the top-level output-format dispatcher. writeText is invoked for
// the human-readable formats ("", "table", "wide", "ja"); every other format
// serializes value directly.
func Format(out io.Writer, format string, templateStr string, value any, writeText func(io.Writer) error) error {
	switch format {
	case "", "table", "wide", "ja":
		if writeText == nil {
			return fmt.Errorf("text output requires a text renderer")
		}
		tracked := &errorTrackingWriter{writer: out}
		if err := writeText(tracked); err != nil {
			return err
		}
		return tracked.err
	}
	if renderer, ok := formatRenderers[format]; ok {
		return renderer(out, templateStr, value)
	}
	if expr, kind, ok := ParsePrefixedFormat(format); ok {
		return formatRenderers[kind](out, expr, value)
	}
	return fmt.Errorf("unsupported output format %q", format)
}

// errorTrackingWriter preserves the first write failure even when a legacy
// text renderer ignores fmt.Fprintf's return value. Format checks err after
// the callback, so broken pipes and short writers are never reported as a
// successful command.
type errorTrackingWriter struct {
	writer io.Writer
	err    error
}

// Unwrap exposes the original destination to presentation helpers such as
// terminal/color detection while writes continue through this error tracker.
func (w *errorTrackingWriter) Unwrap() io.Writer {
	return w.writer
}

func (w *errorTrackingWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return n, err
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
// ListReport each item is emitted on its own line, and []StatusRecordV2 emits
// one canonical v2 record per line. Other types produce one line for the whole
// value; notably, the historical v1 []StatusReport shape remains one JSON array
// on one line.
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
	case []hpaanalysis.StatusRecordV2:
		for i := range report {
			if err := encoder.Encode(report[i]); err != nil {
				return fmt.Errorf("jsonl: encode v2 status record %d: %w", i, err)
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
		if err := writePrometheusMetadata(w); err != nil {
			return err
		}
		for _, item := range report.Items {
			if err := writePrometheusSamples(w, item.Namespace, item.Name, item.HealthScore, item.Current, item.Desired, item.Min, item.Max); err != nil {
				return err
			}
		}
		return nil
	case hpaanalysis.StatusReport:
		a := report.Analysis
		return PrometheusMetrics(w, a.Namespace, a.Name, a.HealthScore, a.Current, a.Desired, a.Min, a.Max)
	default:
		return fmt.Errorf("prometheus output requires a StatusReport or ListReport, got %T", value)
	}
}

type prometheusMetricDefinition struct {
	name string
	help string
}

var prometheusMetricDefinitions = []prometheusMetricDefinition{
	{name: "hpa_health_score", help: "Health score of an HPA (0-100)"},
	{name: "hpa_current_replicas", help: "Current replica count"},
	{name: "hpa_desired_replicas", help: "Desired replica count"},
	{name: "hpa_min_replicas", help: "Minimum replica count"},
	{name: "hpa_max_replicas", help: "Maximum replica count"},
}

func writePrometheusMetadata(w io.Writer) error {
	for _, metric := range prometheusMetricDefinitions {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n", metric.name, metric.help); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "# TYPE %s gauge\n", metric.name); err != nil {
			return err
		}
	}
	return nil
}

func writePrometheusSamples(w io.Writer, namespace, name string, healthScore int, current, desired, minR, maxR int32) error {
	values := []any{healthScore, current, desired, minR, maxR}
	labels := fmt.Sprintf(`namespace="%s",name="%s"`, EscapePrometheusLabelValue(namespace), EscapePrometheusLabelValue(name))
	for i, metric := range prometheusMetricDefinitions {
		if _, err := fmt.Fprintf(w, "%s{%s} %v\n", metric.name, labels, values[i]); err != nil {
			return err
		}
	}
	return nil
}

// PrometheusMetrics writes a complete minimal Prometheus exposition for a
// single HPA. List rendering writes the shared HELP/TYPE metadata once and then
// calls writePrometheusSamples for each item.
func PrometheusMetrics(w io.Writer, namespace, name string, healthScore int, current, desired, minR, maxR int32) error {
	if err := writePrometheusMetadata(w); err != nil {
		return err
	}
	return writePrometheusSamples(w, namespace, name, healthScore, current, desired, minR, maxR)
}

// EscapePrometheusLabelValue escapes a string for safe inclusion in a
// Prometheus label value (per the exposition format rules). Newlines must be
// represented as the two-byte sequence "\n"; leaving them literal would allow
// a label to inject an additional exposition line.
func EscapePrometheusLabelValue(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
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
