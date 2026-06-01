package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
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

func loadConfigFile(path string) (configFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return configFile{}, err
	}
	var cfg configFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return configFile{}, err
	}
	return cfg, nil
}

func applyConfig(cmd *cobra.Command, opts *options, cfg configFile) {
	if cfg.Namespace != "" && !persistentFlagChanged(cmd, "namespace") {
		opts.namespace = cfg.Namespace
	}
	if cfg.AllNamespaces != nil && !persistentFlagChanged(cmd, "all-namespaces") {
		opts.allNamespaces = *cfg.AllNamespaces
	}
	if cfg.Output != "" && !persistentFlagChanged(cmd, "output") {
		opts.output = cfg.Output
	}
	if cfg.Wide != nil && !persistentFlagChanged(cmd, "wide") {
		opts.wide = *cfg.Wide
	}
	if cfg.Selector != "" && !persistentFlagChanged(cmd, "selector") {
		opts.selector = cfg.Selector
	}
	if cfg.Color != "" && !persistentFlagChanged(cmd, "color") {
		opts.color = cfg.Color
	}
	if cfg.Lang != "" && !persistentFlagChanged(cmd, "lang") {
		opts.lang = cfg.Lang
	}
	if cfg.Debug != nil && !persistentFlagChanged(cmd, "debug") {
		opts.debug = *cfg.Debug
	}
	if cfg.Dashboard != nil && !persistentFlagChanged(cmd, "dashboard") {
		opts.dashboard = *cfg.Dashboard
	}
	if cfg.Events != nil && !persistentFlagChanged(cmd, "events") {
		opts.events.enabled = true
		opts.events.limit = *cfg.Events
	}
	if cfg.EventsEnabled != nil && !persistentFlagChanged(cmd, "events") {
		opts.events.enabled = *cfg.EventsEnabled
	}
	if cfg.SortBy != "" && !localFlagChanged(cmd, "sort-by") {
		opts.sortBy = cfg.SortBy
	}
	if cfg.Filter != "" && !localFlagChanged(cmd, "filter") {
		opts.filter = cfg.Filter
	}
	if cfg.MinScore != nil && !localFlagChanged(cmd, "min-score") {
		opts.healthScoreMin = *cfg.MinScore
	}
	if cfg.MaxScore != nil && !localFlagChanged(cmd, "max-score") && !localFlagChanged(cmd, "health-score") {
		opts.healthScoreMax = *cfg.MaxScore
	}
	if cfg.HealthScore != nil && !localFlagChanged(cmd, "health-score") && !localFlagChanged(cmd, "max-score") {
		opts.healthScoreMax = *cfg.HealthScore
	}
	if cfg.HealthWeights != (hpaanalysis.HealthWeights{}) {
		opts.healthWeights = cfg.HealthWeights
	}
}

func persistentFlagChanged(cmd *cobra.Command, name string) bool {
	root := cmd.Root()
	if root == nil {
		return false
	}
	flag := root.PersistentFlags().Lookup(name)
	return flag != nil && flag.Changed
}

func localFlagChanged(cmd *cobra.Command, name string) bool {
	for current := cmd; current != nil; current = current.Parent() {
		flag := current.Flags().Lookup(name)
		if flag != nil {
			return flag.Changed
		}
	}
	return false
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

func writePrometheusMetrics(w io.Writer, namespace, name string, healthScore int, current, desired, min, max int32) error {
	type metric struct {
		name  string
		help  string
		value any
	}
	metrics := []metric{
		{name: "hpa_health_score", help: "Health score of an HPA (0-100)", value: healthScore},
		{name: "hpa_current_replicas", help: "Current replica count", value: current},
		{name: "hpa_desired_replicas", help: "Desired replica count", value: desired},
		{name: "hpa_min_replicas", help: "Minimum replica count", value: min},
		{name: "hpa_max_replicas", help: "Maximum replica count", value: max},
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
