package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

func (m Model) handleSimFieldInput(msg tea.KeyMsg) (tea.Model, bool) {
	if m.simState == nil || len(m.simState.fields) == 0 {
		return m, false
	}
	// bubble tea v2: match on the key's String() form for special keys and
	// inspect Key().Code/Text for printable input. Both backspace and ctrl+h
	// are destructive; everything else that carries printable text is filtered
	// to the numeric grammar the simulate fields accept.
	k := msg.Key()
	switch k.Code {
	case tea.KeyBackspace:
		field := &m.simState.fields[m.simState.focusIndex]
		if len(field.Value) > 0 {
			field.Value = field.Value[:len(field.Value)-1]
		}
		return m, true
	default:
		if len(k.Text) == 0 {
			return m, false
		}
		changed := false
		for _, r := range k.Text {
			if (r >= '0' && r <= '9') || r == '-' || r == '.' {
				field := &m.simState.fields[m.simState.focusIndex]
				field.Value += string(r)
				changed = true
			}
		}
		return m, changed
	}
}

// handleEnter processes the Enter key based on the current view mode.
func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.viewMode {
	case listView:
		filtered := m.filteredItems()
		if m.cursor >= 0 && m.cursor < len(filtered) {
			m.viewMode = detailView
		}
		return m, nil

	case simView:
		if m.simState == nil {
			return m, nil
		}
		return m, m.runSimulation()

	case fixView:
		if m.fixState == nil || len(m.fixState.suggestions) == 0 {
			return m, nil
		}
		if m.opts.ApplyFn == nil {
			m.fixState.applyConfirm = false
			m.fixState.applied = true
			m.fixState.applyErr = fmt.Errorf("live apply is disabled; restart with --apply --dry-run=false")
			return m, nil
		}
		if !m.fixState.applyConfirm {
			m.fixState.applyConfirm = true
			m.fixState.applied = false
			m.fixState.applyErr = nil
			return m, nil
		}
		m.fixState.applyConfirm = false
		return m, m.applyFix()
	}

	return m, nil
}

// handleEscape processes the Escape key based on the current view mode.
func (m Model) handleEscape() (tea.Model, tea.Cmd) {
	switch m.viewMode {
	case listView:
		m.batchApplyConfirm = false
		m.batchApplyPreview = nil
	case helpView, detailView, metricsView:
		m.viewMode = listView
		m.batchApplyConfirm = false
		m.batchApplyPreview = nil
	case simView, fixView, replayView:
		m.simState = nil
		m.fixState = nil
		m.replayState = nil
		m.viewMode = detailView
	case historyView:
		m.historyState = nil
		m.viewMode = detailView
	case hintsView:
		m.hintsState = nil
		m.viewMode = detailView
	case batchAuditView:
		m.batchAuditState = nil
		m.viewMode = listView
	case overviewView:
		m.viewMode = listView
	default:
		return m, nil
	}
	return m, nil
}

func (m Model) handleFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		m.filter = m.filterInput.Value()
		m.filterInput.Blur()
		m.cursor = 0
		return m, nil
	case "esc":
		m.filtering = false
		m.filterInput.Blur()
		return m, nil
	default:
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		return m, cmd
	}
}
