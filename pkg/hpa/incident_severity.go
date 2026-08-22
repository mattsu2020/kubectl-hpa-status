package hpa

import (
	"fmt"
	"strings"
)

// writeIncidentRecommendations renders the recommendations section.
func writeIncidentRecommendations(buf *strings.Builder, reports []StatusReport) {
	buf.WriteString("## Recommendations\n\n")
	recNum := 1
	hasRecs := false
	for _, r := range reports {
		a := r.Analysis

		// Churn recommendations
		if a.Stability.ChurnAnalysis != nil && len(a.Stability.ChurnAnalysis.Recommendations) > 0 {
			for _, rec := range a.Stability.ChurnAnalysis.Recommendations {
				buf.WriteString(fmt.Sprintf("%d. **%s**: Change %s to %s — %s\n",
					recNum, rec.Type, rec.CurrentValue, rec.RecommendedValue, rec.Rationale))
				recNum++
				hasRecs = true
			}
		}

		// Behavior tuning
		if a.Advisory.BehaviorAdvisor != nil && len(a.Advisory.BehaviorAdvisor.Findings) > 0 {
			for _, f := range a.Advisory.BehaviorAdvisor.Findings {
				buf.WriteString(fmt.Sprintf("%d. **%s**: %s\n", recNum, f.Category, f.Message))
				recNum++
				hasRecs = true
			}
		}

		// Interpretation as long-term advice
		if len(a.Actions.Interpretation) > 0 {
			for _, line := range a.Actions.Interpretation {
				buf.WriteString(fmt.Sprintf("%d. %s\n", recNum, line))
				recNum++
				hasRecs = true
			}
		}
	}
	if !hasRecs {
		buf.WriteString("No additional recommendations at this time.\n")
	}
	buf.WriteString("\n")
}

// writeIncidentEscalationNotes renders the escalation notes section.
func writeIncidentEscalationNotes(buf *strings.Builder, reports []StatusReport) {
	buf.WriteString("## Escalation Notes\n\n")
	escalationWritten := false
	for _, r := range reports {
		a := r.Analysis
		switch a.Decision.Health {
		case string(HealthError):
			buf.WriteString(fmt.Sprintf("- **%s/%s**: Escalate to **platform team** — metrics pipeline or HPA controller may be unavailable.\n",
				a.Meta.Namespace, a.Meta.Name))
			escalationWritten = true
		case string(HealthLimited):
			buf.WriteString(fmt.Sprintf("- **%s/%s**: Escalate to **application team** — HPA is capped; review scaling limits and workload configuration.\n",
				a.Meta.Namespace, a.Meta.Name))
			escalationWritten = true
		}
	}
	if !escalationWritten {
		buf.WriteString("No immediate escalation required. All HPAs are healthy.\n")
	}
	buf.WriteString("\n")
}

// overallSeverity computes the highest severity across all reports.
func overallSeverity(reports []StatusReport) string {
	if len(reports) == 0 {
		return "NONE"
	}
	worst := "LOW"
	for _, r := range reports {
		s := healthSeverity(r.Analysis.Decision.Health, r.Analysis.Decision.HealthScore)
		if severityHigher(s, worst) {
			worst = s
		}
	}
	return worst
}

// healthSeverity maps health state and score to a severity label.
func healthSeverity(health string, score int) string {
	switch {
	case health == string(HealthError) && score <= 30:
		return "CRITICAL"
	case health == string(HealthError):
		return "HIGH"
	case health == string(HealthLimited):
		return "MEDIUM"
	default:
		return "LOW"
	}
}

// severityHigher returns true if a is more severe than b.
func severityHigher(a, b string) bool {
	order := map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1, "NONE": 0}
	return order[a] > order[b]
}

// severityPrefix prefixes a visual marker for the health state.
func severityPrefix(health string) string {
	switch health {
	case string(HealthError):
		return "[ALERT] " + health
	case string(HealthLimited):
		return "[WARN] " + health
	case string(HealthStabilized):
		return "[INFO] " + health
	default:
		return "[OK] " + health
	}
}

// healthPriority returns a numeric priority for ordering (lower = more urgent).
func healthPriority(health string) int {
	switch health {
	case string(HealthError):
		return 0
	case string(HealthLimited):
		return 1
	case string(HealthStabilized):
		return 2
	default:
		return 3
	}
}

// conditionImplication returns a human-readable explanation for a condition.
func conditionImplication(c Condition) string {
	switch {
	case c.Type == ConditionScalingActive && c.Status != "True":
		return "Metrics pipeline is not providing data; HPA cannot make scaling decisions."
	case c.Type == ConditionAbleToScale && c.Status != "True":
		return "HPA controller cannot act on scaling decisions (check RBAC or scaleTargetRef)."
	case c.Type == ConditionScalingLimited && c.Status == "True":
		return "HPA is capped by minReplicas or maxReplicas."
	default:
		if c.Message != "" {
			return c.Message
		}
		return "No specific implication."
	}
}

// incidentReportNames builds a comma-separated list of HPA names for the title.
func incidentReportNames(reports []StatusReport) string {
	if len(reports) == 0 {
		return "no-hpas"
	}
	if len(reports) == 1 {
		a := reports[0].Analysis
		return fmt.Sprintf("%s/%s", a.Meta.Namespace, a.Meta.Name)
	}
	names := make([]string, len(reports))
	for i, r := range reports {
		names[i] = fmt.Sprintf("%s/%s", r.Analysis.Meta.Namespace, r.Analysis.Meta.Name)
	}
	return strings.Join(names, ", ")
}
