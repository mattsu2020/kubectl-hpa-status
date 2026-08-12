package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// viewController is the boundary between the top-level Bubble Tea model and
// behavior owned by an individual view. Keeping rendering and view-local key
// handling together makes adding a mode an explicit registry change instead
// of growing switches on Model.
type viewController interface {
	Render(Model) string
	HandleMessage(Model, tea.Msg) (Model, tea.Cmd, bool)
	HandleKey(Model, tea.KeyMsg) (Model, tea.Cmd, bool)
	MoveCursor(Model, int) Model
	HandleEnter(Model) (Model, tea.Cmd)
	HandleEscape(Model) (Model, tea.Cmd)
}

// viewControllerFuncs lets simple views provide only the behavior they own.
// Nil handlers are intentional no-ops.
type viewControllerFuncs struct {
	render        func(Model) string
	moveCursor    func(Model, int) Model
	handleEnter   func(Model) (Model, tea.Cmd)
	handleEscape  func(Model) (Model, tea.Cmd)
	handleKey     func(Model, tea.KeyMsg) (Model, tea.Cmd, bool)
	handleMessage func(Model, tea.Msg) (Model, tea.Cmd, bool)
}

func (c viewControllerFuncs) HandleMessage(m Model, msg tea.Msg) (Model, tea.Cmd, bool) {
	if c.handleMessage == nil {
		return m, nil, false
	}
	return c.handleMessage(m, msg)
}

func (c viewControllerFuncs) HandleKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if c.handleKey == nil {
		return m, nil, false
	}
	return c.handleKey(m, msg)
}

func (c viewControllerFuncs) Render(m Model) string {
	if c.render == nil {
		return ""
	}
	return c.render(m)
}

func (c viewControllerFuncs) MoveCursor(m Model, delta int) Model {
	if c.moveCursor == nil {
		return m
	}
	return c.moveCursor(m, delta)
}

func (c viewControllerFuncs) HandleEnter(m Model) (Model, tea.Cmd) {
	if c.handleEnter == nil {
		return m, nil
	}
	return c.handleEnter(m)
}

func (c viewControllerFuncs) HandleEscape(m Model) (Model, tea.Cmd) {
	if c.handleEscape == nil {
		return m, nil
	}
	return c.handleEscape(m)
}

// viewControllerRegistry is the single mode-to-controller mapping. The
// coverage test is tied to viewModeCount so a newly added mode cannot silently
// render a blank screen or inherit unrelated cursor behavior.
var viewControllerRegistry = [viewModeCount]viewController{
	listView: viewControllerFuncs{
		render:       Model.renderListView,
		moveCursor:   moveListViewCursor,
		handleEnter:  enterListView,
		handleEscape: escapeListView,
	},
	detailView: viewControllerFuncs{
		render:       Model.renderDetailView,
		handleEscape: escapeToListView,
	},
	helpView: viewControllerFuncs{
		render:       Model.renderHelpView,
		handleEscape: escapeToListView,
	},
	metricsView: viewControllerFuncs{
		render:       Model.renderMetricsView,
		handleEscape: escapeToListView,
	},
	simView: viewControllerFuncs{
		render:        Model.renderSimView,
		handleKey:     handleSimViewInput,
		handleMessage: handleSimViewMessage,
		handleEnter:   enterSimView,
		handleEscape:  escapeInteractiveView,
	},
	fixView: viewControllerFuncs{
		render:        Model.renderFixView,
		handleMessage: handleFixViewMessage,
		moveCursor:    moveFixViewCursor,
		handleEnter:   enterFixView,
		handleEscape:  escapeInteractiveView,
	},
	replayView: viewControllerFuncs{
		render:        Model.renderReplayView,
		handleMessage: handleReplayViewMessage,
		moveCursor:    moveReplayViewCursor,
		handleEscape:  escapeInteractiveView,
	},
	batchAuditView: viewControllerFuncs{
		render:        Model.renderBatchAuditView,
		handleMessage: handleBatchAuditViewMessage,
		handleEscape:  escapeBatchAuditView,
	},
	historyView: viewControllerFuncs{
		render:       Model.renderHistoryView,
		moveCursor:   moveHistoryViewCursor,
		handleEscape: escapeHistoryView,
	},
	overviewView: viewControllerFuncs{
		render:       Model.renderOverviewView,
		handleEscape: escapeOverviewView,
	},
	hintsView: viewControllerFuncs{
		render:       Model.renderHintsView,
		moveCursor:   moveHintsViewCursor,
		handleEscape: escapeHintsView,
	},
}

func handleSimViewMessage(m Model, msg tea.Msg) (Model, tea.Cmd, bool) {
	result, ok := msg.(simResultMsg)
	if !ok {
		return m, nil, false
	}
	return m.updateSimResult(result), nil, true
}

func handleFixViewMessage(m Model, msg tea.Msg) (Model, tea.Cmd, bool) {
	switch result := msg.(type) {
	case applyResultMsg:
		return m.updateApplyResult(result), nil, true
	case dryRunResultMsg:
		return m.updateDryRunResult(result), nil, true
	default:
		return m, nil, false
	}
}

func handleReplayViewMessage(m Model, msg tea.Msg) (Model, tea.Cmd, bool) {
	result, ok := msg.(replayLoadedMsg)
	if !ok {
		return m, nil, false
	}
	return m.updateReplayLoaded(result), nil, true
}

func handleBatchAuditViewMessage(m Model, msg tea.Msg) (Model, tea.Cmd, bool) {
	result, ok := msg.(batchAuditMsg)
	if !ok {
		return m, nil, false
	}
	return m.updateBatchAudit(result), nil, true
}

func dispatchViewMessage(m Model, msg tea.Msg) (Model, tea.Cmd, bool) {
	for _, controller := range viewControllerRegistry {
		if controller == nil {
			continue
		}
		if updated, cmd, handled := controller.HandleMessage(m, msg); handled {
			return updated, cmd, true
		}
	}
	return m, nil, false
}

func handleSimViewInput(m Model, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.simState == nil {
		return m, nil, false
	}
	if m.simState.metricMode && m.simState.metricInput.Focused() {
		updated, cmd := m.handleSimInput(msg)
		return updated.(Model), cmd, true
	}
	if !m.simState.metricMode {
		if updated, handled := m.handleSimFieldInput(msg); handled {
			return updated.(Model), nil, true
		}
	}
	return m, nil, false
}

// fallbackViewController keeps a corrupt or future unknown mode usable. It
// renders the list without mutating its cursor and lets Escape recover the
// model to a known mode.
var fallbackViewController viewController = viewControllerFuncs{
	render:       Model.renderListView,
	handleEscape: escapeToListView,
}

func controllerForMode(mode viewMode) viewController {
	if mode < listView || mode >= viewModeCount {
		return fallbackViewController
	}
	controller := viewControllerRegistry[mode]
	if controller == nil {
		return fallbackViewController
	}
	return controller
}

func moveListViewCursor(m Model, delta int) Model {
	filtered := m.filteredItems()
	m.cursor = clampCursor(m.cursor+delta, len(filtered)-1)
	return m
}

func moveFixViewCursor(m Model, delta int) Model {
	if m.fixState != nil {
		m.fixState.selected = clampCursor(m.fixState.selected+delta, len(m.fixState.suggestions)-1)
		m.fixState.applyConfirm = false
	}
	return m
}

func moveReplayViewCursor(m Model, delta int) Model {
	if m.replayState != nil && m.replayState.trace != nil {
		m.replayState.scrollPos = clampCursor(
			m.replayState.scrollPos+delta,
			maxInt(0, len(m.replayState.trace.Snapshots)-1),
		)
	}
	return m
}

func moveHistoryViewCursor(m Model, delta int) Model {
	if m.historyState != nil {
		m.historyState.scrollPos = clampCursor(
			m.historyState.scrollPos+delta,
			maxInt(0, len(m.historyState.snapshots)-1),
		)
	}
	return m
}

func moveHintsViewCursor(m Model, delta int) Model {
	if m.hintsState != nil {
		m.hintsState.selected = clampCursor(m.hintsState.selected+delta, len(m.hintsState.flows)-1)
	}
	return m
}

func enterListView(m Model) (Model, tea.Cmd) {
	filtered := m.filteredItems()
	if m.cursor >= 0 && m.cursor < len(filtered) {
		m.viewMode = detailView
	}
	return m, nil
}

func enterSimView(m Model) (Model, tea.Cmd) {
	if m.simState == nil {
		return m, nil
	}
	return m, m.runSimulation()
}

func enterFixView(m Model) (Model, tea.Cmd) {
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

func escapeListView(m Model) (Model, tea.Cmd) {
	m.batchApplyConfirm = false
	m.batchApplyPreview = nil
	return m, nil
}

func escapeToListView(m Model) (Model, tea.Cmd) {
	m.viewMode = listView
	m.batchApplyConfirm = false
	m.batchApplyPreview = nil
	return m, nil
}

func escapeInteractiveView(m Model) (Model, tea.Cmd) {
	m.simState = nil
	m.fixState = nil
	m.replayState = nil
	m.viewMode = detailView
	return m, nil
}

func escapeHistoryView(m Model) (Model, tea.Cmd) {
	m.historyState = nil
	m.viewMode = detailView
	return m, nil
}

func escapeHintsView(m Model) (Model, tea.Cmd) {
	m.hintsState = nil
	m.viewMode = detailView
	return m, nil
}

func escapeBatchAuditView(m Model) (Model, tea.Cmd) {
	m.batchAuditState = nil
	m.viewMode = listView
	return m, nil
}

func escapeOverviewView(m Model) (Model, tea.Cmd) {
	m.viewMode = listView
	return m, nil
}
