package cmd

import (
	"io"
	"strings"

	outpkg "github.com/mattsu2020/kubectl-hpa-status/cmd/internal/output"
	"github.com/mattsu2020/kubectl-hpa-status/internal/i18n"
	"github.com/mattsu2020/kubectl-hpa-status/internal/render"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// shouldColorize re-exports cmd/internal/output.ShouldColorize under the
// unexported name the rest of cmd/ already uses. When the cmd/ sub-package
// split lands, callers should migrate to outpkg.ShouldColorize directly and
// this wrapper can be deleted.
func shouldColorize(mode string, out io.Writer) bool {
	return outpkg.ShouldColorize(mode, out)
}

// outputLang re-exports cmd/internal/output.Lang (see shouldColorize).
func outputLang(lang, output string) string {
	return outpkg.Lang(lang, output)
}

// stdinIsTerminal re-exports cmd/internal/output.StdinIsTerminal.
func stdinIsTerminal(in io.Reader) bool {
	return outpkg.StdinIsTerminal(in)
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
// string for the given language, keyed on the stable SummaryKey produced by
// pkg/hpa.SummarizeDirectionWithKey. When the language is empty/unset it
// returns nil so the Summary is rendered verbatim. When the key is empty
// (Summary was overwritten outside SummarizeDirection, e.g. the stale prefix),
// the original English summary is returned unchanged. pkg/hpa cannot import
// internal/i18n (internal package visibility), so the translation is injected
// here via StatusTextOptions.SummaryTranslator.
func summaryTranslatorForLang(lang, output string) func(string, string) string {
	l := outputLang(lang, output)
	if l == "" {
		return nil
	}
	return func(summary, key string) string {
		if key == "" {
			return summary
		}
		return i18n.Get(l, key)
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

// normalizeOutputFormat lowercases and trims an output-format value without
// collapsing separators (unlike normalizeSelector). It is used to classify a
// raw --output value into one of the canonical streaming-eligible formats.
func normalizeOutputFormat(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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

// writeErrorIfStructured emits an error in the requested structured format
// (json/yaml) and is a no-op for text output. It collapses the repeated
// `if output == "json" || output == "yaml" { writeError(...) }` idiom.
func writeErrorIfStructured(out io.Writer, output string, err error) {
	if output == "json" || output == "yaml" {
		writeError(out, output, err)
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

// selectOutputFromOptions is the common path through outputSelection: it reads
// the four output-related fields straight off an options value. Callers that
// operate on a mutated copy of options (e.g. after copyOptions) or only need a
// subset of the fields can still build an outputConfig literal directly.
func selectOutputFromOptions(opts *options) (string, string) {
	return outputSelection(outputConfig{
		report:          opts.Report,
		output:          opts.Output,
		template:        opts.Template,
		outputTemplates: opts.OutputTemplates,
	})
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
