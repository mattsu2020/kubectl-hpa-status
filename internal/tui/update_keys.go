package tui

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// keyBindingHandler pairs a key binding with its action. handleKey walks this
// table in order so cyclomatic complexity stays O(1) regardless of binding count.
type keyBindingHandler struct {
	binding key.Binding
	handle  func(Model) (tea.Model, tea.Cmd)
}

// handleKey dispatches key presses via a binding table. Each entry delegates
// to either a small inline mutation or a named handleXxx method. Table order
// is the match priority; the first matching binding wins.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	for _, entry := range m.keyHandlers() {
		if key.Matches(msg, entry.binding) {
			return entry.handle(m)
		}
	}
	return m, nil
}

// keyHandlers returns the full key binding table for the current model. Built
// per call so bindings always reflect the live m.keys configuration (tests
// may override individual bindings).
func (m Model) keyHandlers() []keyBindingHandler {
	return []keyBindingHandler{
		{m.keys.Quit, func(m Model) (tea.Model, tea.Cmd) { return m, tea.Quit }},
		{m.keys.Up, func(m Model) (tea.Model, tea.Cmd) { return m.moveCursor(-1), nil }},
		{m.keys.Down, func(m Model) (tea.Model, tea.Cmd) { return m.moveCursor(+1), nil }},
		{m.keys.Enter, func(m Model) (tea.Model, tea.Cmd) { return m.handleEnter() }},
		{m.keys.Escape, func(m Model) (tea.Model, tea.Cmd) { return m.handleEscape() }},
		{m.keys.Refresh, func(m Model) (tea.Model, tea.Cmd) {
			m.loading = true
			return m, fetchHPAs(m)
		}},
		{m.keys.Pause, func(m Model) (tea.Model, tea.Cmd) {
			m.paused = !m.paused
			return m, nil
		}},
		{m.keys.Filter, func(m Model) (tea.Model, tea.Cmd) {
			m.filtering = true
			m.filterInput.Focus()
			return m, nil
		}},
		{m.keys.Help, func(m Model) (tea.Model, tea.Cmd) { return m.toggleHelpView(), nil }},
		{m.keys.Sort, func(m Model) (tea.Model, tea.Cmd) { return m.handleSortKey(), nil }},
		{m.keys.JumpProblem, func(m Model) (tea.Model, tea.Cmd) { return m.handleJumpProblemKey(), nil }},
		{m.keys.Metrics, func(m Model) (tea.Model, tea.Cmd) {
			if m.viewMode == detailView || m.viewMode == listView {
				m.viewMode = metricsView
			}
			return m, nil
		}},
		{m.keys.ToggleSelect, func(m Model) (tea.Model, tea.Cmd) { return m.handleToggleSelectKey(), nil }},
		{m.keys.SelectAll, func(m Model) (tea.Model, tea.Cmd) { return m.handleSelectAllKey(), nil }},
		{m.keys.DeselectAll, func(m Model) (tea.Model, tea.Cmd) { return m.handleDeselectAllKey(), nil }},
		{m.keys.History, func(m Model) (tea.Model, tea.Cmd) {
			if m.viewMode == detailView {
				m.viewMode = historyView
				m.historyState = &historyState{}
			}
			return m, nil
		}},
		{m.keys.Hints, func(m Model) (tea.Model, tea.Cmd) { return m.handleHintsKey(), nil }},
		{m.keys.Overview, func(m Model) (tea.Model, tea.Cmd) {
			if m.viewMode == listView {
				m.viewMode = overviewView
			}
			return m, nil
		}},
		{m.keys.Simulate, func(m Model) (tea.Model, tea.Cmd) { return m.handleSimulateKey() }},
		{m.keys.Fix, func(m Model) (tea.Model, tea.Cmd) { return m.handleFixKey() }},
		{m.keys.Replay, func(m Model) (tea.Model, tea.Cmd) { return m.handleReplayKey() }},
		{m.keys.BatchAudit, func(m Model) (tea.Model, tea.Cmd) { return m.handleBatchAuditKey() }},
		{m.keys.BatchApply, func(m Model) (tea.Model, tea.Cmd) { return m.handleBatchApplyKey() }},
		{m.keys.MetricMode, func(m Model) (tea.Model, tea.Cmd) { return m.handleMetricModeKey(), nil }},
		{m.keys.DryRun, func(m Model) (tea.Model, tea.Cmd) { return m.handleDryRunKey() }},
		{m.keys.TabField, func(m Model) (tea.Model, tea.Cmd) { return m.handleTabField(+1), nil }},
		{m.keys.ShiftTabField, func(m Model) (tea.Model, tea.Cmd) { return m.handleTabField(-1), nil }},
		{m.keys.IntervalUp, func(m Model) (tea.Model, tea.Cmd) { return m.handleIntervalKey(-1), nil }},
		{m.keys.IntervalDown, func(m Model) (tea.Model, tea.Cmd) { return m.handleIntervalKey(+1), nil }},
	}
}

// toggleHelpView flips between the help overlay and the previous list view.
func (m Model) toggleHelpView() Model {
	if m.viewMode == helpView {
		m.viewMode = listView
	} else {
		m.viewMode = helpView
	}
	return m
}

// handleSortKey cycles through the sort fields (name → health-score → issue →
// namespace) and toggles the sort direction on each press.
func (m Model) handleSortKey() Model {
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
	return m
}

// handleJumpProblemKey moves the cursor to the first non-OK item in the list.
func (m Model) handleJumpProblemKey() Model {
	filtered := m.filteredItems()
	for i, item := range filtered {
		if item.Health != string(hpaanalysis.HealthOK) {
			m.cursor = i
			break
		}
	}
	return m
}

// handleToggleSelectKey toggles selection on the item under the cursor and
// resets any pending batch-apply preview.
func (m Model) handleToggleSelectKey() Model {
	if m.viewMode == listView {
		filtered := m.filteredItems()
		if m.cursor >= 0 && m.cursor < len(filtered) {
			k := filtered[m.cursor].Namespace + "/" + filtered[m.cursor].Name
			m.selected[k] = !m.selected[k]
			m.batchApplyConfirm = false
			m.batchApplyPreview = nil
		}
	}
	return m
}

// handleSelectAllKey selects every filtered item.
func (m Model) handleSelectAllKey() Model {
	if m.viewMode == listView {
		for _, item := range m.filteredItems() {
			m.selected[item.Namespace+"/"+item.Name] = true
		}
		m.batchApplyConfirm = false
		m.batchApplyPreview = nil
	}
	return m
}

// handleDeselectAllKey clears the selection set.
func (m Model) handleDeselectAllKey() Model {
	if m.viewMode == listView {
		m.selected = map[string]bool{}
		m.batchApplyConfirm = false
		m.batchApplyPreview = nil
	}
	return m
}

// handleHintsKey opens the troubleshooting-hints view for the item under the
// cursor when metric hints are available.
func (m Model) handleHintsKey() Model {
	if m.viewMode != detailView {
		return m
	}
	filtered := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(filtered) {
		return m
	}
	item := filtered[m.cursor]
	k := item.Namespace + "/" + item.Name
	report, ok := m.reports[k]
	if !ok || report.Analysis.MetricHints == nil {
		return m
	}
	flows := hpaanalysis.BuildTroubleshootingFlows(report.Analysis.MetricHints.Hints)
	if len(flows) > 0 {
		m.hintsState = &hintsState{flows: flows}
		m.viewMode = hintsView
	}
	return m
}

// handleMetricModeKey toggles metric-override input mode within the simulate view.
func (m Model) handleMetricModeKey() Model {
	if m.viewMode == simView && m.simState != nil {
		m.simState.metricMode = !m.simState.metricMode
		if m.simState.metricMode {
			m.simState.metricInput.Focus()
		} else {
			m.simState.metricInput.Blur()
		}
	}
	return m
}

// handleDryRunKey validates the selected suggestion with Kubernetes
// server-side dry-run without persisting it.
func (m Model) handleDryRunKey() (tea.Model, tea.Cmd) {
	if m.viewMode != fixView || m.fixState == nil || len(m.fixState.suggestions) == 0 {
		return m, nil
	}
	suggestion := m.fixState.suggestions[m.fixState.selected]
	m.fixState.applyConfirm = false
	m.fixState.applied = false
	m.fixState.applyErr = nil
	if suggestion.Patch == "" {
		m.fixState.dryRunResult = "no patch available for server-side validation"
		return m, nil
	}
	if !suggestion.Apply {
		m.fixState.dryRunResult = "this suggestion is advisory and is not approved for automatic apply"
		return m, nil
	}
	if m.opts.DryRunFn == nil {
		m.fixState.dryRunResult = "server-side dry-run is unavailable"
		return m, nil
	}

	filtered := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(filtered) {
		m.fixState.dryRunResult = "cannot resolve selected HPA"
		return m, nil
	}
	item := filtered[m.cursor]
	dryRunFn := m.opts.DryRunFn
	m.fixState.dryRunResult = "validating with Kubernetes API..."
	return m, func() tea.Msg {
		err := dryRunFn(m.ctx, item.Namespace, item.Name, []hpaanalysis.Suggestion{suggestion})
		return dryRunResultMsg{title: suggestion.Title, err: err}
	}
}

// handleTabField moves the input focus within the simulate view by delta
// (+1 forward / -1 backward), wrapping at both ends.
func (m Model) handleTabField(delta int) Model {
	if m.viewMode != simView || m.simState == nil || m.simState.metricMode {
		return m
	}
	n := len(m.simState.fields)
	if n == 0 {
		return m
	}
	m.simState.focusIndex += delta
	if m.simState.focusIndex >= n {
		m.simState.focusIndex = 0
	}
	if m.simState.focusIndex < 0 {
		m.simState.focusIndex = n - 1
	}
	return m
}

// handleIntervalKey adjusts the auto-refresh interval by delta half-steps
// (delta=+1 lengthens, -1 shortens), clamped to [1s, 60s].
func (m Model) handleIntervalKey(delta int) Model {
	step := m.interval / 2
	if step < time.Second {
		step = time.Second
	}
	var newInterval time.Duration
	if delta >= 0 {
		newInterval = m.interval + step
		if newInterval > 60*time.Second {
			newInterval = 60 * time.Second
		}
	} else {
		newInterval = m.interval - step
		if newInterval < time.Second {
			newInterval = time.Second
		}
	}
	m.interval = newInterval
	return m
}

// moveCursor advances the active view's selection/scroll cursor by delta
// (negative for up, positive for down), clamping at both ends. It centralizes
// the per-view cursor math that previously inlined two near-identical nested
// switches in handleKey. The lower bound is always 0; only the upper bound
// (list length) varies per view.
func (m Model) moveCursor(delta int) Model {
	switch m.viewMode {
	case listView:
		filtered := m.filteredItems()
		m.cursor = clampCursor(m.cursor+delta, len(filtered)-1)
	case fixView:
		if m.fixState != nil {
			m.fixState.selected = clampCursor(m.fixState.selected+delta, len(m.fixState.suggestions)-1)
			m.fixState.applyConfirm = false
		}
	case replayView:
		if m.replayState != nil && m.replayState.trace != nil {
			m.replayState.scrollPos = clampCursor(m.replayState.scrollPos+delta, maxInt(0, len(m.replayState.trace.Snapshots)-1))
		}
	case historyView:
		if m.historyState != nil {
			m.historyState.scrollPos = clampCursor(m.historyState.scrollPos+delta, maxInt(0, len(m.historyState.snapshots)-1))
		}
	case hintsView:
		if m.hintsState != nil {
			m.hintsState.selected = clampCursor(m.hintsState.selected+delta, len(m.hintsState.flows)-1)
		}
	}
	return m
}

// clampCursor clamps v into [0, hi]; when hi < 0 (empty list) it returns 0.
func clampCursor(v, hi int) int {
	if v < 0 {
		return 0
	}
	if hi < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
