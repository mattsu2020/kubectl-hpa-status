package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestNewModel_InitialState(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	if m.viewMode != listView {
		t.Fatal("expected listView mode")
	}
	if m.paused {
		t.Fatal("expected not paused")
	}
	if !m.loading {
		t.Fatal("expected loading state")
	}
	if m.interval.Seconds() != 5 {
		t.Fatalf("expected 5s interval, got %v", m.interval)
	}
}

func TestUpdate_WindowSize(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(Model)
	if m2.width != 120 {
		t.Fatalf("expected width 120, got %d", m2.width)
	}
	if m2.height != 40 {
		t.Fatalf("expected height 40, got %d", m2.height)
	}
	if cmd != nil {
		t.Fatal("expected nil cmd for window size")
	}
}

func TestUpdate_QuitKey(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
}

func TestUpdate_PauseToggle(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	if m.paused {
		t.Fatal("expected not paused initially")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m2 := updated.(Model)
	if !m2.paused {
		t.Fatal("expected paused after pressing p")
	}
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m3 := updated2.(Model)
	if m3.paused {
		t.Fatal("expected unpaused after pressing p again")
	}
}

func TestUpdate_EnterDetailView(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK"},
		{Namespace: "default", Name: "api", Health: "ERROR"},
	}
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{Name: "web", Namespace: "default"}},
		"default/api": {Analysis: hpaanalysis.Analysis{Name: "api", Namespace: "default"}},
	}

	// Move to second item and enter detail.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 := updated.(Model)
	if m2.cursor != 1 {
		t.Fatalf("expected cursor at 1, got %d", m2.cursor)
	}

	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := updated2.(Model)
	if m3.viewMode != detailView {
		t.Fatal("expected detailView after enter")
	}

	// Escape goes back to list.
	updated3, _ := m3.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m4 := updated3.(Model)
	if m4.viewMode != listView {
		t.Fatal("expected listView after escape")
	}
}

func TestUpdate_CursorClamped(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web"},
	}
	// Move up past 0.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m2 := updated.(Model)
	if m2.cursor != 0 {
		t.Fatalf("expected cursor clamped at 0, got %d", m2.cursor)
	}
}

func TestFilteredItems_NoFilter(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "web"},
		{Name: "api"},
	}
	filtered := m.filteredItems()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 items, got %d", len(filtered))
	}
}

func TestFilteredItems_WithName(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "web-server"},
		{Name: "api-gateway"},
	}
	m.filter = "web"
	filtered := m.filteredItems()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 item, got %d", len(filtered))
	}
	if filtered[0].Name != "web-server" {
		t.Fatalf("expected web-server, got %s", filtered[0].Name)
	}
}

func TestFilteredItems_CaseInsensitive(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "Web-Server"},
	}
	m.filter = "web"
	filtered := m.filteredItems()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 item (case insensitive), got %d", len(filtered))
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		s, substr string
		want      bool
	}{
		{"Hello", "ell", true},
		{"Hello", "HEL", true},
		{"Hello", "xyz", false},
		{"", "", true},
		{"abc", "", true},
	}
	for _, tt := range tests {
		got := containsIgnoreCase(tt.s, tt.substr)
		if got != tt.want {
			t.Errorf("containsIgnoreCase(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
		}
	}
}

func TestFetchResult_UpdatesItems(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	updated, _ := m.Update(fetchResultMsg{
		items: []hpaanalysis.ListItem{
			{Name: "web", Health: "OK", HealthScore: 100},
		},
		reports: map[string]*hpaanalysis.StatusReport{
			"/web": {Analysis: hpaanalysis.Analysis{Name: "web"}},
		},
	})
	m2 := updated.(Model)
	if len(m2.items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(m2.items))
	}
	if m2.loading {
		t.Fatal("expected loading=false after fetch")
	}
	if m2.err != nil {
		t.Fatalf("unexpected error: %v", m2.err)
	}
}

func TestFetchResult_SetsError(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	updated, _ := m.Update(fetchResultMsg{
		err: fmt.Errorf("connection refused"),
	})
	m2 := updated.(Model)
	if m2.err == nil {
		t.Fatal("expected error to be set")
	}
}
