package tui

import (
	"fmt"
	"strings"
)

// renderFixView renders the fix wizard for a problematic HPA.
func (m Model) renderFixView() string {
	if m.fixState == nil {
		return dimStyle.Render("No fix data. Press f from detail view on a problematic HPA.")
	}

	var sb strings.Builder

	// Header with health context.
	filtered := m.filteredItems()
	if m.cursor >= 0 && m.cursor < len(filtered) {
		item := filtered[m.cursor]
		healthLabel := healthStyle(item.Health).Render(item.Health)
		sb.WriteString(headerStyle.Render(fmt.Sprintf("Fix Wizard: %s/%s", item.Namespace, item.Name)))
		sb.WriteString(fmt.Sprintf("  Health: %s %d/100", healthLabel, item.HealthScore))
	}
	sb.WriteString("\n\n")

	if len(m.fixState.suggestions) == 0 {
		sb.WriteString(okStyle.Render("No suggestions available for this HPA."))
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render("Press Esc to go back."))
		return sb.String()
	}

	// Available fixes list.
	sb.WriteString("Available Fixes:\n")
	for i, suggestion := range m.fixState.suggestions {
		marker := "  "
		if i == m.fixState.selected {
			marker = cursorStyle.Render("▸ ")
		}
		riskStyle := warnStyle
		if suggestion.Risk == "low" {
			riskStyle = okStyle
		}
		sb.WriteString(fmt.Sprintf("%s [%d] %s %s\n",
			marker,
			i+1,
			suggestion.Title,
			riskStyle.Render(fmt.Sprintf("(risk: %s)", suggestion.Risk)),
		))
		if i == m.fixState.selected {
			sb.WriteString(fmt.Sprintf("      %s\n", dimStyle.Render(suggestion.Description)))
		}
	}

	// Preview of selected fix.
	selected := m.fixState.suggestions[m.fixState.selected]
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render(fmt.Sprintf("Preview of Fix [%d]:", m.fixState.selected+1)))
	sb.WriteString("\n")

	if selected.Patch != "" {
		sb.WriteString(fmt.Sprintf("  Patch: %s\n", selected.Patch))
	}
	if selected.Command != "" {
		sb.WriteString(fmt.Sprintf("  Command: %s\n", dimStyle.Render(selected.Command)))
	}

	// Preconditions and warnings.
	if len(selected.Preconditions) > 0 {
		sb.WriteString("\n  Preconditions:\n")
		for _, p := range selected.Preconditions {
			sb.WriteString(fmt.Sprintf("    • %s\n", dimStyle.Render(p)))
		}
	}
	if len(selected.Warnings) > 0 {
		sb.WriteString("  Warnings:\n")
		for _, w := range selected.Warnings {
			sb.WriteString(fmt.Sprintf("    ⚠ %s\n", warnStyle.Render(w)))
		}
	}

	// Apply status.
	if m.fixState.applied {
		sb.WriteString("\n")
		if m.fixState.applyErr != nil {
			sb.WriteString(errorStyle.Render(fmt.Sprintf("  Apply failed: %v", m.fixState.applyErr)))
		} else {
			sb.WriteString(okStyle.Render("  ✓ Applied successfully"))
		}
		sb.WriteString("\n")
	}
	if m.fixState.dryRunResult != "" {
		sb.WriteString(fmt.Sprintf("\n  Dry-run: %s\n", okStyle.Render(m.fixState.dryRunResult)))
	}

	// Footer.
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Enter=apply  d=dry-run  ↑↓=select  Esc=cancel"))

	return sb.String()
}
