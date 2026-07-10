package hpa

import (
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// SchemaVersion is the API version embedded in JSON/YAML output so consumers
// can detect schema drift. Bump when a breaking field rename/removal occurs;
// additive fields do not require a bump. Current shape: "hpa-status/v1".
const SchemaVersion = "hpa-status/v1"

// StatusReport holds the analysis result and events for a single HPA.
type StatusReport struct {
	// APIVersion identifies the JSON/YAML schema version. Consumers should
	// check this before parsing to detect incompatible schema changes.
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Analysis   Analysis `json:"analysis" yaml:"analysis"`
	Events     []Event  `json:"events,omitempty" yaml:"events,omitempty"`
}

// StatusTextOptions configures text output rendering with theme, language, fix mode, and diff display.
type StatusTextOptions struct {
	Theme         style.Theme
	Lang          string
	Fix           bool
	Diff          bool
	HiddenFactors bool
	Labels        LabelProvider // When nil, English defaults are used. Takes precedence over Lang.
	// SummaryTranslator, when non-nil, localises the Analysis.Summary line.
	// It receives both the English summary text and the stable SummaryKey
	// (e.g. "dir_scale_up") produced by pkg/hpa.SummarizeDirectionWithKey;
	// translators should prefer the key for locale lookup and fall back to
	// summary when the key is empty (Summary was overwritten outside the
	// direction switch). pkg/hpa cannot import the i18n package, so the cmd
	// layer injects i18n.Get here. When nil, the Summary is rendered verbatim
	// (English).
	SummaryTranslator func(summary, key string) string
}

// translateSummary applies opts.SummaryTranslator to a summary string when
// configured, returning the original string otherwise. Keeps the two render
// sites consistent without repeating the nil check.
func (o StatusTextOptions) translateSummary(s, key string) string {
	if o.SummaryTranslator != nil {
		return o.SummaryTranslator(s, key)
	}
	return s
}

// WatchState holds the previous and current Analysis for diff display.
type WatchState struct {
	Previous *Analysis
	Current  *Analysis
}

// WriteStatusText writes a plain text status report using the given theme.
func WriteStatusText(w io.Writer, report StatusReport, theme style.Theme) error {
	return WriteStatusTextWithOptions(w, report, StatusTextOptions{Theme: theme})
}

// WriteScalePathText writes only the scale path section.
func WriteScalePathText(w io.Writer, path *ScalePath) error {
	if path == nil {
		_, err := fmt.Fprintln(w, "Scale Path:\n  No scale path data available.")
		return err
	}
	var out []byte
	appendScalePathText(&out, path, style.Theme{})
	_, err := w.Write(out)
	return err
}

// WriteStatusDashboard writes a compact dashboard format status report.
func WriteStatusDashboard(w io.Writer, report StatusReport, theme style.Theme) error {
	return WriteStatusDashboardWithOptions(w, report, StatusTextOptions{Theme: theme})
}

// WriteStatusDashboardWithOptions writes a compact dashboard format status
// report with full rendering options, localising the Summary line via
// opts.SummaryTranslator when configured.
func WriteStatusDashboardWithOptions(w io.Writer, report StatusReport, opts StatusTextOptions) error {
	a := report.Analysis
	theme := opts.Theme
	diff := a.Desired - a.Current
	var out []byte
	out = fmt.Appendf(out, "kubectl-hpa-status dashboard\n")
	out = fmt.Appendf(out, "HPA      %s/%s\n", a.Namespace, a.Name)
	out = fmt.Appendf(out, "Target   %s\n", a.Target)
	out = fmt.Appendf(out, "Health   %s %d/100\n", theme.HealthLabel(a.Health, a.HealthScore), a.HealthScore)
	out = fmt.Appendf(out, "Replicas current=%d desired=%d diff=%+d min=%d max=%d\n", a.Current, a.Desired, diff, a.Min, a.Max)
	if a.StabilizationRemaining != nil && *a.StabilizationRemaining > 0 {
		progress := FormatStabilizationWithSource(a.StabilizationRemaining, a.StabilizationWindowSeconds, a.StabilizationSource)
		out = fmt.Appendf(out, "Stabilizing %s\n", progress)
	}
	out = fmt.Appendf(out, "Summary  %s\n", theme.SummaryColor(opts.translateSummary(a.Summary, a.SummaryKey)))

	out = append(out, "\nConditions\n"...)
	if len(a.Conditions) == 0 {
		out = append(out, "  No conditions reported.\n"...)
	} else {
		for _, condition := range a.Conditions {
			out = fmt.Appendf(out, "  %-15s %s %-24s %s\n",
				SanitizeTerminalText(string(condition.Type)), theme.ConditionStatus(condition.Type, condition.Status),
				SanitizeTerminalText(condition.Reason), SanitizeTerminalText(condition.Message))
		}
	}

	out = append(out, "\nMetrics\n"...)
	if len(a.Metrics) == 0 {
		out = append(out, "  No current metrics reported.\n"...)
	} else {
		for _, metric := range a.Metrics {
			name := metric.Name
			if name == "" {
				name = metric.Type
			}
			bar := "──────────"
			if metric.Ratio != nil {
				bar = progressBar(*metric.Ratio)
			}
			out = fmt.Appendf(out, "  %-24s %s current=%s target=%s %s\n", name, bar, metric.Current, metric.Target, theme.MetricNote(metric.Note))
		}
	}

	if len(a.Actions) > 0 {
		out = append(out, "\nNext actions\n"...)
		for _, action := range a.Actions {
			out = fmt.Appendf(out, "  - %s\n", theme.ActionLine(action))
		}
	}

	_, err := w.Write(out)
	return err
}

// WriteStatusTextWithOptions writes a status report with full rendering options.
// The rendering is organized as a sequence of independent section renderers;
// each append*Section helper appends its output only when the corresponding
// analysis field is populated. See text_sections.go and text_extras.go for the
// section bodies.
func WriteStatusTextWithOptions(w io.Writer, report StatusReport, opts StatusTextOptions) error {
	// Pass Analysis by pointer to the per-section renderers: Analysis has 63
	// fields and each append* helper would otherwise copy it.
	a := &report.Analysis
	theme := opts.Theme
	labels := resolveLabels(opts.Labels)
	var out []byte
	out = fmt.Appendf(out, "HPA %s/%s\n", a.Namespace, a.Name)
	out = fmt.Appendf(out, "%s: %s\n", labels.Target, a.Target)

	// Replicas: highlight desired when it differs from current
	desired := theme.ReplicaHighlight(a.Desired, a.Desired != a.Current)
	out = fmt.Appendf(out, "%s: current=%d desired=%s min=%d max=%d\n", labels.Replicas, a.Current, desired, a.Min, a.Max)
	out = fmt.Appendf(out, "%s: %s %d/100\n", labels.Health, theme.HealthLabel(a.Health, a.HealthScore), a.HealthScore)
	appendScoreBreakdown(&out, a)

	out = append(out, '\n')
	out = fmt.Appendf(out, "%s: %s\n", labels.Summary, theme.SummaryColor(opts.translateSummary(a.Summary, a.SummaryKey)))

	appendConditionsSection(&out, a, theme, labels)
	appendMetricsSection(&out, a, theme, labels)
	appendStabilizationAndBehavior(&out, a, theme, labels)
	appendHiddenFactors(&out, a, opts)
	appendHealthTrendSection(&out, a)
	appendControllerProfileSection(&out, a)
	appendActionsSection(&out, a, theme, labels)
	appendSuggestionsSection(&out, a, opts, theme, labels)
	appendInterpretationSection(&out, a, theme, labels)
	appendDebugSection(&out, a, theme, labels)
	appendDecisionSignalsSection(&out, a)
	appendDecisionTraceSection(&out, a)
	appendStructuredDecisionTraceSection(&out, a, opts)
	appendAdapterDiagnosticsSection(&out, a)
	appendKEDASection(&out, a, theme, labels)
	appendVPASection(&out, a, theme)
	appendMetricsDiagnosticsSection(&out, a, theme, labels)
	appendMetricFreshnessSection(&out, a, theme, labels)
	appendResourceCheckSection(&out, a)
	appendPodAnalysisSection(&out, a, labels)
	appendSimulationSection(&out, a, theme, labels)
	appendCapacityContextSection(&out, a, theme, labels)
	appendCapacityHeadroomSection(&out, a, theme)
	appendReadinessImpactSection(&out, a, theme)
	appendScalePathSection(&out, a, theme)
	appendRolloutDiagnosisSection(&out, a, theme)
	appendBlockerReportSection(&out, a, theme, labels)
	appendCapacityPlanSection(&out, a, theme, labels)
	appendMetricContractSection(&out, a, theme)
	appendContainerAdvisorSection(&out, a, labels)
	appendBehaviorAdvisorSection(&out, a, labels)
	appendFlappingPreventionSection(&out, a, labels)
	appendMetricHintsSection(&out, a)
	appendEventsSection(&out, report, labels)
	appendWarningsSection(&out, a, labels)

	_, err := w.Write(out)
	return err
}

type labels struct {
	Target              string
	Replicas            string
	Health              string
	Summary             string
	Conditions          string
	Metrics             string
	Behavior            string
	Actions             string
	Suggestions         string
	Fix                 string
	Interpretation      string
	Debug               string
	KEDA                string
	Events              string
	Risk                string
	Precondition        string
	Warning             string
	MetricsDiagnostics  string
	MetricFreshness     string
	PodAnalysis         string
	Simulation          string
	CapacityContext     string
	Timeline            string
	MetricDecisionTrace string
	AuditFindings       string
	AuditScore          string
	AuditSeverity       string
	Blockers            string
	NextCommands        string
	CapacityPlan        string
	MetricContract      string
	Warmup              string
	ContainerAdvisor    string
	BehaviorAdvisor     string
	FlappingPrevention  string
}
