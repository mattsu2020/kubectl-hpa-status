package tui

import (
	"fmt"
	"sort"
	"strings"
)

// overviewIssue represents a common issue aggregated across HPAs.
type overviewIssue struct {
	Count   int
	Issue   string
	Example string // "namespace/name"
}

// overviewChurnEntry represents an HPA with high churn.
type overviewChurnEntry struct {
	Namespace  string
	Name       string
	ChurnScore int
	ChurnLevel string
}

// computeOverview aggregates data from items and reports into a cluster summary.
func (m Model) computeOverview() (map[string]int, float64, []overviewIssue, []overviewChurnEntry, []int) {
	healthDist := make(map[string]int)
	totalScore := 0
	issueMap := make(map[string]*overviewIssue)
	var churnEntries []overviewChurnEntry
	histogram := make([]int, 10) // 10 buckets: 0-9, 10-19, ..., 90-100

	for _, item := range m.items {
		h := item.Health
		if h == "" {
			h = "OK"
		}
		healthDist[h]++
		totalScore += item.HealthScore

		bucket := item.HealthScore / 10
		if bucket >= 10 {
			bucket = 9
		}
		if bucket < 0 {
			bucket = 0
		}
		histogram[bucket]++

		if item.Issue != "" {
			key := item.Issue
			if existing, ok := issueMap[key]; ok {
				existing.Count++
			} else {
				issueMap[key] = &overviewIssue{
					Count:   1,
					Issue:   item.Issue,
					Example: item.Namespace + "/" + item.Name,
				}
			}
		}

		if item.ChurnLevel == "HIGH" || item.ChurnLevel == "CRITICAL" || item.ChurnLevel == "MEDIUM" {
			churnEntries = append(churnEntries, overviewChurnEntry{
				Namespace:  item.Namespace,
				Name:       item.Name,
				ChurnScore: item.ChurnScore,
				ChurnLevel: item.ChurnLevel,
			})
		}
	}

	var issues []overviewIssue
	for _, v := range issueMap {
		issues = append(issues, *v)
	}
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Count > issues[j].Count
	})

	sort.Slice(churnEntries, func(i, j int) bool {
		return churnEntries[i].ChurnScore > churnEntries[j].ChurnScore
	})

	var avgScore float64
	if len(m.items) > 0 {
		avgScore = float64(totalScore) / float64(len(m.items))
	}

	return healthDist, avgScore, issues, churnEntries, histogram
}

// renderOverviewView renders the cluster-wide health overview.
func (m Model) renderOverviewView() string {
	healthDist, avgScore, topIssues, churnHotspots, histogram := m.computeOverview()

	var sb strings.Builder

	// 1. Header.
	sb.WriteString(headerStyle.Render(fmt.Sprintf("Cluster HPA Overview -- %d HPAs", len(m.items))))
	sb.WriteString("\n\n")

	// 2. Health Distribution.
	sb.WriteString("Health Distribution:\n")
	healthOrder := []string{"OK", "STABILIZED", "LIMITED", "ERROR"}
	for _, state := range healthOrder {
		count := healthDist[state]
		if count == 0 {
			continue
		}
		bar := strings.Repeat("█", count)
		var coloredBar string
		switch state {
		case "OK":
			coloredBar = okStyle.Render(bar)
		case "STABILIZED":
			coloredBar = warnStyle.Render(bar)
		case "LIMITED":
			coloredBar = warnStyle.Render(bar)
		case "ERROR":
			coloredBar = errorStyle.Render(bar)
		default:
			coloredBar = dimStyle.Render(bar)
		}
		sb.WriteString(fmt.Sprintf("  %-12s %s %d\n", state, coloredBar, count))
	}
	sb.WriteString(fmt.Sprintf("  Average Score: %.0f/100\n", avgScore))
	sb.WriteString("\n")

	// 3. Score Histogram sparkline.
	sb.WriteString("Score Distribution:\n  ")
	histValues := make([]float64, len(histogram))
	for i, v := range histogram {
		histValues[i] = float64(v)
	}
	sb.WriteString(renderSparkline(histValues, 10, dimStyle))
	sb.WriteString("\n  ")
	sb.WriteString(dimStyle.Render("0   1   2   3   4   5   6   7   8   9  (x10)"))
	sb.WriteString("\n\n")

	// 4. Top Issues.
	if len(topIssues) > 0 {
		sb.WriteString(headerStyle.Render("Top Issues:"))
		sb.WriteString("\n")
		maxIssues := 5
		for i, issue := range topIssues {
			if i >= maxIssues {
				sb.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more\n", len(topIssues)-maxIssues)))
				break
			}
			issueLabel := truncate(issue.Issue, 16)
			sb.WriteString(fmt.Sprintf("  %s x%d  %s\n",
				warnStyle.Render(fmt.Sprintf("%-16s", issueLabel)),
				issue.Count,
				dimStyle.Render(issue.Example),
			))
		}
		sb.WriteString("\n")
	}

	// 5. Churn Hotspots.
	if len(churnHotspots) > 0 {
		sb.WriteString(headerStyle.Render("Churn Hotspots:"))
		sb.WriteString("\n")
		maxChurn := 5
		for i, entry := range churnHotspots {
			if i >= maxChurn {
				sb.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more\n", len(churnHotspots)-maxChurn)))
				break
			}
			var churnLabel string
			switch entry.ChurnLevel {
			case "CRITICAL", "HIGH":
				churnLabel = errorStyle.Render(entry.ChurnLevel)
			default:
				churnLabel = warnStyle.Render(entry.ChurnLevel)
			}
			sb.WriteString(fmt.Sprintf("  %s/%s  %s %d/100\n",
				entry.Namespace, entry.Name,
				churnLabel,
				entry.ChurnScore,
			))
		}
		sb.WriteString("\n")
	}

	// 6. Footer.
	sb.WriteString(dimStyle.Render("Press O to toggle | esc: back to list"))

	return sb.String()
}
