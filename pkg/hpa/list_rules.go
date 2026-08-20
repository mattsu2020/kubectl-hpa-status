package hpa

import (
	"fmt"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

// NewListItem converts an Analysis into a compact ListItem for list output.
func NewListItem(src Analysis) ListItem {
	errors, limiteds := classifyListConditions(src.Conditions())
	if src.Current() == src.Desired() && src.Desired() == src.Max() {
		limiteds = append(limiteds, "LIMITED: maxReplicas")
	}

	health := deriveListHealth(src.Health(), errors, limiteds)
	issue := joinListIssues(errors, limiteds)
	conditions := compactConditions(src.Conditions())
	metrics := compactMetrics(src.Metrics())
	behavior := compactBehavior(src.Behavior())

	return ListItem{
		Namespace:          src.Namespace(),
		Name:               src.Name(),
		Target:             src.Target(),
		Current:            src.Current(),
		Desired:            src.Desired(),
		Min:                src.Min(),
		Max:                src.Max(),
		Summary:            src.Summary(),
		SummaryKey:         src.SummaryKey(),
		Health:             health,
		HealthScore:        src.HealthScore(),
		Issue:              issue,
		Metrics:            metrics,
		Behavior:           behavior,
		Conditions:         conditions,
		CreationTimestamp:  src.CreationTimestamp(),
		Stabilizing:        src.StabilizationRemaining() != nil && *src.StabilizationRemaining() > 0,
		StabilizationLabel: FormatCountdownBadge(src.StabilizationRemaining()),
		ChurnLevel:         churnLevelFromAnalysis(src.ChurnAnalysis()),
		ChurnScore:         churnScoreFromAnalysis(src.ChurnAnalysis()),
		TrendSparkline:     trendSparklineFromAnalysis(src.HealthTrend()),
		TrendFlapping:      trendFlappingFromAnalysis(src.HealthTrend()),
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

func trendSparklineFromAnalysis(trend *healthtrend.Result) string {
	if trend == nil {
		return ""
	}
	return healthtrend.FormatTrendListRow(*trend)
}

func trendFlappingFromAnalysis(trend *healthtrend.Result) bool {
	return trend != nil && trend.FlappingDetected
}

func churnLevelFromAnalysis(ca *churn.ChurnAnalysis) string {
	if ca == nil {
		return ""
	}
	return string(ca.Level)
}

func churnScoreFromAnalysis(ca *churn.ChurnAnalysis) int {
	if ca == nil {
		return 0
	}
	return ca.Score
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
