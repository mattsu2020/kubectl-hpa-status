package hpa

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ListItem is a compact row representation for list output.
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
	// Stabilizing is true when StabilizationRemaining > 0.
	Stabilizing bool `json:"stabilizing,omitempty" yaml:"stabilizing,omitempty"`
	// StabilizationLabel is a human-readable countdown like "4m12s".
	StabilizationLabel string `json:"stabilizationLabel,omitempty" yaml:"stabilizationLabel,omitempty"`
	// ChurnLevel is the churn severity (LOW/MEDIUM/HIGH/CRITICAL) if churn was detected.
	ChurnLevel string `json:"churnLevel,omitempty" yaml:"churnLevel,omitempty"`
	// ChurnScore is the numeric churn score 0-100.
	ChurnScore int `json:"churnScore,omitempty" yaml:"churnScore,omitempty"`
	// TrendSparkline is a pre-formatted sparkline showing health score trend.
	TrendSparkline string `json:"trendSparkline,omitempty" yaml:"trendSparkline,omitempty"`
	// TrendFlapping indicates whether flapping was detected in health history.
	TrendFlapping bool `json:"trendFlapping,omitempty" yaml:"trendFlapping,omitempty"`
}

// ListReport holds the list of HPA items for table output.
type ListReport struct {
	// APIVersion identifies the JSON/YAML schema version (see SchemaVersion).
	APIVersion  string              `json:"apiVersion" yaml:"apiVersion"`
	Items       []ListItem          `json:"items" yaml:"items"`
	GitOpsDrift []GitOpsDriftSignal `json:"gitOpsDrift,omitempty" yaml:"gitOpsDrift,omitempty"`
}

// GitOpsDriftSignal describes an HPA that appears to be GitOps-managed and
// should be compared against the declared Git manifest.
type GitOpsDriftSignal struct {
	Namespace string   `json:"namespace" yaml:"namespace"`
	Name      string   `json:"name" yaml:"name"`
	Tool      string   `json:"tool" yaml:"tool"`
	Evidence  []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	Advice    string   `json:"advice" yaml:"advice"`
}

// ListTextOptions configures list output with wide, color, language, and theme.
type ListTextOptions struct {
	Wide   bool
	Color  bool
	Lang   string
	Labels LabelProvider // When nil, English defaults are used. Takes precedence over Lang.
	// Theme takes precedence over Color. When Theme is set, Color is ignored.
	Theme style.Theme
	// SummaryTranslator, when non-nil, localises the per-row Summary line
	// (populated from Analysis.Summary via NewListItem). pkg/hpa cannot import
	// internal/i18n, so the cmd layer injects i18n.Get here, mirroring
	// StatusTextOptions.SummaryTranslator. When nil, Summary is rendered
	// verbatim (English).
	SummaryTranslator func(string) string
}

// translateSummary applies opts.SummaryTranslator when configured, returning
// the original string otherwise.
func (o ListTextOptions) translateSummary(s string) string {
	if o.SummaryTranslator != nil {
		return o.SummaryTranslator(s)
	}
	return s
}

func (o ListTextOptions) theme() style.Theme {
	if o.Theme.Enabled() || !o.Color {
		return o.Theme
	}
	return style.NewTheme(true)
}

// NewListItem converts an Analysis into a compact ListItem for list output.
func NewListItem(src Analysis) ListItem {
	errors, limiteds := classifyListConditions(src.Conditions)
	if src.Current == src.Desired && src.Desired == src.Max {
		limiteds = append(limiteds, "LIMITED: maxReplicas")
	}

	health := deriveListHealth(src.Health, errors, limiteds)
	issue := joinListIssues(errors, limiteds)
	conditions := compactConditions(src.Conditions)
	metrics := compactMetrics(src.Metrics)
	behavior := compactBehavior(src.Behavior)
	if src.HealthScore == 0 {
		_, src.HealthScore = healthFromAnalysis(src)
	}

	return ListItem{
		Namespace:          src.Namespace,
		Name:               src.Name,
		Target:             src.Target,
		Current:            src.Current,
		Desired:            src.Desired,
		Min:                src.Min,
		Max:                src.Max,
		Summary:            src.Summary,
		Health:             health,
		HealthScore:        src.HealthScore,
		Issue:              issue,
		Metrics:            metrics,
		Behavior:           behavior,
		Conditions:         conditions,
		CreationTimestamp:  src.CreationTimestamp,
		Stabilizing:        src.StabilizationRemaining != nil && *src.StabilizationRemaining > 0,
		StabilizationLabel: FormatCountdownBadge(src.StabilizationRemaining),
		ChurnLevel:         churnLevelFromAnalysis(src.ChurnAnalysis),
		ChurnScore:         churnScoreFromAnalysis(src.ChurnAnalysis),
		TrendSparkline:     trendSparklineFromAnalysis(src.HealthTrend),
		TrendFlapping:      trendFlappingFromAnalysis(src.HealthTrend),
	}
}

// classifyListConditions separates conditions into error and limited buckets
// for list display.
func classifyListConditions(conditions []Condition) (errors, limiteds []string) {
	for _, condition := range conditions {
		switch {
		case condition.Type == ConditionScalingActive && condition.Status != "True":
			errors = append(errors, "ERROR: "+condition.Reason)
		case condition.Type == ConditionAbleToScale && condition.Status != "True":
			errors = append(errors, "ERROR: "+condition.Reason)
		case condition.Type == ConditionScalingLimited && condition.Status == "True":
			limiteds = append(limiteds, "LIMITED: "+condition.Reason)
		}
	}
	return errors, limiteds
}

// deriveListHealth returns the health label, defaulting to "OK" when empty and
// overriding with "ERROR"/"LIMITED" based on classified buckets.
func deriveListHealth(base string, errors, limiteds []string) string {
	if base == "" {
		base = string(HealthOK)
	}
	if len(errors) > 0 {
		return string(HealthError)
	}
	if len(limiteds) > 0 {
		return string(HealthLimited)
	}
	return base
}

// joinListIssues joins the error and limited buckets into a single comma-separated string.
func joinListIssues(errors, limiteds []string) string {
	var issues []string
	issues = append(issues, errors...)
	issues = append(issues, limiteds...)
	return strings.Join(issues, ", ")
}

// compactConditions builds a compact "Type=Status;..." string for wide output.
func compactConditions(conditions []Condition) string {
	var condParts []string
	for _, c := range conditions {
		condParts = append(condParts, fmt.Sprintf("%s=%s", c.Type, c.Status))
	}
	return strings.Join(condParts, ";")
}

func trendSparklineFromAnalysis(trend *HealthTrendResult) string {
	if trend == nil {
		return ""
	}
	return FormatTrendListRow(*trend)
}

func trendFlappingFromAnalysis(trend *HealthTrendResult) bool {
	return trend != nil && trend.FlappingDetected
}

func healthFromAnalysis(src Analysis) (string, int) {
	score := 100
	switch src.Health {
	case string(HealthError):
		score = 50
	case string(HealthLimited):
		score = 75
	case string(HealthStabilized):
		score = 90
	}
	return src.Health, score
}

func churnLevelFromAnalysis(ca *ChurnAnalysis) string {
	if ca == nil {
		return ""
	}
	return string(ca.Level)
}

func churnScoreFromAnalysis(ca *ChurnAnalysis) int {
	if ca == nil {
		return 0
	}
	return ca.Score
}

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// WriteListText writes a table-formatted list of HPA items.
func WriteListText(w io.Writer, report ListReport, opts ListTextOptions) error {
	t := opts.theme()
	var out []byte
	showTrend := listHasTrend(report.Items)
	if opts.Wide {
		header := fmt.Sprintf("%s %s %s %s %s %s %s %s %s %s %s %s %s %s",
			padRight("NAMESPACE", 20),
			padRight("NAME", 32),
			padRight("TARGET", 28),
			padRight("CURRENT", 8),
			padRight("DESIRED", 8),
			padRight("DIFF", 8),
			padRight("MIN", 8),
			padRight("MAX", 8),
			padRight("HEALTH", 12),
			padRight("SCORE", 8),
			padRight("METRICS", 20),
			padRight("BEHAVIOR", 28),
			padRight("ISSUE", 32),
			padRight("CONDITIONS", 36))
		if showTrend {
			header = fmt.Sprintf("%s %s", header, padRight("TREND", 20))
		}
		out = fmt.Appendf(out, "%s %s\n", header, "SUMMARY")
		for _, item := range report.Items {
			row := fmt.Sprintf("%s %s %s %s %s %s %s %s %s %s %s %s %s %s",
				padRight(item.Namespace, 20),
				padRight(item.Name, 32),
				padRight(item.Target, 28),
				padRight(fmt.Sprintf("%d", item.Current), 8),
				padRight(fmt.Sprintf("%d", item.Desired), 8),
				padRight(formatReplicaDiff(item.Desired-item.Current), 8),
				padRight(fmt.Sprintf("%d", item.Min), 8),
				padRight(fmt.Sprintf("%d", item.Max), 8),
				padRight(t.HealthLabel(item.Health, item.HealthScore), 12),
				padRight(fmt.Sprintf("%d", item.HealthScore), 8),
				padRight(item.Metrics, 20),
				padRight(item.Behavior, 28),
				padRight(t.Issue(item.Issue, item.Health), 32),
				padRight(item.Conditions, 36))
			if showTrend {
				row = fmt.Sprintf("%s %s", row, padRight(item.TrendSparkline, 20))
			}
			out = fmt.Appendf(out, "%s %s\n", row, opts.translateSummary(item.Summary))
		}
		_, err := w.Write(out)
		return err
	}

	header := fmt.Sprintf("%s %s %s %s %s %s %s",
		padRight("NAMESPACE", 20),
		padRight("NAME", 32),
		padRight("CURRENT", 8),
		padRight("DESIRED", 8),
		padRight("HEALTH", 12),
		padRight("SCORE", 8),
		padRight("ISSUE", 32))
	if showTrend {
		header = fmt.Sprintf("%s %s", header, padRight("TREND", 20))
	}
	out = fmt.Appendf(out, "%s %s\n", header, "SUMMARY")
	for _, item := range report.Items {
		row := fmt.Sprintf("%s %s %s %s %s %s %s",
			padRight(item.Namespace, 20),
			padRight(item.Name, 32),
			padRight(fmt.Sprintf("%d", item.Current), 8),
			padRight(fmt.Sprintf("%d", item.Desired), 8),
			padRight(t.HealthLabel(item.Health, item.HealthScore), 12),
			padRight(fmt.Sprintf("%d", item.HealthScore), 8),
			padRight(t.Issue(item.Issue, item.Health), 32))
		if showTrend {
			row = fmt.Sprintf("%s %s", row, padRight(item.TrendSparkline, 20))
		}
		out = fmt.Appendf(out, "%s %s\n", row, opts.translateSummary(item.Summary))
	}
	_, err := w.Write(out)
	return err
}

func listHasTrend(items []ListItem) bool {
	for _, item := range items {
		if item.TrendSparkline != "" || item.TrendFlapping {
			return true
		}
	}
	return false
}

func formatReplicaDiff(diff int32) string {
	if diff > 0 {
		return fmt.Sprintf("+%d", diff)
	}
	return fmt.Sprintf("%d", diff)
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

func compactBehavior(behavior []BehaviorRule) string {
	var parts []string
	for _, rule := range behavior {
		direction := strings.TrimPrefix(rule.Direction, "scale")
		if direction == "" {
			direction = rule.Direction
		}
		var value string
		switch {
		case rule.StabilizationWindowSeconds != nil:
			value = fmt.Sprintf("%s:%ds", direction, *rule.StabilizationWindowSeconds)
		case len(rule.Policies) > 0:
			value = fmt.Sprintf("%s:%s", direction, strings.Join(rule.Policies, ","))
		default:
			value = direction + ":custom"
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, " ")
}
