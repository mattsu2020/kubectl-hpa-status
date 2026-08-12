package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// Update handles all bubbletea messages.
// Value receivers are intentional here: Bubbletea's architecture uses an
// immutable model pattern where each message produces a new model state
// rather than mutating the existing one. All methods on Model (Update, View,
// Init, filteredItems) use value receivers for consistency with this pattern.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.interactiveStates = m.clone()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.updateWindowSize(msg)
	case tea.KeyMsg:
		return m.updateKeyMsg(msg)
	case tickMsg:
		return m.updateTick()
	case fetchResultMsg:
		return m.updateFetchResult(msg)
	}
	if updated, cmd, handled := dispatchViewMessage(m, msg); handled {
		return updated, cmd
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
	if updated, cmd, handled := controllerForMode(m.viewMode).HandleKey(m, msg); handled {
		return updated, cmd
	}
	return m.handleKey(msg)
}

func (m Model) updateTick() (tea.Model, tea.Cmd) {
	if m.paused || m.loading {
		return m, tickCmd(m.interval)
	}
	m.loading = true
	m.fetchRequestID++
	return m, tea.Batch(fetchHPAs(m), tickCmd(m.interval))
}

func (m Model) updateFetchResult(msg fetchResultMsg) (tea.Model, tea.Cmd) {
	if msg.requestID != 0 && msg.requestID != m.fetchRequestID {
		return m, nil
	}
	m.loading = false
	m.lastRefresh = m.currentTime()
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

func (m Model) updateSimResult(msg simResultMsg) Model {
	if m.simState != nil {
		m.simState.result = msg.result
		m.simState.err = msg.err
	}
	return m
}

func (m Model) updateApplyResult(msg applyResultMsg) Model {
	if m.fixState != nil {
		m.fixState.applyConfirm = false
		m.fixState.applied = true
		m.fixState.applyErr = msg.err
	}
	return m
}

func (m Model) updateDryRunResult(msg dryRunResultMsg) Model {
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
	return m
}

func (m Model) updateReplayLoaded(msg replayLoadedMsg) Model {
	if m.replayState != nil {
		m.replayState.loading = false
		m.replayState.trace = msg.trace
		m.replayState.err = msg.err
	}
	return m
}

func (m Model) updateBatchAudit(msg batchAuditMsg) Model {
	if m.batchAuditState == nil {
		return m
	}
	m.batchAuditState.loading = false
	if msg.err != nil {
		m.batchAuditState.err = msg.err
		return m
	}
	m.batchAuditState.reports = msg.reports
	m.batchAuditState.results = buildBatchAuditEntries(msg.reports)
	return m
}
