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
	m = m.clone()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.updateWindowSize(msg)
	case tea.KeyMsg:
		return m.updateKeyMsg(msg)
	case tickMsg:
		return m.updateTick()
	case fetchResultMsg:
		return m.updateFetchResult(msg)
	case batchApplyResultMsg:
		return m.updateBatchApplyResult(msg)
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
	// A refresh supersedes any transient status notice and can change the HPA
	// state and regenerate suggestions. Never carry an armed live-apply
	// confirmation across that state boundary, and invalidate any fix-wizard
	// apply/dry-run still in flight (their results would otherwise mark a
	// stale suggestion set as applied).
	m.statusMessage = ""
	m.fixEpoch++
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
	m.hpaUIDs = msg.uids
	m.err = nil

	m.refreshFixStateAfterFetch()
	m.updateReplicaHistory()
	m.refocusAndClampCursorAfterFetch()
	return m, nil
}

// refreshFixStateAfterFetch re-binds the open fix wizard to the freshly
// fetched data. The wizard targets an HPA identity, not a cursor row, so a
// refresh must not leave it showing suggestions captured from a superseded
// report — nor let the displayed target and the suggestion source diverge.
// The wizard closes when its HPA is gone or was replaced (UID change, which
// catches a delete+recreate of the same name); otherwise the suggestions are
// regenerated from the latest report.
func (m *Model) refreshFixStateAfterFetch() {
	if m.fixState == nil {
		return
	}
	st := m.fixState
	if st.namespace == "" && st.name == "" {
		// Synthetic state without identity (only possible from tests): leave
		// it untouched rather than guessing a target.
		return
	}

	report, ok := m.reports[st.key()]
	if !ok {
		if m.viewMode == fixView {
			m.viewMode = listView
		}
		m.fixState = nil
		m.statusMessage = fmt.Sprintf("refresh closed the fix wizard: %s is gone", st.key())
		return
	}
	if err := st.verifyAgainstCurrentData(m.reports, m.hpaUIDs); err != nil {
		if m.viewMode == fixView {
			m.viewMode = listView
		}
		m.fixState = nil
		m.statusMessage = "refresh closed the fix wizard: " + err.Error()
		return
	}
	if len(report.Analysis.Actions.Suggestions) == 0 {
		if m.viewMode == fixView {
			m.viewMode = listView
		}
		m.fixState = nil
		m.statusMessage = fmt.Sprintf("refresh closed the fix wizard: no suggestions left for %s", st.key())
		return
	}

	// Keep the wizard anchored to the same (still-live) HPA. If the wizard's
	// UID was unknown when it opened, adopt the freshly observed one.
	if st.uid == "" {
		st.uid = m.hpaUIDs[st.key()]
	}
	st.regenerateFrom(report)
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

func (m Model) updateBatchApplyResult(msg batchApplyResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMessage = fmt.Sprintf("batch apply failed: %s (%v)", msg.title, msg.err)
	} else {
		m.statusMessage = msg.title
	}
	return m, nil
}

func (m Model) updateSimResult(msg simResultMsg) Model {
	if m.simState != nil {
		m.simState.update(msg)
	}
	return m
}

func (m Model) updateApplyResult(msg applyResultMsg) Model {
	if m.fixState == nil || msg.epoch != m.fixEpoch {
		return m
	}
	m.fixState.updateApply(msg)
	return m
}

func (m Model) updateDryRunResult(msg dryRunResultMsg) Model {
	if m.fixState == nil || msg.epoch != m.fixEpoch {
		return m
	}
	m.fixState.updateDryRun(msg)
	return m
}

func (m Model) updateReplayLoaded(msg replayLoadedMsg) Model {
	if m.replayState != nil {
		m.replayState.update(msg)
	}
	return m
}

func (m Model) updateBatchAudit(msg batchAuditMsg) Model {
	if m.batchAuditState == nil {
		return m
	}
	m.batchAuditState.update(msg)
	return m
}
