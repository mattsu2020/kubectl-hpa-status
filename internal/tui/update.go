package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Update handles all bubbletea messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// If filter input is active, handle filter input keys.
		if m.filtering {
			return m.handleFilterInput(msg)
		}
		return m.handleKey(msg)

	case tickMsg:
		if m.paused {
			return m, tickCmd(m.interval)
		}
		return m, tea.Batch(fetchHPAs(m), tickCmd(m.interval))

	case fetchResultMsg:
		m.loading = false
		m.lastRefresh = time.Now()
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.items = msg.items
		m.reports = msg.reports
		m.err = nil
		if m.sortField != "" {
			m.sortItems()
		}
		if !m.initialFocused {
			m.focusInitialItem()
			m.initialFocused = true
		}
		// Clamp cursor.
		filtered := m.filteredItems()
		if m.cursor >= len(filtered) {
			m.cursor = len(filtered) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Up):
		if m.viewMode == listView {
			m.cursor--
			if m.cursor < 0 {
				m.cursor = 0
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.viewMode == listView {
			filtered := m.filteredItems()
			m.cursor++
			if m.cursor >= len(filtered) {
				m.cursor = len(filtered) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if m.viewMode == listView {
			filtered := m.filteredItems()
			if m.cursor >= 0 && m.cursor < len(filtered) {
				m.viewMode = detailView
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Escape):
		switch m.viewMode {
		case helpView, detailView, metricsView:
			m.viewMode = listView
			return m, nil
		}
		return m, nil

	case key.Matches(msg, m.keys.Refresh):
		m.loading = true
		return m, fetchHPAs(m)

	case key.Matches(msg, m.keys.Pause):
		m.paused = !m.paused
		return m, nil

	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		m.filterInput.Focus()
		return m, nil

	case key.Matches(msg, m.keys.Help):
		if m.viewMode == helpView {
			m.viewMode = listView
		} else {
			m.viewMode = helpView
		}
		return m, nil

	case key.Matches(msg, m.keys.Sort):
		sortCycle := []string{"name", "health-score", "issue", "namespace"}
		found := false
		for i, f := range sortCycle {
			if m.sortField == f {
				m.sortField = sortCycle[(i+1)%len(sortCycle)]
				found = true
				break
			}
		}
		if !found {
			m.sortField = "health-score"
		}
		m.sortDescending = !m.sortDescending
		m.sortItems()
		m.cursor = 0
		return m, nil

	case key.Matches(msg, m.keys.JumpProblem):
		filtered := m.filteredItems()
		for i, item := range filtered {
			if item.Health != "OK" {
				m.cursor = i
				break
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Metrics):
		if m.viewMode == detailView || m.viewMode == listView {
			m.viewMode = metricsView
		}
		return m, nil

	case key.Matches(msg, m.keys.ToggleSelect):
		if m.viewMode == listView {
			filtered := m.filteredItems()
			if m.cursor >= 0 && m.cursor < len(filtered) {
				key := filtered[m.cursor].Namespace + "/" + filtered[m.cursor].Name
				m.selected[key] = !m.selected[key]
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.SelectAll):
		if m.viewMode == listView {
			for _, item := range m.filteredItems() {
				m.selected[item.Namespace+"/"+item.Name] = true
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.DeselectAll):
		if m.viewMode == listView {
			m.selected = map[string]bool{}
		}
		return m, nil

	case key.Matches(msg, m.keys.ApplySelected):
		if m.viewMode == listView && len(m.selected) > 0 {
			filtered := m.filteredItems()
			selectedNames := make([]string, 0, len(m.selected))
			for _, item := range filtered {
				key := item.Namespace + "/" + item.Name
				if m.selected[key] {
					selectedNames = append(selectedNames, item.Name)
				}
			}
			m.selected = map[string]bool{}
			m.err = fmt.Errorf("apply for %d HPA(s): use CLI instead (kubectl-hpa-status list --problem --fix --apply). Selected: %s", len(selectedNames), strings.Join(selectedNames, ", "))
			return m, nil
		}
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
