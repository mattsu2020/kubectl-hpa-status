package hpa

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// StatusReport holds the analysis result and events for a single HPA.
type StatusReport struct {
	Analysis Analysis `json:"analysis" yaml:"analysis"`
	Events   []Event  `json:"events,omitempty" yaml:"events,omitempty"`
}

// StatusTextOptions configures text output rendering with theme, language, fix mode, and diff display.
type StatusTextOptions struct {
	Theme         style.Theme
	Lang          string
	Fix           bool
	Diff          bool
	HiddenFactors bool
	Labels        LabelProvider // When nil, English defaults are used. Takes precedence over Lang.
	// SummaryTranslator, when non-nil, localises the Analysis.Summary line
	// (e.g. "HPA currently wants to scale up."). pkg/hpa cannot import the
	// i18n package, so the cmd layer injects i18n.Get here. When nil, the
	// Summary is rendered verbatim (English).
	SummaryTranslator func(string) string
}

// translateSummary applies opts.SummaryTranslator to a summary string when
// configured, returning the original string otherwise. Keeps the two render
// sites consistent without repeating the nil check.
func (o StatusTextOptions) translateSummary(s string) string {
	if o.SummaryTranslator != nil {
		return o.SummaryTranslator(s)
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
	a := report.Analysis
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
	out = fmt.Appendf(out, "Summary  %s\n", theme.SummaryColor(a.Summary))

	out = append(out, "\nConditions\n"...)
	if len(a.Conditions) == 0 {
		out = append(out, "  No conditions reported.\n"...)
	} else {
		for _, condition := range a.Conditions {
			out = fmt.Appendf(out, "  %-15s %s %-24s %s\n", condition.Type, theme.ConditionStatus(condition.Type, condition.Status), condition.Reason, condition.Message)
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

// triggerStatusBadge returns a display string with a visual indicator for a KEDA trigger status.
func triggerStatusBadge(status string, theme style.Theme) string {
	switch status {
	case "Active":
		return theme.OK.Render("Active ✓")
	case "Inactive":
		return theme.Error.Render("Inactive ✗")
	default:
		return theme.Dim.Render("Unknown ?")
	}
}

// metricsDiagnosticsStatus returns a themed display string for the overall metrics diagnostics status.
func metricsDiagnosticsStatus(status string, theme style.Theme) string {
	switch status {
	case "healthy":
		return theme.OK.Render("healthy")
	case "degraded":
		return theme.Bold.Render("degraded")
	case "error":
		return theme.Error.Render("error")
	default:
		return theme.Dim.Render(status)
	}
}

// metricsDiagnosticsIndicator returns a themed status indicator for a per-metric health check.
func metricsDiagnosticsIndicator(status string, theme style.Theme) string {
	switch status {
	case "healthy":
		return theme.OK.Render("✓")
	case "missing":
		return theme.Error.Render("✗")
	case "stale":
		return theme.Bold.Render("!")
	default:
		return theme.Dim.Render("?")
	}
}

// WriteStatusTextWithOptions writes a status report with full rendering options.
// The rendering is organized as a sequence of independent section renderers;
// each append*Section helper appends its output only when the corresponding
// analysis field is populated. See text_sections.go for the section bodies.
//
//nolint:gocyclo // Sequential delegation to per-section renderers; the orchestrator's complexity is just a flat list of calls.
func WriteStatusTextWithOptions(w io.Writer, report StatusReport, opts StatusTextOptions) error {
	a := report.Analysis
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
	out = fmt.Appendf(out, "%s: %s\n", labels.Summary, theme.SummaryColor(opts.translateSummary(a.Summary)))

	appendConditionsSection(&out, a, theme, labels)
	appendMetricsSection(&out, a, theme, labels)
	appendStabilizationAndBehavior(&out, a, theme, labels)
	appendHiddenFactors(&out, a, opts)

	if a.HealthTrend != nil && len(a.HealthTrend.Snapshots) > 0 {
		out = append(out, '\n')
		trendText := FormatTrendText(*a.HealthTrend)
		out = fmt.Appendf(out, "%s\n", trendText)
	}

	if a.ControllerProfile != nil {
		out = append(out, '\n')
		appendControllerProfileText(&out, a.ControllerProfile)
	}

	if len(a.Actions) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.Actions)
		for _, action := range a.Actions {
			out = fmt.Appendf(out, "  - %s\n", theme.ActionLine(action))
		}
	}

	appendSuggestionsSection(&out, a, opts, theme, labels)

	if len(a.Interpretation) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.Interpretation)
		for _, line := range a.Interpretation {
			out = fmt.Appendf(out, "  - %s\n", theme.InterpretationLine(line))
		}
	}

	if len(a.Debug) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.Debug)
		for _, line := range a.Debug {
			out = fmt.Appendf(out, "  - %s\n", theme.Dim.Render(line))
		}
	}

	if len(a.DecisionSignals) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s\n", FormatDecisionSignals(a.DecisionSignals))
	}

	if a.DecisionTrace != nil {
		out = append(out, '\n')
		AppendDecisionTraceText(&out, a.DecisionTrace)
	}

	if a.StructuredDecisionTrace != nil {
		out = append(out, '\n')
		AppendStructuredDecisionTraceText(&out, a.StructuredDecisionTrace, opts.Labels)
	}

	if a.AdapterDiagnostics != nil {
		out = append(out, '\n')
		AppendAdapterDiagnosticsText(&out, a.AdapterDiagnostics)
	}

	appendKEDASection(&out, a, theme, labels)
	appendVPASection(&out, a, theme)
	appendMetricsDiagnosticsSection(&out, a, theme, labels)
	appendMetricFreshnessSection(&out, a, theme, labels)
	appendResourceCheckSection(&out, a)
	appendPodAnalysisSection(&out, a, labels)
	appendSimulationSection(&out, a, theme, labels)
	appendCapacityContextSection(&out, a, theme, labels)

	if a.CapacityHeadroom != nil {
		out = append(out, '\n')
		appendCapacityHeadroomText(&out, a.CapacityHeadroom, theme)
	}

	if a.ReadinessImpact != nil {
		out = append(out, '\n')
		appendReadinessImpactText(&out, a.ReadinessImpact, theme)
	}

	if a.ScalePath != nil {
		out = append(out, '\n')
		appendScalePathText(&out, a.ScalePath, theme)
	}

	if a.RolloutDiagnosis != nil {
		out = append(out, '\n')
		appendRolloutDiagnosisText(&out, a.RolloutDiagnosis, theme)
	}

	if a.BlockerReport != nil {
		AppendBlockerText(&out, a.BlockerReport, theme, labels)
		appendScaleoutBlockersText(&out, a.BlockerReport, theme)
	}

	if a.CapacityPlan != nil {
		AppendCapacityPlanText(&out, a.CapacityPlan, theme, labels)
	}

	if a.MetricContract != nil {
		out = append(out, '\n')
		appendMetricContractText(&out, a.MetricContract, theme)
	}

	if a.ContainerAdvisor != nil {
		AppendContainerAdvisorText(&out, a.ContainerAdvisor, labels)
	}

	if a.BehaviorAdvisor != nil {
		AppendBehaviorAdvisorText(&out, a.BehaviorAdvisor, labels)
	}

	if a.FlappingPrevention != nil {
		AppendFlappingPreventionText(&out, a.FlappingPrevention, labels)
	}

	if a.MetricHints != nil && len(a.MetricHints.TroubleshootingFlows) > 0 {
		out = append(out, '\n')
		out = append(out, "Metric Troubleshooting:\n"...)
		for _, flow := range a.MetricHints.TroubleshootingFlows {
			out = fmt.Appendf(out, "  [%s] %s (%s/%s)\n", flow.Severity, flow.Title, flow.MetricType, flow.MetricName)
			for _, step := range flow.Steps {
				out = fmt.Appendf(out, "    %d. %s\n", step.StepNumber, step.Description)
				if step.Command != "" {
					out = fmt.Appendf(out, "       $ %s\n", step.Command)
				}
			}
		}
	}

	appendEventsSection(&out, report, labels)

	_, err := w.Write(out)
	return err
}

func emptyAsUnknown(value string) string {
	if value == "" {
		return "<unknown>"
	}
	return value
}

func indentBlock(text string, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
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

// metricFreshnessIndicator returns a themed status indicator for a metric
// freshness entry.
func metricFreshnessIndicator(status string, theme style.Theme) string {
	switch status {
	case string(FreshnessOK):
		return theme.OK.Render("✓")
	case string(FreshnessMissing):
		return theme.Error.Render("✗")
	case string(FreshnessStale):
		return theme.Bold.Render("!")
	default:
		return theme.Dim.Render("?")
	}
}

// metricFreshnessStatusDisplay returns a themed display string for the
// freshness status label.
func metricFreshnessStatusDisplay(status string, theme style.Theme) string {
	switch status {
	case string(FreshnessOK):
		return theme.OK.Render(string(FreshnessOK))
	case string(FreshnessMissing):
		return theme.Error.Render(string(FreshnessMissing))
	case string(FreshnessStale):
		return theme.Bold.Render(string(FreshnessStale))
	default:
		return theme.Dim.Render("UNKNOWN")
	}
}

// formatFreshnessDuration returns a human-readable duration string
// (e.g., "12s", "4m32s", "1h5m").
func formatFreshnessDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int64(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm%ds", minutes, secs)
	}
	hours := minutes / 60
	mins := minutes % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}
