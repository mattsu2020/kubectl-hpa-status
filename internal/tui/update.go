package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
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
	case dryRunResultMsg:
		return m.updateDryRunResult(msg)
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
	// A refresh can change the HPA state and regenerate suggestions. Never
	// carry an armed live-apply confirmation across that state boundary.
	if m.fixState != nil {
		m.fixState.applyConfirm = false
	}
	m.batchApplyConfirm = false
	m.batchApplyPreview = nil
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
		m.fixState.applyConfirm = false
		m.fixState.applied = true
		m.fixState.applyErr = msg.err
	}
	return m, nil
}

func (m Model) updateDryRunResult(msg dryRunResultMsg) (tea.Model, tea.Cmd) {
	if m.fixState != nil {
		m.fixState.applyConfirm = false
		m.fixState.applied = false
		m.fixState.applyErr = msg.err
		if msg.err != nil {
			m.fixState.dryRunResult = fmt.Sprintf("validation failed: %v", msg.err)
		} else {
			m.fixState.dryRunResult = fmt.Sprintf("server-side validation passed: %s", msg.title)
		}
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
