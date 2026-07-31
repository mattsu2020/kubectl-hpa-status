package tui

import (
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestViewControllerRegistryComplete(t *testing.T) {
	t.Parallel()

	if got, want := len(viewControllerRegistry), int(viewModeCount); got != want {
		t.Fatalf("registered controllers = %d, want %d", got, want)
	}
	for mode := listView; mode < viewModeCount; mode++ {
		controller := viewControllerRegistry[mode]
		if controller == nil {
			t.Errorf("view mode %d has a nil controller", mode)
		}
	}
}

func TestViewControllerRenderDispatch(t *testing.T) {
	tests := []struct {
		name   string
		mode   viewMode
		render func(Model) string
	}{
		{name: "list", mode: listView, render: Model.renderListView},
		{name: "detail", mode: detailView, render: Model.renderDetailView},
		{name: "help", mode: helpView, render: Model.renderHelpView},
		{name: "metrics", mode: metricsView, render: Model.renderMetricsView},
		{name: "simulation", mode: simView, render: Model.renderSimView},
		{name: "fix", mode: fixView, render: Model.renderFixView},
		{name: "replay", mode: replayView, render: Model.renderReplayView},
		{name: "batch audit", mode: batchAuditView, render: Model.renderBatchAuditView},
		{name: "history", mode: historyView, render: Model.renderHistoryView},
		{name: "overview", mode: overviewView, render: Model.renderOverviewView},
		{name: "hints", mode: hintsView, render: Model.renderHintsView},
	}

	if got, want := len(tests), int(viewModeCount); got != want {
		t.Fatalf("render dispatch cases = %d, want %d", got, want)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := detailModel(Options{})
			got := controllerForMode(tt.mode).Render(m)
			want := tt.render(m)
			if got != want {
				t.Fatalf("view mode %d dispatched to the wrong renderer", tt.mode)
			}
		})
	}
}

func TestViewControllerCursorRules(t *testing.T) {
	t.Run("list owns the selected HPA cursor", func(t *testing.T) {
		m := detailModel(Options{})
		m.viewMode = listView
		m.items = append(m.items, hpaanalysis.ListItem{Namespace: "default", Name: "api"})

		got := controllerForMode(m.viewMode).MoveCursor(m, 1)
		if got.cursor != 1 {
			t.Fatalf("list cursor = %d, want 1", got.cursor)
		}
	})

	t.Run("fix owns suggestion selection and cancels confirmation", func(t *testing.T) {
		m := detailModel(Options{})
		m.viewMode = fixView
		m.fixState = &fixState{
			suggestions:  []hpaanalysis.Suggestion{{Title: "one"}, {Title: "two"}},
			applyConfirm: true,
		}

		got := controllerForMode(m.viewMode).MoveCursor(m, 1)
		if got.fixState.selected != 1 {
			t.Fatalf("fix selection = %d, want 1", got.fixState.selected)
		}
		if got.fixState.applyConfirm {
			t.Fatal("moving the fix selection must cancel apply confirmation")
		}
	})

	t.Run("replay owns timeline scrolling", func(t *testing.T) {
		m := detailModel(Options{})
		m.viewMode = replayView
		m.replayState = &replayState{trace: &hpaanalysis.TimelineTrace{
			Snapshots: []hpaanalysis.TimelineSnapshot{{}, {}},
		}}

		got := controllerForMode(m.viewMode).MoveCursor(m, 1)
		if got.replayState.scrollPos != 1 {
			t.Fatalf("replay scroll = %d, want 1", got.replayState.scrollPos)
		}
	})

	t.Run("history owns snapshot scrolling", func(t *testing.T) {
		m := detailModel(Options{})
		m.viewMode = historyView
		m.historyState = &historyState{snapshots: []hpaanalysis.TimelineSnapshot{{}, {}}}

		got := controllerForMode(m.viewMode).MoveCursor(m, 1)
		if got.historyState.scrollPos != 1 {
			t.Fatalf("history scroll = %d, want 1", got.historyState.scrollPos)
		}
	})

	t.Run("hints owns troubleshooting flow selection", func(t *testing.T) {
		m := detailModel(Options{})
		m.viewMode = hintsView
		m.hintsState = &hintsState{
			flows: []hpaanalysis.MetricHintTroubleshooting{{}, {}},
		}

		got := controllerForMode(m.viewMode).MoveCursor(m, 1)
		if got.hintsState.selected != 1 {
			t.Fatalf("hint selection = %d, want 1", got.hintsState.selected)
		}
	})

	noCursorModes := []viewMode{
		detailView,
		helpView,
		metricsView,
		simView,
		batchAuditView,
		overviewView,
	}
	for _, mode := range noCursorModes {
		t.Run("no-op mode "+viewModeTestName(mode), func(t *testing.T) {
			m := detailModel(Options{})
			m.viewMode = mode
			m.cursor = 7
			m.batchAuditState = &batchAuditState{scrollPos: 3}

			got := controllerForMode(mode).MoveCursor(m, 1)
			if got.cursor != 7 {
				t.Fatalf("top-level cursor changed in mode %d: got %d", mode, got.cursor)
			}
			if got.batchAuditState.scrollPos != 3 {
				t.Fatalf("batch audit scroll changed in mode %d: got %d", mode, got.batchAuditState.scrollPos)
			}
		})
	}
}

func TestViewControllerEscapeTransitions(t *testing.T) {
	tests := []struct {
		name       string
		mode       viewMode
		wantMode   viewMode
		assertions func(*testing.T, Model)
	}{
		{name: "list", mode: listView, wantMode: listView, assertions: assertBatchPreviewCleared},
		{name: "detail", mode: detailView, wantMode: listView, assertions: assertBatchPreviewCleared},
		{name: "help", mode: helpView, wantMode: listView, assertions: assertBatchPreviewCleared},
		{name: "metrics", mode: metricsView, wantMode: listView, assertions: assertBatchPreviewCleared},
		{name: "simulation", mode: simView, wantMode: detailView, assertions: assertInteractiveStatesCleared},
		{name: "fix", mode: fixView, wantMode: detailView, assertions: assertInteractiveStatesCleared},
		{name: "replay", mode: replayView, wantMode: detailView, assertions: assertInteractiveStatesCleared},
		{name: "batch audit", mode: batchAuditView, wantMode: listView, assertions: func(t *testing.T, m Model) {
			t.Helper()
			if m.batchAuditState != nil {
				t.Fatal("batch audit state was not cleared")
			}
		}},
		{name: "history", mode: historyView, wantMode: detailView, assertions: func(t *testing.T, m Model) {
			t.Helper()
			if m.historyState != nil {
				t.Fatal("history state was not cleared")
			}
		}},
		{name: "overview", mode: overviewView, wantMode: listView},
		{name: "hints", mode: hintsView, wantMode: detailView, assertions: func(t *testing.T, m Model) {
			t.Helper()
			if m.hintsState != nil {
				t.Fatal("hints state was not cleared")
			}
		}},
	}

	if got, want := len(tests), int(viewModeCount); got != want {
		t.Fatalf("escape transition cases = %d, want %d", got, want)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := modelWithAllViewStates()
			m.viewMode = tt.mode

			got, cmd := controllerForMode(tt.mode).HandleEscape(m)
			if cmd != nil {
				t.Fatal("Escape unexpectedly returned a command")
			}
			if got.viewMode != tt.wantMode {
				t.Fatalf("view mode after Escape = %d, want %d", got.viewMode, tt.wantMode)
			}
			if tt.assertions != nil {
				tt.assertions(t, got)
			}
		})
	}
}

func TestViewControllerUnknownModeFallback(t *testing.T) {
	for _, mode := range []viewMode{-1, viewModeCount + 1} {
		t.Run(viewModeTestName(mode), func(t *testing.T) {
			m := detailModel(Options{})
			m.viewMode = mode
			m.cursor = 0
			m.batchApplyConfirm = true
			m.batchApplyPreview = []string{"default/web: preview"}

			if got := m.View().Content; !strings.Contains(got, "web") {
				t.Fatalf("unknown mode did not render the safe list fallback:\n%s", got)
			}

			afterDown, _ := pressTUIKey(t, m, "j")
			if afterDown.viewMode != mode || afterDown.cursor != 0 {
				t.Fatalf("cursor input mutated unknown mode: mode=%d cursor=%d", afterDown.viewMode, afterDown.cursor)
			}
			afterEnter, cmd := pressTUIKey(t, afterDown, "enter")
			if cmd != nil || afterEnter.viewMode != mode {
				t.Fatalf("Enter mutated unknown mode: mode=%d cmd=%v", afterEnter.viewMode, cmd)
			}
			afterEscape, cmd := pressTUIKey(t, afterEnter, "esc")
			if cmd != nil || afterEscape.viewMode != listView {
				t.Fatalf("Escape did not recover to list view: mode=%d cmd=%v", afterEscape.viewMode, cmd)
			}
			assertBatchPreviewCleared(t, afterEscape)
		})
	}
}

func modelWithAllViewStates() Model {
	m := detailModel(Options{})
	m.simState = &simState{}
	m.fixState = &fixState{}
	m.replayState = &replayState{}
	m.batchAuditState = &batchAuditState{}
	m.historyState = &historyState{}
	m.hintsState = &hintsState{}
	m.batchApplyConfirm = true
	m.batchApplyPreview = []string{"default/web: preview"}
	return m
}

func assertBatchPreviewCleared(t *testing.T, m Model) {
	t.Helper()
	if m.batchApplyConfirm || m.batchApplyPreview != nil {
		t.Fatal("batch apply preview was not cleared")
	}
}

func assertInteractiveStatesCleared(t *testing.T, m Model) {
	t.Helper()
	if m.simState != nil || m.fixState != nil || m.replayState != nil {
		t.Fatal("interactive states were not cleared")
	}
}

func viewModeTestName(mode viewMode) string {
	switch mode {
	case listView:
		return "list"
	case detailView:
		return "detail"
	case helpView:
		return "help"
	case metricsView:
		return "metrics"
	case simView:
		return "simulation"
	case fixView:
		return "fix"
	case replayView:
		return "replay"
	case batchAuditView:
		return "batch-audit"
	case historyView:
		return "history"
	case overviewView:
		return "overview"
	case hintsView:
		return "hints"
	default:
		return "unknown"
	}
}
