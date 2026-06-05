package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/mattsu2020/kubectl-hpa-status/internal/i18n"
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

func outputLang(lang, output string) string {
	if lang != "" {
		return strings.ToLower(lang)
	}
	if strings.EqualFold(output, "ja") {
		return "ja"
	}
	return ""
}

// i18nLabels is a LabelProvider backed by the internal/i18n locale system.
type i18nLabels struct {
	lang string
}

func (p i18nLabels) Get(key string) string {
	return i18n.Get(p.lang, key)
}

func labelProviderForLang(lang, output string) hpaanalysis.LabelProvider {
	l := outputLang(lang, output)
	if l == "" {
		return nil // use DefaultLabels
	}
	return i18nLabels{lang: l}
}

func analysisOptions(hw hpaanalysis.HealthWeights, debug bool) hpaanalysis.AnalysisOptions {
	return hpaanalysis.AnalysisOptions{
		HealthWeights: hw,
		Debug:         debug,
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
	case "markdown", "md":
		return writeMarkdown(out, value)
	case "html":
		return writeHTML(out, value)
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

// outputConfig holds the output-related fields needed by outputSelection,
// decoupled from the full options struct.
type outputConfig struct {
	report          string
	output          string
	template        string
	outputTemplates map[string]outputTemplateConfig
}

func outputSelection(cfg outputConfig) (string, string) {
	if cfg.report != "" {
		switch cfg.report {
		case "markdown", "md":
			return "markdown", ""
		case "html":
			return "html", ""
		}
	}
	format := cfg.output
	templateStr := cfg.template
	if len(cfg.outputTemplates) == 0 || format == "" {
		return format, templateStr
	}
	if tpl, ok := cfg.outputTemplates[format]; ok {
		if tpl.Type == "" {
			return "go-template", tpl.Template
		}
		return normalizeTemplateType(tpl.Type), tpl.Template
	}
	for _, prefix := range []string{"jsonpath=", "jsonpath:", "template=", "template:", "go-template=", "go-template:"} {
		name, ok := strings.CutPrefix(format, prefix)
		if !ok {
			continue
		}
		tpl, exists := cfg.outputTemplates[name]
		if !exists {
			return format, templateStr
		}
		if tpl.Type == "" {
			if strings.HasPrefix(prefix, "jsonpath") {
				return "jsonpath", tpl.Template
			}
			return "go-template", tpl.Template
		}
		return normalizeTemplateType(tpl.Type), tpl.Template
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

func writeMarkdown(out io.Writer, value any) error {
	switch report := value.(type) {
	case hpaanalysis.StatusReport:
		return hpaanalysis.WriteMarkdownReport(out, report)
	case []hpaanalysis.StatusReport:
		for i, r := range report {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if err := hpaanalysis.WriteMarkdownReport(out, r); err != nil {
				return err
			}
		}
		return nil
	case hpaanalysis.ListReport:
		return hpaanalysis.WriteMarkdownListReport(out, report)
	default:
		return fmt.Errorf("markdown output requires a StatusReport or ListReport, got %T", value)
	}
}

func writeHTML(out io.Writer, value any) error {
	switch report := value.(type) {
	case hpaanalysis.StatusReport:
		return hpaanalysis.WriteHTMLReport(out, report)
	case []hpaanalysis.StatusReport:
		for i, r := range report {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if err := hpaanalysis.WriteHTMLReport(out, r); err != nil {
				return err
			}
		}
		return nil
	case hpaanalysis.ListReport:
		return hpaanalysis.WriteHTMLListReport(out, report)
	default:
		return fmt.Errorf("html output requires a StatusReport or ListReport, got %T", value)
	}
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
