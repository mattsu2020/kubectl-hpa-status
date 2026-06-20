package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// Update handles all bubbletea messages.
// Value receivers are intentional here: Bubbletea's architecture uses an
// immutable model pattern where each message produces a new model state
// rather than mutating the existing one. All methods on Model (Update, View,
// Init, filteredItems) use value receivers for consistency with this pattern.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.updateWindowSize(msg)
	case tea.KeyMsg:
		return m.updateKeyMsg(msg)
	case tickMsg:
		return m.updateTick()
	case fetchResultMsg:
		return m.updateFetchResult(msg)
	case simResultMsg:
		return m.updateSimResult(msg)
	case applyResultMsg:
		return m.updateApplyResult(msg)
	case replayLoadedMsg:
		return m.updateReplayLoaded(msg)
	case batchAuditMsg:
		return m.updateBatchAudit(msg)
	}
	return m, nil
}

func (m Model) updateWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	return m, nil
}

func (m Model) updateKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If filter input is active, handle filter input keys.
	if m.filtering {
		return m.handleFilterInput(msg)
	}
	// If in simulation view and textinput is focused, delegate.
	if m.viewMode == simView && m.simState != nil && m.simState.metricMode && m.simState.metricInput.Focused() {
		return m.handleSimInput(msg)
	}
	if m.viewMode == simView && m.simState != nil && !m.simState.metricMode {
		if updated, handled := m.handleSimFieldInput(msg); handled {
			return updated, nil
		}
	}
	return m.handleKey(msg)
}

func (m Model) updateTick() (tea.Model, tea.Cmd) {
	if m.paused {
		return m, tickCmd(m.interval)
	}
	return m, tea.Batch(fetchHPAs(m), tickCmd(m.interval))
}

func (m Model) updateFetchResult(msg fetchResultMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	m.lastRefresh = time.Now()
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.items = msg.items
	m.reports = msg.reports
	m.err = nil

	m.updateReplicaHistory()
	m.refocusAndClampCursorAfterFetch()
	return m, nil
}

// updateReplicaHistory appends the current desired replica count per HPA, capping history length.
func (m *Model) updateReplicaHistory() {
	const maxReplicaHistoryPoints = 15
	for _, item := range m.items {
		key := item.Namespace + "/" + item.Name
		history := m.replicaHistory[key]
		history = append(history, float64(item.Desired))
		if len(history) > maxReplicaHistoryPoints {
			history = history[len(history)-maxReplicaHistoryPoints:]
		}
		m.replicaHistory[key] = history
	}
}

// refocusAndClampCursorAfterFetch re-sorts items, focuses the initial item on first load, and clamps the cursor.
func (m *Model) refocusAndClampCursorAfterFetch() {
	if m.sortField != "" {
		m.sortItems()
	}
	if !m.initialFocused {
		m.focusInitialItem()
		m.initialFocused = true
	}
	filtered := m.filteredItems()
	if m.cursor >= len(filtered) {
		m.cursor = len(filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m Model) updateSimResult(msg simResultMsg) (tea.Model, tea.Cmd) {
	if m.simState != nil {
		m.simState.result = msg.result
		m.simState.err = msg.err
	}
	return m, nil
}

func (m Model) updateApplyResult(msg applyResultMsg) (tea.Model, tea.Cmd) {
	if m.fixState != nil {
		m.fixState.applied = true
		m.fixState.applyErr = msg.err
	}
	return m, nil
}

func (m Model) updateReplayLoaded(msg replayLoadedMsg) (tea.Model, tea.Cmd) {
	if m.replayState != nil {
		m.replayState.loading = false
		m.replayState.trace = msg.trace
		m.replayState.err = msg.err
	}
	return m, nil
}

func (m Model) updateBatchAudit(msg batchAuditMsg) (tea.Model, tea.Cmd) {
	if m.batchAuditState == nil {
		return m, nil
	}
	m.batchAuditState.loading = false
	if msg.err != nil {
		m.batchAuditState.err = msg.err
		return m, nil
	}
	m.batchAuditState.reports = msg.reports
	m.batchAuditState.results = buildBatchAuditEntries(msg.reports)
	return m, nil
}

//nolint:gocyclo // Key dispatch table: each case is a flat, independent key binding handler.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Up):
		return m.moveCursor(-1), nil

	case key.Matches(msg, m.keys.Down):
		return m.moveCursor(+1), nil

	case key.Matches(msg, m.keys.Enter):
		return m.handleEnter()

	case key.Matches(msg, m.keys.Escape):
		return m.handleEscape()

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
			if item.Health != string(hpaanalysis.HealthOK) {
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
				k := filtered[m.cursor].Namespace + "/" + filtered[m.cursor].Name
				m.selected[k] = !m.selected[k]
				m.batchApplyConfirm = false
				m.batchApplyPreview = nil
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.SelectAll):
		if m.viewMode == listView {
			for _, item := range m.filteredItems() {
				m.selected[item.Namespace+"/"+item.Name] = true
			}
			m.batchApplyConfirm = false
			m.batchApplyPreview = nil
		}
		return m, nil

	case key.Matches(msg, m.keys.DeselectAll):
		if m.viewMode == listView {
			m.selected = map[string]bool{}
			m.batchApplyConfirm = false
			m.batchApplyPreview = nil
		}
		return m, nil

	case key.Matches(msg, m.keys.History):
		if m.viewMode == detailView {
			m.viewMode = historyView
			m.historyState = &historyState{}
		}
		return m, nil

	case key.Matches(msg, m.keys.Hints):
		if m.viewMode == detailView {
			filtered := m.filteredItems()
			if m.cursor >= 0 && m.cursor < len(filtered) {
				item := filtered[m.cursor]
				k := item.Namespace + "/" + item.Name
				if report, ok := m.reports[k]; ok && report.Analysis.MetricHints != nil {
					flows := hpaanalysis.BuildTroubleshootingFlows(report.Analysis.MetricHints.Hints)
					if len(flows) > 0 {
						m.hintsState = &hintsState{flows: flows}
						m.viewMode = hintsView
					}
				}
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Overview):
		if m.viewMode == listView {
			m.viewMode = overviewView
		}
		return m, nil

	case key.Matches(msg, m.keys.Simulate):
		return m.handleSimulateKey()

	case key.Matches(msg, m.keys.Fix):
		return m.handleFixKey()

	case key.Matches(msg, m.keys.Replay):
		return m.handleReplayKey()

	case key.Matches(msg, m.keys.BatchAudit):
		return m.handleBatchAuditKey()

	case key.Matches(msg, m.keys.BatchApply):
		return m.handleBatchApplyKey()

	case key.Matches(msg, m.keys.MetricMode):
		if m.viewMode == simView && m.simState != nil {
			m.simState.metricMode = !m.simState.metricMode
			if m.simState.metricMode {
				m.simState.metricInput.Focus()
			} else {
				m.simState.metricInput.Blur()
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.DryRun):
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

	case key.Matches(msg, m.keys.TabField):
		if m.viewMode == simView && m.simState != nil && !m.simState.metricMode {
			m.simState.focusIndex++
			if m.simState.focusIndex >= len(m.simState.fields) {
				m.simState.focusIndex = 0
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.ShiftTabField):
		if m.viewMode == simView && m.simState != nil && !m.simState.metricMode {
			m.simState.focusIndex--
			if m.simState.focusIndex < 0 {
				m.simState.focusIndex = len(m.simState.fields) - 1
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.IntervalUp):
		step := m.interval / 2
		if step < time.Second {
			step = time.Second
		}
		newInterval := m.interval - step
		if newInterval < time.Second {
			newInterval = time.Second
		}
		m.interval = newInterval
		return m, nil

	case key.Matches(msg, m.keys.IntervalDown):
		step := m.interval / 2
		if step < time.Second {
			step = time.Second
		}
		newInterval := m.interval + step
		if newInterval > 60*time.Second {
			newInterval = 60 * time.Second
		}
		m.interval = newInterval
		return m, nil
	}

	return m, nil
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

func (m Model) handleSimFieldInput(msg tea.KeyMsg) (tea.Model, bool) {
	if m.simState == nil || len(m.simState.fields) == 0 {
		return m, false
	}
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyCtrlH:
		field := &m.simState.fields[m.simState.focusIndex]
		if len(field.Value) > 0 {
			field.Value = field.Value[:len(field.Value)-1]
		}
		return m, true
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return m, false
		}
		changed := false
		for _, r := range msg.Runes {
			if (r >= '0' && r <= '9') || r == '-' || r == '.' {
				field := &m.simState.fields[m.simState.focusIndex]
				field.Value += string(r)
				changed = true
			}
		}
		return m, changed
	}
	return m, false
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
		return m, m.applyFix()
	}

	return m, nil
}

// handleEscape processes the Escape key based on the current view mode.
func (m Model) handleEscape() (tea.Model, tea.Cmd) {
	switch m.viewMode {
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
