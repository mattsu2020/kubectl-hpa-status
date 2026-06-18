package cmd

import (
	"io"
	"os"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/i18n"
	"github.com/mattsu2020/kubectl-hpa-status/internal/render"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"golang.org/x/term"
)

// shouldColorize returns true when the caller wants color and the writer is
// connected to a terminal.
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

func stdinIsTerminal(in io.Reader) bool {
	if in == nil {
		return false
	}
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
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

// summaryTranslatorForLang returns a function that localises the Analysis.Summary
// string (produced by pkg/hpa.SummarizeDirection in English) for the given
// language. When the language is empty/unset, it returns nil so the Summary is
// rendered verbatim.
//
// The mapping is keyed on the English source strings emitted by
// summarizeDirectionFromReplicas and SummarizeDirection in pkg/hpa; those are
// the only values that ever land in Analysis.Summary. pkg/hpa cannot import
// internal/i18n (internal package visibility), so the translation is injected
// here via StatusTextOptions.SummaryTranslator.
func summaryTranslatorForLang(lang, output string) func(string) string {
	l := outputLang(lang, output)
	if l == "" {
		return nil
	}
	return func(summary string) string {
		key, ok := summaryTranslationKey(summary)
		if !ok {
			return summary
		}
		return i18n.Get(l, key)
	}
}

// summaryTranslationKey maps an English Summary string to its i18n locale key.
// Every string emitted by pkg/hpa.SummarizeDirection (metric_utils.go) has a
// matching dir_* entry in locales/{en,ja}.yaml, so a Summary is always
// translated when a translator is wired in; returns ok=false only for strings
// the analysis layer may set outside SummarizeDirection.
func summaryTranslationKey(summary string) (string, bool) {
	switch summary {
	case "HPA currently wants to scale up.":
		return "dir_scale_up", true
	case "HPA currently wants to scale down.":
		return "dir_scale_down", true
	case "HPA is at maxReplicas.":
		return "dir_at_max", true
	case "HPA is at minReplicas.":
		return "dir_at_min", true
	case "HPA is at minReplicas (scale-to-zero enabled).":
		return "dir_at_min_scale_to_zero", true
	case "HPA currently keeps the replica count unchanged.":
		return "dir_unchanged", true
	case "HPA has no visible desired replica recommendation in status.":
		return "dir_no_recommendation", true
	case "HPA cannot currently compute a scaling recommendation from metrics.":
		return "dir_inactive", true
	case "HPA wants to scale to zero (cold start will occur on next scale-up).":
		return "dir_scale_to_zero", true
	case "HPA is scaled to zero (minReplicas=0); awaiting trigger to scale up.":
		return "dir_scaled_to_zero", true
	case "HPA data is unavailable.":
		return "dir_unavailable", true
	default:
		return "", false
	}
}

func analysisOptions(hw hpaanalysis.HealthWeights, debug bool) hpaanalysis.AnalysisOptions {
	return hpaanalysis.AnalysisOptions{HealthWeights: hw, Debug: debug}
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

// --- render facade ---
// The pure format-routing/serialization functions now live in internal/render.
// These unexported wrappers keep the historical cmd/ call sites compiling; they
// should migrate to render.* directly when the cmd/ sub-package split lands.
// Only the functions actually called from cmd/ are re-exported.

func writeOutput(out io.Writer, format string, templateStr string, value any, writeText func() error) error {
	return render.Format(out, format, templateStr, value, writeText)
}

func parsePrefixedFormat(format string) (expr string, kind string, ok bool) {
	return render.ParsePrefixedFormat(format)
}

func writePrometheusMetrics(w io.Writer, namespace, name string, healthScore int, current, desired, minR, maxR int32) error {
	return render.PrometheusMetrics(w, namespace, name, healthScore, current, desired, minR, maxR)
}

func escapePrometheusLabelValue(s string) string {
	return render.EscapePrometheusLabelValue(s)
}

func writeError(out io.Writer, format string, err error) {
	render.Error(out, format, err)
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
	if reportFormat, ok := reportFormatSelection(cfg.report); ok {
		return reportFormat, ""
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

// reportFormatSelection maps a --report value to its fixed output format.
// Returns ok=false when the report value does not pin a format.
func reportFormatSelection(report string) (string, bool) {
	switch report {
	case "markdown", "md":
		return "markdown", true
	case "html":
		return "html", true
	case "incident":
		return "incident", true
	case "junit":
		return "junit", true
	case "sarif":
		return "sarif", true
	default:
		return "", false
	}
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
