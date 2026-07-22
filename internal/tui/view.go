package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// View renders the current state of the TUI. Returns a tea.View so the bubble
// tea v2 runtime can manage alt-screen and downsampling; the content itself
// is assembled by viewContent. AltScreen is pinned on the view because v2
// removed the program-level WithAltScreen option.
func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	return v
}

// viewContent assembles the screen content as a plain string. Split out from
// View so the multiple early-return paths stay simple string returns.
func (m Model) viewContent() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.loading && len(m.items) == 0 {
		return "Loading HPA data..."
	}

	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err)) + "\n\nPress q to quit."
	}

	content := m.renderViewByMode()
	statusBar := m.renderStatusBar()

	return content + "\n" + statusBar
}

// renderViewByMode dispatches to the renderer for the current view mode.
func (m Model) renderViewByMode() string {
	switch m.viewMode {
	case listView:
		return m.renderListView()
	case detailView:
		return m.renderDetailView()
	case helpView:
		return m.renderHelpView()
	case metricsView:
		return m.renderMetricsView()
	case simView:
		return m.renderSimView()
	case fixView:
		return m.renderFixView()
	case replayView:
		return m.renderReplayView()
	case batchAuditView:
		return m.renderBatchAuditView()
	case historyView:
		return m.renderHistoryView()
	case overviewView:
		return m.renderOverviewView()
	case hintsView:
		return m.renderHintsView()
	}
	return ""
}

func (m Model) renderHelpView() string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render(" TUI Help "))
	sb.WriteString("\n\n")

	writeHelpSection(&sb, "Daily triage", []helpBinding{
		{"↑/k", "Move cursor up"},
		{"↓/j", "Move cursor down"},
		{"Enter", "Open HPA detail"},
		{"/", "Filter by name, namespace, health, issue, or summary"},
		{"S", "Cycle sort: name, health-score, issue, namespace"},
		{"g", "Jump to first HPA whose health is not OK"},
		{"O", "Open cluster overview from the list"},
	})

	writeHelpSection(&sb, "Refresh and display", []helpBinding{
		{"r", "Refresh data now"},
		{"p", "Pause or resume auto-refresh"},
		{"+/=", "Decrease refresh interval, minimum 1s"},
		{"-", "Increase refresh interval, maximum 60s"},
		{"Esc", "Go back to the previous dashboard level"},
		{"?", "Toggle this help"},
		{"q/Ctrl+c", "Quit"},
	})

	writeHelpSection(&sb, "Detail drill-down", []helpBinding{
		{"m", "Open per-metric diagnostics"},
		{"s", "Open the what-if simulation panel"},
		{"M", "Toggle parameter and metric-value simulation inside simulation"},
		{"Tab", "Move to the next simulation field"},
		{"Shift+Tab", "Move to the previous simulation field"},
		{"f", "Open the fix wizard when suggestions are available"},
		{"d", "Validate the selected fix with Kubernetes server-side dry-run"},
		{"T", "Open replay timeline from hpa-trace.json"},
		{"H", "Open history and sparkline view"},
		{"h", "Open metric hints troubleshooting when hints are available"},
	})

	writeHelpSection(&sb, "Selection and batch work", []helpBinding{
		{"space", "Toggle current HPA selection"},
		{"a", "Select all visible HPAs"},
		{"A", "Clear the selection"},
		{"B", "Run the batch auditor on selected HPAs"},
		{"x", "Preview and explicitly confirm batch apply for selected HPAs"},
	})

	sb.WriteString(headerStyle.Render(" Export workflow "))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("  The TUI keeps export operations explicit. Leave the dashboard and run:"))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("  kubectl hpa status <name> -n <namespace> --suggest --export yaml"))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("  Other formats: --export kustomize or --export helm-values."))
	sb.WriteString("\n")
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Press ? or Esc to close"))
	return sb.String()
}

type helpBinding struct {
	key  string
	desc string
}

func writeHelpSection(sb *strings.Builder, title string, bindings []helpBinding) {
	sb.WriteString(headerStyle.Render(" " + title + " "))
	sb.WriteString("\n")
	for _, b := range bindings {
		sb.WriteString(fmt.Sprintf("  %-14s %s\n", b.key, dimStyle.Render(b.desc)))
	}
	sb.WriteString("\n")
}

// tuiTriggerStatusBadge returns a styled status string for a KEDA trigger.
func tuiTriggerStatusBadge(status string) string {
	switch status {
	case "Active":
		return okStyle.Render("Active ✓")
	case "Inactive":
		return errorStyle.Render("Inactive ✗")
	default:
		return dimStyle.Render("Unknown ?")
	}
}
