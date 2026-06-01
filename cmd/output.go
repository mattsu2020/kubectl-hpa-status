package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"golang.org/x/term"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/yaml"
)

func shouldColorize(mode string, out io.Writer) bool {
	switch strings.ToLower(mode) {
	case "always", "true", "yes":
		return true
	case "never", "false", "no":
		return false
	case "", "auto":
		file, ok := out.(*os.File)
		return ok && term.IsTerminal(int(file.Fd()))
	default:
		return false
	}
}

func outputLang(opts *options) string {
	if opts.lang != "" {
		return strings.ToLower(opts.lang)
	}
	if strings.EqualFold(opts.output, "ja") {
		return "ja"
	}
	return ""
}

func analysisOptions(opts *options) hpaanalysis.AnalysisOptions {
	return hpaanalysis.AnalysisOptions{
		HealthWeights: opts.healthWeights,
		Debug:         opts.debug,
	}
}

func reportHasCondition(report hpaanalysis.StatusReport, condition string) bool {
	want := normalizeSelector(condition)
	for _, current := range report.Analysis.Conditions {
		if normalizeSelector(current.Type) == want {
			return true
		}
	}
	return false
}

func normalizeSelector(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func writeOutput(out io.Writer, format string, templateStr string, value any, writeText func() error) error {
	switch format {
	case "", "table", "wide", "ja":
		return writeText()
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	case "yaml":
		data, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "jsonpath":
		return writeJSONPath(out, templateStr, value)
	case "go-template", "template":
		return writeTemplate(out, templateStr, value)
	case "prometheus":
		return writePrometheus(out, value)
	default:
		if expression, ok := strings.CutPrefix(format, "jsonpath="); ok {
			return writeJSONPath(out, expression, value)
		}
		if expression, ok := strings.CutPrefix(format, "jsonpath:"); ok {
			return writeJSONPath(out, expression, value)
		}
		if expression, ok := strings.CutPrefix(format, "template="); ok {
			return writeTemplate(out, expression, value)
		}
		if expression, ok := strings.CutPrefix(format, "template:"); ok {
			return writeTemplate(out, expression, value)
		}
		if expression, ok := strings.CutPrefix(format, "go-template="); ok {
			return writeTemplate(out, expression, value)
		}
		if expression, ok := strings.CutPrefix(format, "go-template:"); ok {
			return writeTemplate(out, expression, value)
		}
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func outputSelection(opts *options) (string, string) {
	format := opts.output
	templateStr := opts.template
	if len(opts.outputTemplates) == 0 || format == "" {
		return format, templateStr
	}
	if cfg, ok := opts.outputTemplates[format]; ok {
		if cfg.Type == "" {
			return "go-template", cfg.Template
		}
		return normalizeTemplateType(cfg.Type), cfg.Template
	}
	for _, prefix := range []string{"jsonpath=", "jsonpath:", "template=", "template:", "go-template=", "go-template:"} {
		name, ok := strings.CutPrefix(format, prefix)
		if !ok {
			continue
		}
		cfg, exists := opts.outputTemplates[name]
		if !exists {
			return format, templateStr
		}
		if cfg.Type == "" {
			if strings.HasPrefix(prefix, "jsonpath") {
				return "jsonpath", cfg.Template
			}
			return "go-template", cfg.Template
		}
		return normalizeTemplateType(cfg.Type), cfg.Template
	}
	return format, templateStr
}

func normalizeTemplateType(value string) string {
	switch normalizeSelector(value) {
	case "jsonpath":
		return "jsonpath"
	case "gotemplate", "template":
		return "go-template"
	default:
		return value
	}
}

func writeJSONPath(out io.Writer, expression string, value any) error {
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

func writeTemplate(out io.Writer, expression string, value any) error {
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

func writePrometheus(w io.Writer, value any) error {
	switch report := value.(type) {
	case hpaanalysis.ListReport:
		for _, item := range report.Items {
			if err := writePrometheusMetrics(w, item.Namespace, item.Name, item.HealthScore, item.Current, item.Desired, item.Min, item.Max); err != nil {
				return err
			}
		}
	case hpaanalysis.StatusReport:
		a := report.Analysis
		return writePrometheusMetrics(w, a.Namespace, a.Name, a.HealthScore, a.Current, a.Desired, a.Min, a.Max)
	default:
		return fmt.Errorf("prometheus output requires a StatusReport or ListReport, got %T", value)
	}
	return nil
}

func writePrometheusMetrics(w io.Writer, namespace, name string, healthScore int, current, desired, minR, maxR int32) error {
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
	labels := fmt.Sprintf(`namespace="%s",name="%s"`, escapePrometheusLabelValue(namespace), escapePrometheusLabelValue(name))
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

func escapePrometheusLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func writeError(out io.Writer, format string, err error) {
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
