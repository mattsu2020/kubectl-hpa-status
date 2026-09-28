package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// anchoredHistoryModel returns a model positioned in the history view for
// "default/web" (cursor on the second item so silent cursor resets are
// detectable).
func anchoredHistoryModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, "default", Options{LoadHistoryFn: func(_ context.Context, _, _, _ string) ([]hpaanalysis.TimelineSnapshot, error) {
		return nil, nil
	}})
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "api", Health: "OK"},
		{Namespace: "default", Name: "web", Health: "OK"},
	}
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/api": {Analysis: hpaanalysis.Analysis{Meta: hpaanalysis.MetaView{Name: "api", Namespace: "default"}}},
		"default/web": {Analysis: hpaanalysis.Analysis{Meta: hpaanalysis.MetaView{Name: "web", Namespace: "default"}}},
	}
	m.hpaUIDs = map[string]string{"default/api": "uid-a", "default/web": "uid-b"}
	m.cursor = 1
	m.sortField = "name"
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(Model)
	if m2.viewMode != detailView {
		t.Fatal("expected detailView after enter")
	}
	updated2, _ := m2.Update(tea.KeyPressMsg{Text: "H"})
	m3 := updated2.(Model)
	if m3.viewMode != historyView {
		t.Fatal("expected historyView after H")
	}
	return m3
}

// TestListScopedKeysGuardedInAnchoredView pins the viewMode guards: sort,
// jump-to-problem, and filter are list-view actions and must be inert in the
// history view, whose rendering is anchored to the HPA the state was loaded
// for. Without the guard, S re-sorts and resets the cursor so the history
// header would describe a different HPA than the loaded snapshots.
func TestListScopedKeysGuardedInAnchoredView(t *testing.T) {
	for _, key := range []string{"S", "g", "/"} {
		m := anchoredHistoryModel(t)
		updated, _ := m.Update(tea.KeyPressMsg{Text: key})
		m2 := updated.(Model)
		if m2.viewMode != historyView {
			t.Fatalf("key %q must not leave the history view", key)
		}
		if m2.sortField != "name" || m2.cursor != 1 {
			t.Fatalf("key %q mutated list state: sortField=%q cursor=%d", key, m2.sortField, m2.cursor)
		}
		if key == "/" && m2.filtering {
			t.Fatal("filter input must not open outside the list view")
		}
	}
}

// TestHelpReturnsToOriginView pins the help-return wiring: opening help from
// the history view and pressing either ? or Esc must return to the history
// view, not silently drop it back to the list.
func TestHelpReturnsToOriginView(t *testing.T) {
	m := anchoredHistoryModel(t)

	updated, _ := m.Update(tea.KeyPressMsg{Text: "?"})
	m2 := updated.(Model)
	if m2.viewMode != helpView {
		t.Fatalf("expected helpView, got %d", m2.viewMode)
	}
	updated2, _ := m2.Update(tea.KeyPressMsg{Text: "?"})
	m3 := updated2.(Model)
	if m3.viewMode != historyView {
		t.Fatalf("? toggle must return to historyView, got %d", m3.viewMode)
	}
	if m3.historyState == nil {
		t.Fatal("history state must survive the help round-trip")
	}

	updated3, _ := m3.Update(tea.KeyPressMsg{Text: "?"})
	m4 := updated3.(Model)
	updated4, _ := m4.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m5 := updated4.(Model)
	if m5.viewMode != historyView {
		t.Fatalf("Esc from help must return to historyView, got %d", m5.viewMode)
	}
}

// TestHistoryLoadComputesChurnOnce pins that the churn analysis is computed
// when the load lands, instead of being recomputed on every render frame.
func TestHistoryLoadComputesChurnOnce(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := NewModel(nil, "default", Options{LoadHistoryFn: func(_ context.Context, _, _, _ string) ([]hpaanalysis.TimelineSnapshot, error) {
		return []hpaanalysis.TimelineSnapshot{
			{Timestamp: base, Desired: 2, Current: 2},
			{Timestamp: base.Add(time.Minute), Desired: 4, Current: 4},
			{Timestamp: base.Add(2 * time.Minute), Desired: 2, Current: 2},
		}, nil
	}})
	m.items = []hpaanalysis.ListItem{{Namespace: "default", Name: "web", Health: "OK"}}
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{Meta: hpaanalysis.MetaView{Name: "web", Namespace: "default"}}},
	}
	m.hpaUIDs = map[string]string{"default/web": "uid-1"}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(Model)
	updated2, cmd2 := m2.Update(tea.KeyPressMsg{Text: "H"})
	m3 := updated2.(Model)
	updated3, _ := m3.Update(cmd2())
	m4 := updated3.(Model)
	if m4.historyState == nil {
		t.Fatal("expected history state")
	}
	if m4.historyState.churnAnalysis == nil {
		t.Fatal("expected churnAnalysis computed at load time")
	}
	if m4.historyState.churnAnalysis.DirectionFlips == 0 {
		t.Fatalf("expected direction flips from 2→4→2, got %+v", m4.historyState.churnAnalysis)
	}
}
