package hpa

import (
	"fmt"
	"sort"
	"strings"
)

// writeIncidentAffectedWorkloads renders the affected workloads section per HPA.
func writeIncidentAffectedWorkloads(buf *strings.Builder, reports []StatusReport) {
	buf.WriteString("## Affected Workloads\n\n")
	for _, r := range reports {
		a := r.Analysis
		buf.WriteString(fmt.Sprintf("### %s/%s\n\n", a.Meta.Namespace, a.Meta.Name))
		buf.WriteString(fmt.Sprintf("- **Target:** %s\n", a.Meta.Target))
		buf.WriteString(fmt.Sprintf("- **Replicas:** current=%d desired=%d min=%d max=%d\n",
			a.Replicas.Current, a.Replicas.Desired, a.Replicas.Min, a.Replicas.Max))
		if a.Replicas.TargetReplicas != nil {
			tr := a.Replicas.TargetReplicas
			buf.WriteString(fmt.Sprintf("- **Scale Target Replicas:** total=%d ready=%d notReady=%d",
				tr.TotalReplicas, tr.ReadyReplicas, tr.NotReady))
			if tr.Pending > 0 {
				buf.WriteString(fmt.Sprintf(" pending=%d", tr.Pending))
			}
			if tr.Unschedulable > 0 {
				buf.WriteString(fmt.Sprintf(" unschedulable=%d", tr.Unschedulable))
			}
			buf.WriteString("\n")
		}

		// Metrics context
		if len(a.Metrics.Metrics) > 0 {
			buf.WriteString("- **Metrics:**\n")
			for _, m := range a.Metrics.Metrics {
				name := m.Name
				if name == "" {
					name = m.Type
				}
				buf.WriteString(fmt.Sprintf("  - %s: %s / %s", name, m.Current, m.Target))
				if m.Ratio != nil {
					buf.WriteString(fmt.Sprintf(" (ratio: %.3f)", *m.Ratio))
				}
				buf.WriteString("\n")
			}
		}
		buf.WriteString("\n")
	}
}

// writeIncidentRemediationSteps renders the remediation steps ordered by health severity.
func writeIncidentRemediationSteps(buf *strings.Builder, reports []StatusReport) {
	buf.WriteString("## Remediation Steps\n\n")
	// Collect all remediation items ordered by health severity.
	orderedReports := make([]StatusReport, len(reports))
	copy(orderedReports, reports)
	sort.Slice(orderedReports, func(i, j int) bool {
		return healthPriority(orderedReports[i].Analysis.Decision.Health) < healthPriority(orderedReports[j].Analysis.Decision.Health)
	})

	stepNum := 1
	for _, r := range orderedReports {
		a := r.Analysis

		// Suggestions first
		for _, s := range a.Actions.Suggestions {
			buf.WriteString(fmt.Sprintf("%d. **[%s/%s] %s**: %s\n",
				stepNum, a.Meta.Namespace, a.Meta.Name, s.Title, s.Description))
			if s.Command != "" {
				buf.WriteString(fmt.Sprintf("   ```\n   %s\n   ```\n", s.Command))
			}
			if s.Risk != "" {
				buf.WriteString(fmt.Sprintf("   - Risk: %s\n", s.Risk))
			}
			if s.Patch != "" {
				buf.WriteString(fmt.Sprintf("   - Patch: `%s`\n", s.Patch))
			}
			stepNum++
		}

		// Actions as fallback
		if len(a.Actions.Suggestions) == 0 && len(a.Actions.Actions) > 0 {
			for _, action := range a.Actions.Actions {
				buf.WriteString(fmt.Sprintf("%d. **[%s/%s]** %s\n",
					stepNum, a.Meta.Namespace, a.Meta.Name, action))
				stepNum++
			}
		}
	}

	if stepNum == 1 {
		buf.WriteString("No specific remediation steps identified.\n")
	}
	buf.WriteString("\n")
}

// writeIncidentCapacityContext renders the capacity context section when present.
func writeIncidentCapacityContext(buf *strings.Builder, reports []StatusReport) {
	if !anyReportHasCapacity(reports) {
		return
	}
	buf.WriteString("## Capacity Context\n\n")
	for _, r := range reports {
		writeIncidentReportCapacity(buf, r.Analysis)
	}
}

// anyReportHasCapacity reports whether any report carries pending pods, quota constraints, node hints, or capacity headroom.
func anyReportHasCapacity(reports []StatusReport) bool {
	for _, r := range reports {
		if r.Analysis.Capacity.CapacityContext != nil {
			cc := r.Analysis.Capacity.CapacityContext
			if len(cc.PendingPods) > 0 || len(cc.QuotaConstraints) > 0 || len(cc.NodeHints) > 0 {
				return true
			}
		}
		if r.Analysis.Capacity.CapacityHeadroom != nil {
			return true
		}
	}
	return false
}

// writeIncidentReportCapacity renders the capacity context and headroom subsections for a single report.
func writeIncidentReportCapacity(buf *strings.Builder, a Analysis) {
	wroteHeader := false

	if a.Capacity.CapacityContext != nil {
		cc := a.Capacity.CapacityContext
		if len(cc.PendingPods) > 0 || len(cc.QuotaConstraints) > 0 || len(cc.NodeHints) > 0 {
			buf.WriteString(fmt.Sprintf("### %s/%s\n\n", a.Meta.Namespace, a.Meta.Name))
			wroteHeader = true

			writeIncidentPendingPods(buf, cc.PendingPods)
			writeIncidentQuotaConstraints(buf, cc.QuotaConstraints)
			writeIncidentNodeHints(buf, cc.NodeHints)
		}
	}

	if a.Capacity.CapacityHeadroom != nil {
		writeIncidentCapacityHeadroom(buf, a, wroteHeader)
	}
}

func writeIncidentPendingPods(buf *strings.Builder, pods []PendingPodInfo) {
	if len(pods) == 0 {
		return
	}
	buf.WriteString("**Pending Pods:**\n\n")
	for _, p := range pods {
		status := "scheduled"
		if p.Unschedulable {
			status = "unschedulable"
		}
		buf.WriteString(fmt.Sprintf("- %s (%s): %s\n",
			p.Name, status, escapeMarkdown(strings.Join(p.Reasons, "; "))))
	}
	buf.WriteString("\n")
}

func writeIncidentQuotaConstraints(buf *strings.Builder, quotas []QuotaConstraint) {
	if len(quotas) == 0 {
		return
	}
	buf.WriteString("**Resource Quotas:**\n\n")
	for _, q := range quotas {
		buf.WriteString(fmt.Sprintf("- %s/%s: used=%s hard=%s — %s\n",
			q.Name, q.Resource, q.Used, q.Hard, q.Message))
	}
	buf.WriteString("\n")
}

func writeIncidentNodeHints(buf *strings.Builder, hints []string) {
	if len(hints) == 0 {
		return
	}
	buf.WriteString("**Hints:**\n\n")
	for _, hint := range hints {
		buf.WriteString(fmt.Sprintf("- %s\n", hint))
	}
	buf.WriteString("\n")
}

func writeIncidentCapacityHeadroom(buf *strings.Builder, a Analysis, wroteHeader bool) {
	if !wroteHeader {
		buf.WriteString(fmt.Sprintf("### %s/%s\n\n", a.Meta.Namespace, a.Meta.Name))
	}
	ch := a.Capacity.CapacityHeadroom
	buf.WriteString(fmt.Sprintf("**Capacity Headroom:** %s (risk: %s)\n",
		ch.ClusterSchedulableHeadroom, ch.Risk))
	if len(ch.Evidence) > 0 {
		for _, e := range ch.Evidence {
			buf.WriteString(fmt.Sprintf("- %s\n", e))
		}
	}
	buf.WriteString("\n")
}
