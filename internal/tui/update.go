package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
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
