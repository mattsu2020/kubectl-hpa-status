package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// keyHandlers is the key dispatch table: each entry pairs a binding selector
// with the handler invoked when the pressed key matches. The first matching
// entry wins, mirroring the switch this table replaced. Add a new key binding
// by appending a row here and defining a small handler method below.
var keyHandlers = []struct {
	binding func(keyMap) key.Binding
	handle  func(Model) (tea.Model, tea.Cmd)
}{
	{func(k keyMap) key.Binding { return k.Quit }, Model.handleQuitKey},
	{func(k keyMap) key.Binding { return k.Up }, Model.handleUpKey},
	{func(k keyMap) key.Binding { return k.Down }, Model.handleDownKey},
	{func(k keyMap) key.Binding { return k.Enter }, Model.handleEnter},
	{func(k keyMap) key.Binding { return k.Escape }, Model.handleEscape},
	{func(k keyMap) key.Binding { return k.Refresh }, Model.handleRefreshKey},
	{func(k keyMap) key.Binding { return k.Pause }, Model.handlePauseKey},
	{func(k keyMap) key.Binding { return k.Filter }, Model.handleFilterKey},
	{func(k keyMap) key.Binding { return k.Help }, Model.handleHelpKey},
	{func(k keyMap) key.Binding { return k.Sort }, Model.handleSortKey},
	{func(k keyMap) key.Binding { return k.JumpProblem }, Model.handleJumpProblemKey},
	{func(k keyMap) key.Binding { return k.Metrics }, Model.handleMetricsKey},
	{func(k keyMap) key.Binding { return k.ToggleSelect }, Model.handleToggleSelectKey},
	{func(k keyMap) key.Binding { return k.SelectAll }, Model.handleSelectAllKey},
	{func(k keyMap) key.Binding { return k.DeselectAll }, Model.handleDeselectAllKey},
	{func(k keyMap) key.Binding { return k.History }, Model.handleHistoryKey},
	{func(k keyMap) key.Binding { return k.Hints }, Model.handleHintsKey},
	{func(k keyMap) key.Binding { return k.Overview }, Model.handleOverviewKey},
	{func(k keyMap) key.Binding { return k.Simulate }, Model.handleSimulateKey},
	{func(k keyMap) key.Binding { return k.Fix }, Model.handleFixKey},
	{func(k keyMap) key.Binding { return k.Replay }, Model.handleReplayKey},
	{func(k keyMap) key.Binding { return k.BatchAudit }, Model.handleBatchAuditKey},
	{func(k keyMap) key.Binding { return k.BatchApply }, Model.handleBatchApplyKey},
	{func(k keyMap) key.Binding { return k.MetricMode }, Model.handleMetricModeKey},
	{func(k keyMap) key.Binding { return k.DryRun }, Model.handleDryRunKey},
	{func(k keyMap) key.Binding { return k.TabField }, Model.handleTabFieldKey},
	{func(k keyMap) key.Binding { return k.ShiftTabField }, Model.handleShiftTabFieldKey},
	{func(k keyMap) key.Binding { return k.IntervalUp }, Model.handleIntervalUpKey},
	{func(k keyMap) key.Binding { return k.IntervalDown }, Model.handleIntervalDownKey},
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	for _, h := range keyHandlers {
		if key.Matches(msg, h.binding(m.keys)) {
			return h.handle(m)
		}
	}
	return m, nil
}

func (m Model) handleQuitKey() (tea.Model, tea.Cmd) {
	return m, tea.Quit
}

func (m Model) handleUpKey() (tea.Model, tea.Cmd) {
	return m.moveCursor(-1), nil
}

func (m Model) handleDownKey() (tea.Model, tea.Cmd) {
	return m.moveCursor(+1), nil
}

func (m Model) handleRefreshKey() (tea.Model, tea.Cmd) {
	m.loading = true
	return m, fetchHPAs(m)
}

func (m Model) handlePauseKey() (tea.Model, tea.Cmd) {
	m.paused = !m.paused
	return m, nil
}

func (m Model) handleFilterKey() (tea.Model, tea.Cmd) {
	m.filtering = true
	m.filterInput.Focus()
	return m, nil
}

func (m Model) handleHelpKey() (tea.Model, tea.Cmd) {
	if m.viewMode == helpView {
		m.viewMode = listView
	} else {
		m.viewMode = helpView
	}
	return m, nil
}

func (m Model) handleSortKey() (tea.Model, tea.Cmd) {
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
}

func (m Model) handleJumpProblemKey() (tea.Model, tea.Cmd) {
	filtered := m.filteredItems()
	for i, item := range filtered {
		if item.Health != string(hpaanalysis.HealthOK) {
			m.cursor = i
			break
		}
	}
	return m, nil
}

func (m Model) handleMetricsKey() (tea.Model, tea.Cmd) {
	if m.viewMode == detailView || m.viewMode == listView {
		m.viewMode = metricsView
	}
	return m, nil
}

func (m Model) handleToggleSelectKey() (tea.Model, tea.Cmd) {
	if m.viewMode == listView {
		filtered := m.filteredItems()
		if m.cursor >= 0 && m.cursor < len(filtered) {
			k := filtered[m.cursor].Namespace + "/" + filtered[m.cursor].Name
			m.selected[k] = !m.selected[k]
			m.batchApplyConfirm = false
			m.batchApplyPreview = nil
		}
	}
	return m, nil
}

func (m Model) handleSelectAllKey() (tea.Model, tea.Cmd) {
	if m.viewMode == listView {
		for _, item := range m.filteredItems() {
			m.selected[item.Namespace+"/"+item.Name] = true
		}
		m.batchApplyConfirm = false
		m.batchApplyPreview = nil
	}
	return m, nil
}

func (m Model) handleDeselectAllKey() (tea.Model, tea.Cmd) {
	if m.viewMode == listView {
		m.selected = map[string]bool{}
		m.batchApplyConfirm = false
		m.batchApplyPreview = nil
	}
	return m, nil
}

func (m Model) handleHistoryKey() (tea.Model, tea.Cmd) {
	if m.viewMode == detailView {
		m.viewMode = historyView
		m.historyState = &historyState{}
	}
	return m, nil
}

func (m Model) handleHintsKey() (tea.Model, tea.Cmd) {
	if m.viewMode != detailView {
		return m, nil
	}
	filtered := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(filtered) {
		return m, nil
	}
	item := filtered[m.cursor]
	k := item.Namespace + "/" + item.Name
	if report, ok := m.reports[k]; ok && report.Analysis.MetricHints != nil {
		flows := hpaanalysis.BuildTroubleshootingFlows(report.Analysis.MetricHints.Hints)
		if len(flows) > 0 {
			m.hintsState = &hintsState{flows: flows}
			m.viewMode = hintsView
		}
	}
	return m, nil
}

func (m Model) handleOverviewKey() (tea.Model, tea.Cmd) {
	if m.viewMode == listView {
		m.viewMode = overviewView
	}
	return m, nil
}

func (m Model) handleMetricModeKey() (tea.Model, tea.Cmd) {
	if m.viewMode == simView && m.simState != nil {
		m.simState.metricMode = !m.simState.metricMode
		if m.simState.metricMode {
			m.simState.metricInput.Focus()
		} else {
			m.simState.metricInput.Blur()
		}
	}
	return m, nil
}

func (m Model) handleDryRunKey() (tea.Model, tea.Cmd) {
	if m.viewMode == fixView && m.fixState != nil && len(m.fixState.suggestions) > 0 {
		suggestion := m.fixState.suggestions[m.fixState.selected]
		switch {
		case suggestion.Patch != "":
			m.fixState.dryRunResult = "patch preview: " + suggestion.Patch
		case suggestion.Command != "":
			m.fixState.dryRunResult = "command preview: " + suggestion.Command
		default:
			m.fixState.dryRunResult = "no patch or command available for this suggestion"
		}
		m.fixState.applied = false
		m.fixState.applyErr = nil
	}
	return m, nil
}

func (m Model) handleTabFieldKey() (tea.Model, tea.Cmd) {
	return m.cycleSimField(+1), nil
}

func (m Model) handleShiftTabFieldKey() (tea.Model, tea.Cmd) {
	return m.cycleSimField(-1), nil
}

// cycleSimField moves simulation-field focus by delta, wrapping at both ends.
func (m Model) cycleSimField(delta int) Model {
	if m.viewMode != simView || m.simState == nil || m.simState.metricMode {
		return m
	}
	m.simState.focusIndex += delta
	if m.simState.focusIndex >= len(m.simState.fields) {
		m.simState.focusIndex = 0
	}
	if m.simState.focusIndex < 0 {
		m.simState.focusIndex = len(m.simState.fields) - 1
	}
	return m
}

func (m Model) handleIntervalUpKey() (tea.Model, tea.Cmd) {
	return m.adjustInterval(-1), nil
}

func (m Model) handleIntervalDownKey() (tea.Model, tea.Cmd) {
	return m.adjustInterval(+1), nil
}

// adjustInterval speeds up (direction -1) or slows down (direction +1) the
// refresh interval by half the current interval, clamped to [1s, 60s].
func (m Model) adjustInterval(direction int) Model {
	step := m.interval / 2
	if step < time.Second {
		step = time.Second
	}
	newInterval := m.interval + time.Duration(direction)*step
	if newInterval < time.Second {
		newInterval = time.Second
	}
	if newInterval > 60*time.Second {
		newInterval = 60 * time.Second
	}
	m.interval = newInterval
	return m
}
