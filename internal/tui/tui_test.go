package tui

import (
	"fmt"
	"testing"
	"time"

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

func TestNewModel_CustomInterval(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 2 * time.Second})
	if m.interval != 2*time.Second {
		t.Fatalf("expected 2s interval, got %v", m.interval)
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

func TestFetchResult_FocusesInitialDetailItem(t *testing.T) {
	m := NewModel(nil, "default", Options{
		InitialName:   "api",
		InitialNS:     "default",
		StartInDetail: true,
	})
	updated, _ := m.Update(fetchResultMsg{
		items: []hpaanalysis.ListItem{
			{Namespace: "default", Name: "web", Health: "OK"},
			{Namespace: "default", Name: "api", Health: "WARN"},
		},
		reports: map[string]*hpaanalysis.StatusReport{
			"default/web": {Analysis: hpaanalysis.Analysis{Name: "web", Namespace: "default"}},
			"default/api": {Analysis: hpaanalysis.Analysis{Name: "api", Namespace: "default"}},
		},
	})
	m2 := updated.(Model)
	if m2.cursor != 1 {
		t.Fatalf("expected cursor to focus api at index 1, got %d", m2.cursor)
	}
	if m2.viewMode != detailView {
		t.Fatal("expected detailView for watch dashboard initial item")
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

func TestHelpView_Toggle(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.items = []hpaanalysis.ListItem{{Name: "web"}}
	if m.viewMode != listView {
		t.Fatal("expected listView initially")
	}

	// Press ? to open help.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m2 := updated.(Model)
	if m2.viewMode != helpView {
		t.Fatal("expected helpView after pressing ?")
	}

	// Press ? again to close help.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m3 := updated2.(Model)
	if m3.viewMode != listView {
		t.Fatal("expected listView after pressing ? again")
	}
}

func TestHelpView_EscapeClose(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.items = []hpaanalysis.ListItem{{Name: "web"}}
	m.viewMode = helpView

	// Press Esc to close help.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)
	if m2.viewMode != listView {
		t.Fatal("expected listView after pressing Esc in helpView")
	}
}

func TestSort_Cycling(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "web", HealthScore: 80},
		{Name: "api", HealthScore: 50},
	}

	// First S press: sortField not in cycle, defaults to health-score.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m2 := updated.(Model)
	if m2.sortField != "health-score" {
		t.Fatalf("expected sortField health-score, got %q", m2.sortField)
	}

	// Second S press: cycles from health-score -> issue.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m3 := updated2.(Model)
	if m3.sortField != "issue" {
		t.Fatalf("expected sortField issue, got %q", m3.sortField)
	}

	// Third S press: cycles from issue -> namespace.
	updated3, _ := m3.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m4 := updated3.(Model)
	if m4.sortField != "namespace" {
		t.Fatalf("expected sortField namespace, got %q", m4.sortField)
	}

	// Fourth S press: cycles from namespace -> name.
	updated4, _ := m4.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m5 := updated4.(Model)
	if m5.sortField != "name" {
		t.Fatalf("expected sortField name, got %q", m5.sortField)
	}

	// Fifth S press: cycles from name -> health-score (wraps around).
	updated5, _ := m5.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m6 := updated5.(Model)
	if m6.sortField != "health-score" {
		t.Fatalf("expected sortField health-score (wrap), got %q", m6.sortField)
	}
}

func TestSort_OrdersItems(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "beta", HealthScore: 50},
		{Name: "alpha", HealthScore: 90},
		{Name: "gamma", HealthScore: 30},
	}

	// Sort by name.
	m.sortField = "name"
	m.sortDescending = false
	m.sortItems()

	if m.items[0].Name != "alpha" {
		t.Fatalf("expected first item alpha, got %q", m.items[0].Name)
	}
	if m.items[2].Name != "gamma" {
		t.Fatalf("expected last item gamma, got %q", m.items[2].Name)
	}

	// Sort descending by health-score.
	m.sortField = "health-score"
	m.sortDescending = true
	m.sortItems()

	if m.items[0].Name != "alpha" {
		t.Fatalf("expected first item alpha (score 90 desc), got %q", m.items[0].Name)
	}
}

func TestJumpProblem(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "web", Health: "OK"},
		{Name: "api", Health: "WARN"},
		{Name: "worker", Health: "ERROR"},
		{Name: "cache", Health: "OK"},
	}

	// Press g to jump to first non-OK item.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m2 := updated.(Model)
	if m2.cursor != 1 {
		t.Fatalf("expected cursor at 1 (first non-OK), got %d", m2.cursor)
	}
}

func TestJumpProblem_AllOK(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "web", Health: "OK"},
		{Name: "api", Health: "OK"},
	}

	// Press g when all items are OK: cursor should stay at 0.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m2 := updated.(Model)
	if m2.cursor != 0 {
		t.Fatalf("expected cursor to stay at 0, got %d", m2.cursor)
	}
}

func TestFilteredItems_ByHealth(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "web", Health: "OK"},
		{Name: "api", Health: "ERROR"},
	}
	m.filter = "error"
	filtered := m.filteredItems()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 item matching health 'error', got %d", len(filtered))
	}
	if filtered[0].Name != "api" {
		t.Fatalf("expected api, got %s", filtered[0].Name)
	}
}

func TestFilteredItems_ByIssue(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "web", Issue: "ScalingLimited"},
		{Name: "api", Issue: "OK"},
	}
	m.filter = "scaling"
	filtered := m.filteredItems()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 item matching issue, got %d", len(filtered))
	}
	if filtered[0].Name != "web" {
		t.Fatalf("expected web, got %s", filtered[0].Name)
	}
}

func TestFilteredItems_BySummary(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "web", Summary: "Replicas at max capacity"},
		{Name: "api", Summary: "Running normally"},
	}
	m.filter = "max capacity"
	filtered := m.filteredItems()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 item matching summary, got %d", len(filtered))
	}
	if filtered[0].Name != "web" {
		t.Fatalf("expected web, got %s", filtered[0].Name)
	}
}

func TestIntervalUp_DecreasesInterval(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 10 * time.Second})
	if m.interval != 10*time.Second {
		t.Fatalf("expected initial interval 10s, got %v", m.interval)
	}

	// Press + to decrease interval (faster refresh).
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m2 := updated.(Model)
	// 10s - max(10s/2, 1s) = 10s - 5s = 5s
	if m2.interval != 5*time.Second {
		t.Fatalf("expected interval 5s after +, got %v", m2.interval)
	}

	// Press + again.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m3 := updated2.(Model)
	// 5s - max(5s/2, 1s) = 5s - 2.5s = 2.5s
	if m3.interval != 2500*time.Millisecond {
		t.Fatalf("expected interval 2.5s after second +, got %v", m3.interval)
	}
}

func TestIntervalUp_EqualsSign(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 10 * time.Second})

	// Press = (same as + for IntervalUp).
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'='}})
	m2 := updated.(Model)
	if m2.interval != 5*time.Second {
		t.Fatalf("expected interval 5s after =, got %v", m2.interval)
	}
}

func TestIntervalUp_FloorOneSecond(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 2 * time.Second})

	// Press +: 2s - max(2s/2, 1s) = 2s - 1s = 1s
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m2 := updated.(Model)
	if m2.interval != 1*time.Second {
		t.Fatalf("expected interval 1s, got %v", m2.interval)
	}

	// Press + again: step = max(1s/2, 1s) = 1s, but 1s - 1s = 0s -> floor 1s
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m3 := updated2.(Model)
	if m3.interval != 1*time.Second {
		t.Fatalf("expected interval clamped at 1s, got %v", m3.interval)
	}
}

func TestIntervalDown_IncreasesInterval(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 10 * time.Second})

	// Press - to increase interval (slower refresh).
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m2 := updated.(Model)
	// 10s + max(10s/2, 1s) = 10s + 5s = 15s
	if m2.interval != 15*time.Second {
		t.Fatalf("expected interval 15s after -, got %v", m2.interval)
	}
}

func TestIntervalDown_CeilingSixtySeconds(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 50 * time.Second})

	// Press -: 50s + max(50s/2, 1s) = 50s + 25s = 75s -> ceiling 60s
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m2 := updated.(Model)
	if m2.interval != 60*time.Second {
		t.Fatalf("expected interval clamped at 60s, got %v", m2.interval)
	}

	// Press - again while at ceiling: step = 30s, 60s + 30s = 90s -> ceiling 60s
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m3 := updated2.(Model)
	if m3.interval != 60*time.Second {
		t.Fatalf("expected interval still 60s at ceiling, got %v", m3.interval)
	}
}

func TestIntervalDown_MinimumStep(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 1 * time.Second})

	// Press -: step = max(1s/2, 1s) = 1s, 1s + 1s = 2s
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m2 := updated.(Model)
	if m2.interval != 2*time.Second {
		t.Fatalf("expected interval 2s after - from 1s, got %v", m2.interval)
	}
}
