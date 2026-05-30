package hpa

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type StatusReport struct {
	Analysis Analysis `json:"analysis" yaml:"analysis"`
	Events   []Event  `json:"events,omitempty" yaml:"events,omitempty"`
}

type StatusTextOptions struct {
	Theme style.Theme
	Lang  string
	Fix   bool
}

// WatchState holds the previous and current Analysis for diff display.
type WatchState struct {
	Previous *Analysis
	Current  *Analysis
}

func WriteStatusText(w io.Writer, report StatusReport, theme style.Theme) error {
	return WriteStatusTextWithOptions(w, report, StatusTextOptions{Theme: theme})
}

func WriteStatusTextWithOptions(w io.Writer, report StatusReport, opts StatusTextOptions) error {
	a := report.Analysis
	theme := opts.Theme
	labels := textLabels(opts.Lang)
	var out []byte
	out = fmt.Appendf(out, "HPA %s/%s\n", a.Namespace, a.Name)
	out = fmt.Appendf(out, "%s: %s\n", labels.Target, a.Target)

	// Replicas: highlight desired when it differs from current
	desired := theme.ReplicaHighlight(a.Desired, a.Desired != a.Current)
	out = fmt.Appendf(out, "%s: current=%d desired=%s min=%d max=%d\n", labels.Replicas, a.Current, desired, a.Min, a.Max)
	out = fmt.Appendf(out, "%s: %s %d/100\n", labels.Health, theme.HealthLabel(a.Health), a.HealthScore)

	out = append(out, '\n')
	out = fmt.Appendf(out, "%s: %s\n", labels.Summary, theme.SummaryColor(a.Summary))

	out = append(out, '\n')
	out = fmt.Appendf(out, "%s:\n", labels.Conditions)
	if len(a.Conditions) == 0 {
		out = append(out, "  No conditions reported.\n"...)
	} else {
		for _, condition := range a.Conditions {
			statusText := theme.ConditionStatus(condition.Type, condition.Status)
			out = fmt.Appendf(out, "  %-15s %-7s %-24s %s\n", condition.Type, statusText, condition.Reason, condition.Message)
		}
	}

	out = append(out, '\n')
	out = fmt.Appendf(out, "%s:\n", labels.Metrics)
	if len(a.Metrics) == 0 {
		out = append(out, "  No current metrics reported.\n"...)
	} else {
		for _, metric := range a.Metrics {
			note := theme.MetricNote(metric.Note)
			text := formatMetricText(metric, note)
			out = fmt.Appendf(out, "  - %s\n", text)
		}
	}

	if len(a.Behavior) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.Behavior)
		for _, behavior := range a.Behavior {
			out = fmt.Appendf(out, "  - %s\n", behavior.Text)
		}
	}

	if len(a.Actions) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.Actions)
		for _, action := range a.Actions {
			out = fmt.Appendf(out, "  - %s\n", theme.ActionLine(action))
		}
	}

	if len(a.Suggestions) > 0 {
		out = append(out, '\n')
		if opts.Fix {
			out = fmt.Appendf(out, "%s:\n", labels.Fix)
		} else {
			out = fmt.Appendf(out, "%s:\n", labels.Suggestions)
		}
		for _, suggestion := range a.Suggestions {
			out = fmt.Appendf(out, "  - %s: %s", suggestion.Title, suggestion.Description)
			if suggestion.Risk != "" {
				out = fmt.Appendf(out, " (risk: %s)", suggestion.Risk)
			}
			out = append(out, '\n')
			if suggestion.Command != "" {
				out = fmt.Appendf(out, "    $ %s\n", theme.ActionLine(suggestion.Command))
			}
		}
	}

	if len(a.Interpretation) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.Interpretation)
		for _, line := range a.Interpretation {
			out = fmt.Appendf(out, "  - %s\n", theme.InterpretationLine(line))
		}
	}

	out = append(out, '\n')
	out = fmt.Appendf(out, "%s:\n", labels.Events)
	if len(report.Events) == 0 {
		out = append(out, "  No recent events found.\n"...)
	} else {
		for _, event := range report.Events {
			out = fmt.Appendf(out, "  - %s: %s\n", event.Reason, event.Message)
		}
	}

	_, err := w.Write(out)
	return err
}

type labels struct {
	Target         string
	Replicas       string
	Health         string
	Summary        string
	Conditions     string
	Metrics        string
	Behavior       string
	Actions        string
	Suggestions    string
	Fix            string
	Interpretation string
	Events         string
}

func textLabels(lang string) labels {
	if strings.EqualFold(lang, "ja") {
		return labels{
			Target:         "対象",
			Replicas:       "レプリカ",
			Health:         "ヘルススコア",
			Summary:        "要約",
			Conditions:     "状態",
			Metrics:        "メトリクス",
			Behavior:       "Behavior",
			Actions:        "推奨アクション",
			Suggestions:    "推奨コマンド",
			Fix:            "修正プラン",
			Interpretation: "解釈",
			Events:         "最近のイベント",
		}
	}
	return labels{
		Target:         "Target",
		Replicas:       "Replicas",
		Health:         "Health score",
		Summary:        "Summary",
		Conditions:     "Conditions",
		Metrics:        "Metrics",
		Behavior:       "Behavior",
		Actions:        "Recommended actions",
		Suggestions:    "Recommended commands",
		Fix:            "Fix plan",
		Interpretation: "Interpretation",
		Events:         "Recent events",
	}
}

// WriteStatusDiff writes a status display that highlights changes between the
// previous and current analysis. Changed fields are shown with emphasis;
// unchanged fields are dimmed when the theme supports it.
func WriteStatusDiff(w io.Writer, state WatchState, theme style.Theme) error {
	// When there is no previous state, fall back to full status display.
	if state.Previous == nil {
		return WriteStatusText(w, StatusReport{Analysis: *state.Current}, theme)
	}
	a := state.Current
	prev := state.Previous
	var out []byte

	out = fmt.Appendf(out, "HPA %s/%s\n", a.Namespace, a.Name)
	out = fmt.Appendf(out, "Target: %s\n", a.Target)

	// Replicas: highlight when changed
	desiredChanged := a.Desired != prev.Desired
	currentChanged := a.Current != prev.Current
	desired := theme.ReplicaHighlight(a.Desired, desiredChanged)
	current := fmt.Sprintf("%d", a.Current)
	if currentChanged {
		current = theme.Bold.Render(current)
	}
	out = fmt.Appendf(out, "Replicas: current=%s desired=%s min=%d max=%d\n", current, desired, a.Min, a.Max)

	out = append(out, '\n')
	summaryChanged := a.Summary != prev.Summary
	summaryText := theme.SummaryColor(a.Summary)
	if summaryChanged {
		out = fmt.Appendf(out, "Summary: %s\n", summaryText)
	} else {
		out = fmt.Appendf(out, "Summary: %s\n", theme.Dim.Render(a.Summary))
	}

	// Conditions: highlight changed statuses
	out = append(out, '\n')
	out = append(out, "Conditions:\n"...)
	if len(a.Conditions) == 0 {
		out = append(out, "  No conditions reported.\n"...)
	} else {
		prevCondMap := conditionMap(prev.Conditions)
		for _, condition := range a.Conditions {
			statusText := theme.ConditionStatus(condition.Type, condition.Status)
			prevStatus := prevCondMap[condition.Type]
			if prevStatus != "" && prevStatus != condition.Status {
				out = fmt.Appendf(out, "  %-15s %s (was %s) %-24s %s\n", condition.Type, statusText, prevStatus, condition.Reason, condition.Message)
			} else {
				out = fmt.Appendf(out, "  %-15s %-7s %-24s %s\n", condition.Type, statusText, condition.Reason, condition.Message)
			}
		}
	}

	// Metrics: highlight changed values
	out = append(out, '\n')
	out = append(out, "Metrics:\n"...)
	if len(a.Metrics) == 0 {
		out = append(out, "  No current metrics reported.\n"...)
	} else {
		prevMetricMap := metricMap(prev.Metrics)
		for _, metric := range a.Metrics {
			note := theme.MetricNote(metric.Note)
			text := formatMetricText(metric, note)
			if prevText, ok := prevMetricMap[metric.Name]; ok && prevText != metric.Text {
				out = fmt.Appendf(out, "  - %s\n", text)
			} else {
				out = fmt.Appendf(out, "  - %s\n", theme.Dim.Render(text))
			}
		}
	}

	if len(a.Behavior) > 0 {
		out = append(out, '\n')
		out = append(out, "Behavior:\n"...)
		for _, behavior := range a.Behavior {
			out = fmt.Appendf(out, "  - %s\n", behavior.Text)
		}
	}

	if len(a.Actions) > 0 {
		out = append(out, '\n')
		out = append(out, "Recommended actions:\n"...)
		for _, action := range a.Actions {
			out = fmt.Appendf(out, "  - %s\n", theme.ActionLine(action))
		}
	}

	if len(a.Interpretation) > 0 {
		out = append(out, '\n')
		out = append(out, "Interpretation:\n"...)
		for _, line := range a.Interpretation {
			out = fmt.Appendf(out, "  - %s\n", theme.InterpretationLine(line))
		}
	}

	_, writeErr := w.Write(out)
	return writeErr
}

// conditionMap builds a map from condition type to status for quick lookup.
func conditionMap(conditions []Condition) map[string]string {
	m := make(map[string]string, len(conditions))
	for _, c := range conditions {
		m[c.Type] = c.Status
	}
	return m
}

// metricMap builds a map from metric name to text for quick lookup.
func metricMap(metrics []Metric) map[string]string {
	m := make(map[string]string, len(metrics))
	for _, metric := range metrics {
		if metric.Name != "" {
			m[metric.Name] = metric.Text
		}
	}
	return m
}

// formatMetricText reconstructs a metric display line using the original Text
// but with the note replaced by a potentially colorized version.
func formatMetricText(m Metric, coloredNote string) string {
	if m.Note == "" || coloredNote == m.Note {
		return m.Text
	}
	return strings.Replace(m.Text, m.Note, coloredNote, 1)
}

type ListItem struct {
	Namespace         string      `json:"namespace" yaml:"namespace"`
	Name              string      `json:"name" yaml:"name"`
	Target            string      `json:"target" yaml:"target"`
	Current           int32       `json:"currentReplicas" yaml:"currentReplicas"`
	Desired           int32       `json:"desiredReplicas" yaml:"desiredReplicas"`
	Min               int32       `json:"minReplicas" yaml:"minReplicas"`
	Max               int32       `json:"maxReplicas" yaml:"maxReplicas"`
	Summary           string      `json:"summary" yaml:"summary"`
	Health            string      `json:"health" yaml:"health"`
	HealthScore       int         `json:"healthScore" yaml:"healthScore"`
	Issue             string      `json:"issue,omitempty" yaml:"issue,omitempty"`
	Metrics           string      `json:"metrics,omitempty" yaml:"metrics,omitempty"`
	Behavior          string      `json:"behavior,omitempty" yaml:"behavior,omitempty"`
	Conditions        string      `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	CreationTimestamp metav1.Time `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"`
}

type ListReport struct {
	Items []ListItem `json:"items" yaml:"items"`
}

type ListTextOptions struct {
	Wide  bool
	Color bool
	Lang  string
	// Theme takes precedence over Color. When Theme is set, Color is ignored.
	Theme style.Theme
}

func (o ListTextOptions) theme() style.Theme {
	if o.Theme.Enabled() || !o.Color {
		return o.Theme
	}
	return style.NewTheme(true)
}

func NewListItem(src Analysis) ListItem {
	var errors []string
	var limiteds []string
	health := src.Health
	if health == "" {
		health = "OK"
	}

	for _, condition := range src.Conditions {
		if condition.Type == "ScalingActive" && condition.Status != "True" {
			errors = append(errors, "ERROR: "+condition.Reason)
		} else if condition.Type == "AbleToScale" && condition.Status != "True" {
			errors = append(errors, "ERROR: "+condition.Reason)
		} else if condition.Type == "ScalingLimited" && condition.Status == "True" {
			limiteds = append(limiteds, "LIMITED: "+condition.Reason)
		}
	}
	if src.Current == src.Desired && src.Current == src.Max {
		limiteds = append(limiteds, "LIMITED: maxReplicas")
	}

	if len(errors) > 0 {
		health = "ERROR"
	} else if len(limiteds) > 0 {
		health = "LIMITED"
	}

	var issues []string
	issues = append(issues, errors...)
	issues = append(issues, limiteds...)
	issue := strings.Join(issues, ", ")

	// Build compact conditions string for wide output
	var condParts []string
	for _, c := range src.Conditions {
		condParts = append(condParts, fmt.Sprintf("%s=%s", c.Type, c.Status))
	}
	conditions := strings.Join(condParts, ";")
	metrics := compactMetrics(src.Metrics)
	behavior := compactBehavior(src.Behavior)
	if src.HealthScore == 0 {
		_, src.HealthScore = healthFromAnalysis(src)
	}

	return ListItem{
		Namespace:         src.Namespace,
		Name:              src.Name,
		Target:            src.Target,
		Current:           src.Current,
		Desired:           src.Desired,
		Min:               src.Min,
		Max:               src.Max,
		Summary:           src.Summary,
		Health:            health,
		HealthScore:       src.HealthScore,
		Issue:             issue,
		Metrics:           metrics,
		Behavior:          behavior,
		Conditions:        conditions,
		CreationTimestamp: src.CreationTimestamp,
	}
}

func healthFromAnalysis(src Analysis) (string, int) {
	score := 100
	switch src.Health {
	case "ERROR":
		score = 50
	case "LIMITED":
		score = 75
	case "STABILIZED":
		score = 90
	}
	return src.Health, score
}

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func WriteListText(w io.Writer, report ListReport, opts ListTextOptions) error {
	t := opts.theme()
	var out []byte
	if opts.Wide {
		out = fmt.Appendf(out, "%s %s %s %s %s %s %s %s %s %s %s %s %s %s\n",
			padRight("NAMESPACE", 20),
			padRight("NAME", 32),
			padRight("TARGET", 28),
			padRight("CURRENT", 8),
			padRight("DESIRED", 8),
			padRight("MIN", 8),
			padRight("MAX", 8),
			padRight("HEALTH", 12),
			padRight("SCORE", 8),
			padRight("METRICS", 20),
			padRight("BEHAVIOR", 28),
			padRight("ISSUE", 32),
			padRight("CONDITIONS", 36),
			"SUMMARY")
		for _, item := range report.Items {
			out = fmt.Appendf(out, "%s %s %s %s %s %s %s %s %s %s %s %s %s %s\n",
				padRight(item.Namespace, 20),
				padRight(item.Name, 32),
				padRight(item.Target, 28),
				padRight(fmt.Sprintf("%d", item.Current), 8),
				padRight(fmt.Sprintf("%d", item.Desired), 8),
				padRight(fmt.Sprintf("%d", item.Min), 8),
				padRight(fmt.Sprintf("%d", item.Max), 8),
				padRight(t.HealthLabel(item.Health), 12),
				padRight(fmt.Sprintf("%d", item.HealthScore), 8),
				padRight(item.Metrics, 20),
				padRight(item.Behavior, 28),
				padRight(t.Issue(item.Issue, item.Health), 32),
				padRight(item.Conditions, 36),
				item.Summary)
		}
		_, err := w.Write(out)
		return err
	}

	out = fmt.Appendf(out, "%s %s %s %s %s %s %s %s\n",
		padRight("NAMESPACE", 20),
		padRight("NAME", 32),
		padRight("CURRENT", 8),
		padRight("DESIRED", 8),
		padRight("HEALTH", 12),
		padRight("SCORE", 8),
		padRight("ISSUE", 32),
		"SUMMARY")
	for _, item := range report.Items {
		out = fmt.Appendf(out, "%s %s %s %s %s %s %s %s\n",
			padRight(item.Namespace, 20),
			padRight(item.Name, 32),
			padRight(fmt.Sprintf("%d", item.Current), 8),
			padRight(fmt.Sprintf("%d", item.Desired), 8),
			padRight(t.HealthLabel(item.Health), 12),
			padRight(fmt.Sprintf("%d", item.HealthScore), 8),
			padRight(t.Issue(item.Issue, item.Health), 32),
			item.Summary)
	}
	_, err := w.Write(out)
	return err
}

func compactMetrics(metrics []Metric) string {
	var parts []string
	for _, metric := range metrics {
		if metric.Ratio == nil {
			continue
		}
		name := metric.Name
		if name == "" {
			name = metric.Type
		}
		parts = append(parts, fmt.Sprintf("%s %s", name, progressBar(*metric.Ratio)))
	}
	return strings.Join(parts, ",")
}

func progressBar(ratio float64) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 2 {
		ratio = 2
	}
	filled := int((ratio/2)*10 + 0.5)
	if filled > 10 {
		filled = 10
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
}

func compactBehavior(behavior []BehaviorRule) string {
	var parts []string
	for _, rule := range behavior {
		direction := strings.TrimPrefix(rule.Direction, "scale")
		if direction == "" {
			direction = rule.Direction
		}
		var value string
		if rule.StabilizationWindowSeconds != nil {
			value = fmt.Sprintf("%s:%ds", direction, *rule.StabilizationWindowSeconds)
		} else if len(rule.Policies) > 0 {
			value = fmt.Sprintf("%s:%s", direction, strings.Join(rule.Policies, ","))
		} else {
			value = direction + ":custom"
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, " ")
}
