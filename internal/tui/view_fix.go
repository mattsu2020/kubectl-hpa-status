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
	appendFixSuggestionsList(&sb, m.fixState)

	// Preview of selected fix.
	appendFixPreview(&sb, m.fixState)

	// Apply status.
	appendFixApplyStatus(&sb, m.fixState)

	// Footer.
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Enter=apply  d=dry-run  ↑↓=select  Esc=cancel"))

	return sb.String()
}

func appendFixSuggestionsList(sb *strings.Builder, st *fixState) {
	sb.WriteString("Available Fixes:\n")
	for i, suggestion := range st.suggestions {
		marker := "  "
		if i == st.selected {
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
		if i == st.selected {
			sb.WriteString(fmt.Sprintf("      %s\n", dimStyle.Render(suggestion.Description)))
		}
	}
}

func appendFixPreview(sb *strings.Builder, st *fixState) {
	selected := st.suggestions[st.selected]
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render(fmt.Sprintf("Preview of Fix [%d]:", st.selected+1)))
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
}

func appendFixApplyStatus(sb *strings.Builder, st *fixState) {
	if st.applied {
		sb.WriteString("\n")
		if st.applyErr != nil {
			sb.WriteString(errorStyle.Render(fmt.Sprintf("  Apply failed: %v", st.applyErr)))
		} else {
			sb.WriteString(okStyle.Render("  ✓ Applied successfully"))
		}
		sb.WriteString("\n")
	}
	if st.dryRunResult != "" {
		sb.WriteString(fmt.Sprintf("\n  Dry-run: %s\n", okStyle.Render(st.dryRunResult)))
	}
}
