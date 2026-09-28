package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func historyTestModel(load HistoryLoader) Model {
	m := NewModel(nil, "default", Options{LoadHistoryFn: load})
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK"},
	}
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{
			Meta: hpaanalysis.MetaView{Name: "web", Namespace: "default"},
		}},
	}
	m.hpaUIDs = map[string]string{"default/web": "uid-1"}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(Model)
	if m2.viewMode != detailView {
		panic("expected detailView")
	}
	return m2
}

// TestHistoryKeyStartsBackgroundLoad pins the wiring that was previously
// missing: pressing H in the detail view must return a command that loads the
// selected HPA's history through LoadHistoryFn (anchored to ns/name + UID).
func TestHistoryKeyStartsBackgroundLoad(t *testing.T) {
	var gotNS, gotName, gotUID string
	m := historyTestModel(func(_ context.Context, namespace, name, uid string) ([]hpaanalysis.TimelineSnapshot, error) {
		gotNS, gotName, gotUID = namespace, name, uid
		return []hpaanalysis.TimelineSnapshot{{Timestamp: time.Now(), HealthScore: 90}}, nil
	})

	updated, cmd := m.Update(tea.KeyPressMsg{Text: "H"})
	m2 := updated.(Model)
	if m2.viewMode != historyView {
		t.Fatal("expected historyView after H")
	}
	if m2.historyState == nil || !m2.historyState.loading {
		t.Fatal("expected a loading historyState")
	}
	if cmd == nil {
		t.Fatal("expected a load command")
	}

	msg := cmd()
	loaded, ok := msg.(historyLoadedMsg)
	if !ok {
		t.Fatalf("load command produced %T, want historyLoadedMsg", msg)
	}
	if gotNS != "default" || gotName != "web" || gotUID != "uid-1" {
		t.Fatalf("load called with %s/%s uid=%s", gotNS, gotName, gotUID)
	}

	updated2, _ := m2.Update(loaded)
	m3 := updated2.(Model)
	if m3.historyState == nil || len(m3.historyState.snapshots) != 1 || m3.historyState.loading {
		t.Fatalf("history view did not attach the loaded snapshots: %+v", m3.historyState)
	}
}

// TestHistoryLoadErrorSurfacesInState pins the error path: a failing load is
// visible in the view state instead of silently rendering "no data".
func TestHistoryLoadErrorSurfacesInState(t *testing.T) {
	m := historyTestModel(func(_ context.Context, _, _, _ string) ([]hpaanalysis.TimelineSnapshot, error) {
		return nil, errors.New("store unavailable")
	})

	updated, cmd := m.Update(tea.KeyPressMsg{Text: "H"})
	if cmd == nil {
		t.Fatal("expected a load command")
	}
	updated2, _ := updated.(Model).Update(cmd())
	m2 := updated2.(Model)
	if m2.historyState == nil || m2.historyState.loadErr == nil {
		t.Fatal("expected loadErr in history state")
	}
}

// TestHistoryWithoutLoaderStaysEmpty pins the nil-callback behavior: no
// command is issued and the view shows its documented empty state.
func TestHistoryWithoutLoaderStaysEmpty(t *testing.T) {
	m := historyTestModel(nil)
	updated, cmd := m.Update(tea.KeyPressMsg{Text: "H"})
	m2 := updated.(Model)
	if m2.viewMode != historyView {
		t.Fatal("expected historyView after H")
	}
	if cmd != nil {
		t.Fatal("expected no load command without LoadHistoryFn")
	}
	if m2.historyState == nil || m2.historyState.loading {
		t.Fatal("expected a non-loading empty history state")
	}
}

// TestStaleHistoryResultDropped pins the identity guard: a result keyed to
// another HPA must not overwrite the open history view's snapshots.
func TestStaleHistoryResultDropped(t *testing.T) {
	m := historyTestModel(func(_ context.Context, _, _, _ string) ([]hpaanalysis.TimelineSnapshot, error) {
		return []hpaanalysis.TimelineSnapshot{{HealthScore: 10}}, nil
	})
	updated, _ := m.Update(tea.KeyPressMsg{Text: "H"})
	m2 := updated.(Model)

	stale := historyLoadedMsg{key: "other/thing", snapshots: []hpaanalysis.TimelineSnapshot{{HealthScore: 1}}}
	updated2, _ := m2.Update(stale)
	m3 := updated2.(Model)
	if m3.historyState == nil || len(m3.historyState.snapshots) != 0 {
		t.Fatalf("stale result must be dropped, got %+v", m3.historyState)
	}
}
