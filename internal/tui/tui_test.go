package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"

	tea "charm.land/bubbletea/v2"
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
	_, cmd := m.Update(tea.KeyPressMsg{Text: "q"})
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
}

func TestUpdate_PauseToggle(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	if m.paused {
		t.Fatal("expected not paused initially")
	}
	updated, _ := m.Update(tea.KeyPressMsg{Text: "p"})
	m2 := updated.(Model)
	if !m2.paused {
		t.Fatal("expected paused after pressing p")
	}
	updated2, _ := m2.Update(tea.KeyPressMsg{Text: "p"})
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m2 := updated.(Model)
	if m2.cursor != 1 {
		t.Fatalf("expected cursor at 1, got %d", m2.cursor)
	}

	updated2, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := updated2.(Model)
	if m3.viewMode != detailView {
		t.Fatal("expected detailView after enter")
	}

	// Escape goes back to list.
	updated3, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
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
	updated, _ := m.Update(tea.KeyPressMsg{Text: "?"})
	m2 := updated.(Model)
	if m2.viewMode != helpView {
		t.Fatal("expected helpView after pressing ?")
	}

	// Press ? again to close help.
	updated2, _ := m2.Update(tea.KeyPressMsg{Text: "?"})
	m3 := updated2.(Model)
	if m3.viewMode != listView {
		t.Fatal("expected listView after pressing ? again")
	}
}

func TestHelpView_IncludesWorkflowGuidance(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40

	help := m.renderHelpView()
	for _, want := range []string{
		"Daily triage",
		"Detail drill-down",
		"Selection and batch work",
		"Export workflow",
		"--suggest --export yaml",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("expected help view to include %q, got:\n%s", want, help)
		}
	}
}

func TestBatchApplyKeyRequiresSecondConfirmation(t *testing.T) {
	applied := 0
	calls := 0
	m := NewModel(nil, "default", Options{
		ApplyFn: func(_ context.Context, _, _ string, suggestions []hpaanalysis.Suggestion) error {
			calls++
			applied += len(suggestions)
			return nil
		},
	})
	m.width = 120
	m.height = 40
	m.loading = false
	m.items = []hpaanalysis.ListItem{{Namespace: "default", Name: "web"}}
	m.selected = map[string]bool{"default/web": true}
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{
			Namespace: "default",
			Name:      "web",
			Suggestions: []hpaanalysis.Suggestion{
				{
					Title: "Raise maxReplicas",
					Patch: `{"spec":{"maxReplicas":20}}`,
					Apply: true,
				},
				{
					Title: "Set stabilization",
					Patch: `{"spec":{"behavior":{"scaleDown":{"stabilizationWindowSeconds":300}}}}`,
					Apply: true,
				},
			},
		}},
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Text: "x"})
	m2 := updated.(Model)
	if cmd != nil {
		t.Fatal("first x should only preview")
	}
	if !m2.batchApplyConfirm || applied != 0 {
		t.Fatalf("expected preview confirmation without apply, confirm=%v applied=%d", m2.batchApplyConfirm, applied)
	}
	if !strings.Contains(m2.View().Content, "Batch apply preview") {
		t.Fatalf("expected preview in list view, got:\n%s", m2.View().Content)
	}
	cancelled, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cancelled.(Model).batchApplyConfirm {
		t.Fatal("Escape must cancel an armed batch apply")
	}

	_, cmd = m2.Update(tea.KeyPressMsg{Text: "x"})
	if cmd == nil {
		t.Fatal("second x should run apply command")
	}
	msg := cmd()
	if _, ok := msg.(applyResultMsg); !ok {
		t.Fatalf("expected applyResultMsg, got %T", msg)
	}
	if calls != 1 || applied != 2 {
		t.Fatalf("expected one grouped call with two suggestions, calls=%d suggestions=%d", calls, applied)
	}
}

func TestHelpView_EscapeClose(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.items = []hpaanalysis.ListItem{{Name: "web"}}
	m.viewMode = helpView

	// Press Esc to close help.
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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
	updated, _ := m.Update(tea.KeyPressMsg{Text: "S"})
	m2 := updated.(Model)
	if m2.sortField != "health-score" {
		t.Fatalf("expected sortField health-score, got %q", m2.sortField)
	}

	// Second S press: cycles from health-score -> issue.
	updated2, _ := m2.Update(tea.KeyPressMsg{Text: "S"})
	m3 := updated2.(Model)
	if m3.sortField != "issue" {
		t.Fatalf("expected sortField issue, got %q", m3.sortField)
	}

	// Third S press: cycles from issue -> namespace.
	updated3, _ := m3.Update(tea.KeyPressMsg{Text: "S"})
	m4 := updated3.(Model)
	if m4.sortField != "namespace" {
		t.Fatalf("expected sortField namespace, got %q", m4.sortField)
	}

	// Fourth S press: cycles from namespace -> name.
	updated4, _ := m4.Update(tea.KeyPressMsg{Text: "S"})
	m5 := updated4.(Model)
	if m5.sortField != "name" {
		t.Fatalf("expected sortField name, got %q", m5.sortField)
	}

	// Fifth S press: cycles from name -> health-score (wraps around).
	updated5, _ := m5.Update(tea.KeyPressMsg{Text: "S"})
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
	updated, _ := m.Update(tea.KeyPressMsg{Text: "g"})
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
	updated, _ := m.Update(tea.KeyPressMsg{Text: "g"})
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
	updated, _ := m.Update(tea.KeyPressMsg{Text: "+"})
	m2 := updated.(Model)
	// 10s - max(10s/2, 1s) = 10s - 5s = 5s
	if m2.interval != 5*time.Second {
		t.Fatalf("expected interval 5s after +, got %v", m2.interval)
	}

	// Press + again.
	updated2, _ := m2.Update(tea.KeyPressMsg{Text: "+"})
	m3 := updated2.(Model)
	// 5s - max(5s/2, 1s) = 5s - 2.5s = 2.5s
	if m3.interval != 2500*time.Millisecond {
		t.Fatalf("expected interval 2.5s after second +, got %v", m3.interval)
	}
}

func TestIntervalUp_EqualsSign(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 10 * time.Second})

	// Press = (same as + for IntervalUp).
	updated, _ := m.Update(tea.KeyPressMsg{Text: "="})
	m2 := updated.(Model)
	if m2.interval != 5*time.Second {
		t.Fatalf("expected interval 5s after =, got %v", m2.interval)
	}
}

func TestIntervalUp_FloorOneSecond(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 2 * time.Second})

	// Press +: 2s - max(2s/2, 1s) = 2s - 1s = 1s
	updated, _ := m.Update(tea.KeyPressMsg{Text: "+"})
	m2 := updated.(Model)
	if m2.interval != 1*time.Second {
		t.Fatalf("expected interval 1s, got %v", m2.interval)
	}

	// Press + again: step = max(1s/2, 1s) = 1s, but 1s - 1s = 0s -> floor 1s
	updated2, _ := m2.Update(tea.KeyPressMsg{Text: "+"})
	m3 := updated2.(Model)
	if m3.interval != 1*time.Second {
		t.Fatalf("expected interval clamped at 1s, got %v", m3.interval)
	}
}

func TestIntervalDown_IncreasesInterval(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 10 * time.Second})

	// Press - to increase interval (slower refresh).
	updated, _ := m.Update(tea.KeyPressMsg{Text: "-"})
	m2 := updated.(Model)
	// 10s + max(10s/2, 1s) = 10s + 5s = 15s
	if m2.interval != 15*time.Second {
		t.Fatalf("expected interval 15s after -, got %v", m2.interval)
	}
}

func TestIntervalDown_CeilingSixtySeconds(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 50 * time.Second})

	// Press -: 50s + max(50s/2, 1s) = 50s + 25s = 75s -> ceiling 60s
	updated, _ := m.Update(tea.KeyPressMsg{Text: "-"})
	m2 := updated.(Model)
	if m2.interval != 60*time.Second {
		t.Fatalf("expected interval clamped at 60s, got %v", m2.interval)
	}

	// Press - again while at ceiling: step = 30s, 60s + 30s = 90s -> ceiling 60s
	updated2, _ := m2.Update(tea.KeyPressMsg{Text: "-"})
	m3 := updated2.(Model)
	if m3.interval != 60*time.Second {
		t.Fatalf("expected interval still 60s at ceiling, got %v", m3.interval)
	}
}

func TestIntervalDown_MinimumStep(t *testing.T) {
	m := NewModel(nil, "default", Options{Interval: 1 * time.Second})

	// Press -: step = max(1s/2, 1s) = 1s, 1s + 1s = 2s
	updated, _ := m.Update(tea.KeyPressMsg{Text: "-"})
	m2 := updated.(Model)
	if m2.interval != 2*time.Second {
		t.Fatalf("expected interval 2s after - from 1s, got %v", m2.interval)
	}
}

// --- View rendering tests ---

func TestView_LoadingNoWidth(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	output := m.View().Content
	if output != "Loading..." {
		t.Fatalf("expected 'Loading...' for zero-width, got %q", output)
	}
}

func TestView_LoadingWithWidth(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	// loading is true, items empty
	output := m.View().Content
	if output != "Loading HPA data..." {
		t.Fatalf("expected 'Loading HPA data...', got %q", output)
	}
}

func TestView_Error(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.err = fmt.Errorf("connection refused")
	output := m.View().Content
	if !containsSubstring(output, "connection refused") {
		t.Fatalf("expected error message in output, got %q", output)
	}
}

func TestView_ListView(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK", HealthScore: 100},
	}
	output := m.View().Content
	if !containsSubstring(output, "web") {
		t.Fatalf("expected 'web' in list view, got %q", output)
	}
	if !containsSubstring(output, "LIVE") {
		t.Fatalf("expected LIVE status bar, got %q", output)
	}
}

func TestView_ListViewEmpty(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	output := m.View().Content
	if !containsSubstring(output, "No HPAs found") {
		t.Fatalf("expected 'No HPAs found', got %q", output)
	}
}

func TestView_ListViewEmptyWithFilter(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.filter = "nonexistent"
	output := m.View().Content
	if !containsSubstring(output, "No HPAs matching filter") {
		t.Fatalf("expected filter empty message, got %q", output)
	}
}

func TestView_DetailView(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.viewMode = detailView
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK", HealthScore: 90,
			Target: "Deployment/web", Current: 3, Desired: 5, Min: 1, Max: 10,
			Summary: "Scaling up"},
	}
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{
			Name: "web", Namespace: "default", Target: "Deployment/web",
			Health: "OK", HealthScore: 90, Current: 3, Desired: 5, Min: 1, Max: 10,
			Summary: "Scaling up",
		}},
	}
	output := m.View().Content
	if !containsSubstring(output, "HPA default/web") {
		t.Fatalf("expected HPA header, got %q", output)
	}
	if !containsSubstring(output, "Scaling up") {
		t.Fatalf("expected summary, got %q", output)
	}
}

func TestView_DetailViewWithKEDAInfo(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.viewMode = detailView
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "keda-worker", Health: "OK", HealthScore: 100,
			Target: "Deployment/keda-worker", Current: 3, Desired: 3, Min: 1, Max: 10,
			Summary: "OK"},
	}
	polling := int32(30)
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/keda-worker": {Analysis: hpaanalysis.Analysis{
			Name: "keda-worker", Namespace: "default",
			KEDAInfo: &hpakeda.Analysis{
				ScaledObjectName: "worker-so",
				Triggers: []hpakeda.TriggerSummary{
					{Type: "prometheus", Name: "http-rate", Status: "Active", MetricName: "http_requests", Threshold: "100", CurrentValue: "250"},
				},
				PollingInterval: &polling,
			},
		}},
	}
	output := m.View().Content
	if !containsSubstring(output, "KEDA") {
		t.Fatalf("expected KEDA section, got output:\n%s", output)
	}
	if !containsSubstring(output, "worker-so") {
		t.Fatalf("expected ScaledObject name, got output:\n%s", output)
	}
}

func TestView_DetailViewWithVPAConflict(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.viewMode = detailView
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "WARN", HealthScore: 80,
			Target: "Deployment/web", Current: 3, Desired: 3, Min: 1, Max: 10,
			Summary: "OK"},
	}
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{
			Name: "web", Namespace: "default",
			VPAConflict: &hpavpa.ConflictInfo{
				VPAName:    "web-vpa",
				UpdateMode: "Auto",
				Recommendations: []hpavpa.Recommendation{
					{Container: "app", Resource: "cpu", Target: "500m"},
				},
			},
		}},
	}
	output := m.View().Content
	if !containsSubstring(output, "VPA") {
		t.Fatalf("expected VPA section, got output:\n%s", output)
	}
	if !containsSubstring(output, "web-vpa") {
		t.Fatalf("expected VPA name, got output:\n%s", output)
	}
}

func TestView_DetailViewWithConditions(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.viewMode = detailView
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK", HealthScore: 100,
			Target: "Deployment/web", Current: 3, Desired: 3, Min: 1, Max: 10,
			Summary: "OK"},
	}
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{
			Name: "web", Namespace: "default",
			Conditions: []hpaanalysis.Condition{
				{Type: "ScalingActive", Status: "True", Reason: "ValidMetricFound"},
				{Type: "AbleToScale", Status: "True", Reason: "ReadyForNewScale"},
			},
		}},
	}
	output := m.View().Content
	if !containsSubstring(output, "Conditions") {
		t.Fatalf("expected Conditions section, got output:\n%s", output)
	}
}

func TestView_DetailViewWithMetrics(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.viewMode = detailView
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK", HealthScore: 100,
			Target: "Deployment/web", Current: 3, Desired: 3, Min: 1, Max: 10,
			Summary: "OK"},
	}
	ratio := 0.85
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{
			Name: "web", Namespace: "default",
			Metrics: []hpaanalysis.Metric{
				{Name: "cpu", Type: "Resource", Current: "85%", Target: "80%", Ratio: &ratio},
			},
		}},
	}
	output := m.View().Content
	if !containsSubstring(output, "Metrics") {
		t.Fatalf("expected Metrics section, got output:\n%s", output)
	}
	if !containsSubstring(output, "cpu") {
		t.Fatalf("expected cpu metric, got output:\n%s", output)
	}
}

func TestView_DetailViewWithInterpretation(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.viewMode = detailView
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK", HealthScore: 100,
			Target: "Deployment/web", Current: 3, Desired: 3, Min: 1, Max: 10,
			Summary: "OK"},
	}
	lines := make([]string, 8)
	for i := range lines {
		lines[i] = fmt.Sprintf("Interpretation line %d", i)
	}
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{
			Name:           "web",
			Namespace:      "default",
			Interpretation: lines,
		}},
	}
	output := m.View().Content
	if !containsSubstring(output, "Interpretation") {
		t.Fatalf("expected Interpretation section, got output:\n%s", output)
	}
	if !containsSubstring(output, "... and 3 more") {
		t.Fatalf("expected truncation at 5 lines, got output:\n%s", output)
	}
}

func TestView_MetricsView(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.viewMode = metricsView
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK", HealthScore: 100,
			Target: "Deployment/web", Current: 3, Desired: 3, Min: 1, Max: 10,
			Summary: "OK"},
	}
	ratio := 1.2
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{
			Name: "web", Namespace: "default",
			Metrics: []hpaanalysis.Metric{
				{Name: "cpu", Type: "Resource", Current: "120%", Target: "80%", Ratio: &ratio, Note: "within tolerance"},
			},
		}},
	}
	output := m.View().Content
	if !containsSubstring(output, "Metrics Diagnostics") {
		t.Fatalf("expected Metrics Diagnostics header, got output:\n%s", output)
	}
	if !containsSubstring(output, "above target") {
		t.Fatalf("expected 'above target' for ratio 1.2, got output:\n%s", output)
	}
}

func TestView_MetricsViewBelowTarget(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.viewMode = metricsView
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK", HealthScore: 100,
			Target: "Deployment/web"},
	}
	ratio := 0.5
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{
			Name:      "web",
			Namespace: "default",
			Metrics: []hpaanalysis.Metric{
				{Name: "cpu", Type: "Resource", Current: "50%", Target: "80%", Ratio: &ratio},
			},
		}},
	}
	output := m.View().Content
	if !containsSubstring(output, "below target") {
		t.Fatalf("expected 'below target' for ratio 0.5, got output:\n%s", output)
	}
}

func TestView_HelpView(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.viewMode = helpView
	m.items = []hpaanalysis.ListItem{{Name: "web"}}
	output := m.View().Content
	if !containsSubstring(output, "TUI Help") {
		t.Fatalf("expected TUI Help header, got output:\n%s", output)
	}
	if !containsSubstring(output, "Quit") {
		t.Fatalf("expected Quit binding, got output:\n%s", output)
	}
	if !containsSubstring(output, "Export workflow") {
		t.Fatalf("expected Export workflow guidance, got output:\n%s", output)
	}
}

func TestView_PausedStatusBar(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.paused = true
	m.items = []hpaanalysis.ListItem{{Name: "web"}}
	output := m.View().Content
	if !containsSubstring(output, "PAUSED") {
		t.Fatalf("expected PAUSED status, got output:\n%s", output)
	}
}

func TestView_FilterStatusBar(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.filter = "web"
	m.items = []hpaanalysis.ListItem{{Name: "web"}}
	output := m.View().Content
	if !containsSubstring(output, "filter: web") {
		t.Fatalf("expected filter in status bar, got output:\n%s", output)
	}
}

func TestView_SelectedStatusBar(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.selected = map[string]bool{"default/web": true}
	m.items = []hpaanalysis.ListItem{{Namespace: "default", Name: "web"}}
	output := m.View().Content
	if !containsSubstring(output, "selected: 1") {
		t.Fatalf("expected selected count in status bar, got output:\n%s", output)
	}
}

func TestView_SortFieldStatusBar(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.sortField = "name"
	m.items = []hpaanalysis.ListItem{{Name: "web"}}
	output := m.View().Content
	if !containsSubstring(output, "sort:name") {
		t.Fatalf("expected sort field in status bar, got output:\n%s", output)
	}
}

func TestView_FilteringStatusBar(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.loading = false
	m.filtering = true
	m.filterInput.Focus()
	m.items = []hpaanalysis.ListItem{{Name: "web"}}
	output := m.View().Content
	// When filtering, the status bar shows the filter input prompt (the
	// textinput renders a ">" prompt). bubble tea v2's textinput only shows
	// the placeholder once focused, which the activation path does, so we
	// assert on the prompt marker rather than the placeholder text.
	if !containsSubstring(output, ">") {
		t.Fatalf("expected filter input prompt in status bar, got output:\n%s", output)
	}
}

// --- Filter input tests ---

func TestHandleFilterInput_Enter(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.filtering = true
	m.filterInput.SetValue("test-filter")
	// Simulate the Update dispatch: when filtering, key messages go to handleFilterInput.
	// We use the main Update path to press /, then type, then enter.
	// First activate filter mode
	updated, _ := m.Update(tea.KeyPressMsg{Text: "/"})
	m2 := updated.(Model)
	if !m2.filtering {
		t.Fatal("expected filtering to be true after /")
	}
	// Type into filter
	m2.filterInput.SetValue("test-filter")
	// Press enter through Update (which dispatches to handleFilterInput since filtering is true)
	updated2, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := updated2.(Model)
	if m3.filtering {
		t.Fatal("expected filtering to be false after enter")
	}
	if m3.filter != "test-filter" {
		t.Fatalf("expected filter to be 'test-filter', got %q", m3.filter)
	}
}

func TestHandleFilterInput_Escape(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.filtering = true
	m.filterInput.SetValue("test")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m2 := updated.(Model)
	if m2.filtering {
		t.Fatal("expected filtering to be false after escape")
	}
}

func TestUpdate_FilterActivation(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.items = []hpaanalysis.ListItem{{Name: "web"}}
	updated, _ := m.Update(tea.KeyPressMsg{Text: "/"})
	m2 := updated.(Model)
	if !m2.filtering {
		t.Fatal("expected filtering to be true after pressing /")
	}
}

// --- Selection tests ---

func TestUpdate_ToggleSelect(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web"},
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m2 := updated.(Model)
	if !m2.selected["default/web"] {
		t.Fatal("expected web to be selected after space")
	}
	// Toggle again
	updated2, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m3 := updated2.(Model)
	if m3.selected["default/web"] {
		t.Fatal("expected web to be deselected after second space")
	}
}

func TestUpdate_SelectAll(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web"},
		{Namespace: "default", Name: "api"},
	}
	updated, _ := m.Update(tea.KeyPressMsg{Text: "a"})
	m2 := updated.(Model)
	if !m2.selected["default/web"] || !m2.selected["default/api"] {
		t.Fatal("expected all items selected after 'a'")
	}
}

func TestUpdate_DeselectAll(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web"},
	}
	m.selected = map[string]bool{"default/web": true}
	// Press 'A' (shift+a) for deselect all
	updated, _ := m.Update(tea.KeyPressMsg{Text: "A"})
	m2 := updated.(Model)
	if len(m2.selected) != 0 {
		t.Fatal("expected all items deselected after 'A'")
	}
}

func TestUpdate_SimulateKeyFromListViewDoesNothing(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web"},
	}
	m.selected = map[string]bool{"default/web": true}
	updated, _ := m.Update(tea.KeyPressMsg{Text: "s"})
	m2 := updated.(Model)
	if m2.err != nil {
		t.Fatalf("expected no error from simulation shortcut in list view, got %v", m2.err)
	}
	if m2.viewMode != listView {
		t.Fatalf("expected list view to remain active, got %d", m2.viewMode)
	}
}

func TestUpdate_MetricsKeyFromListView(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web"},
	}
	updated, _ := m.Update(tea.KeyPressMsg{Text: "m"})
	m2 := updated.(Model)
	if m2.viewMode != metricsView {
		t.Fatalf("expected metricsView, got %d", m2.viewMode)
	}
}

func TestUpdate_RefreshKey(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.width = 120
	m.height = 40
	m.items = []hpaanalysis.ListItem{{Name: "web"}}
	updated, _ := m.Update(tea.KeyPressMsg{Text: "r"})
	m2 := updated.(Model)
	if !m2.loading {
		t.Fatal("expected loading after refresh key")
	}
}

// --- Helper function tests ---

func TestHealthStyle(_ *testing.T) {
	tests := []struct {
		health string
		name   string
	}{
		{"OK", "ok"},
		{"ERROR", "error"},
		{"WARN", "warn"},
	}
	for _, tt := range tests {
		// healthStyle should not panic for any health string
		_ = healthStyle(tt.health).String()
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello w…"},
		{"short", 5, "short"},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		input string
		width int
		want  string
	}{
		{"hi", 5, "hi   "},
		{"hello", 3, "hel"},
		{"abc", 3, "abc"},
	}
	for _, tt := range tests {
		got := padRight(tt.input, tt.width)
		if got != tt.want {
			t.Errorf("padRight(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.want)
		}
	}
}

func TestRenderScoreBar(t *testing.T) {
	tests := []struct {
		score int
	}{
		{100}, {80}, {75}, {49}, {0}, {-5}, {150},
	}
	for _, tt := range tests {
		bar := renderScoreBar(tt.score)
		if len(bar) == 0 {
			t.Errorf("renderScoreBar(%d) returned empty string", tt.score)
		}
	}
}

func TestRenderCountdownBar(t *testing.T) {
	tests := []struct {
		remaining int
		total     int
		empty     bool
	}{
		{5, 10, false},
		{0, 10, false},
		{10, 10, false},
		{5, 0, true},   // total <= 0 returns empty
		{-1, -1, true}, // total <= 0 returns empty
	}
	for _, tt := range tests {
		bar := renderCountdownBar(tt.remaining, tt.total)
		if tt.empty && bar != "" {
			t.Errorf("renderCountdownBar(%d, %d) expected empty, got %q", tt.remaining, tt.total, bar)
		}
		if !tt.empty && bar == "" {
			t.Errorf("renderCountdownBar(%d, %d) expected non-empty", tt.remaining, tt.total)
		}
	}
}

func TestTUITriggerStatusBadge(t *testing.T) {
	tests := []struct {
		status string
	}{
		{"Active"},
		{"Inactive"},
		{"Unknown"},
		{"SomethingElse"},
	}
	for _, tt := range tests {
		badge := tuiTriggerStatusBadge(tt.status)
		if badge == "" {
			t.Errorf("tuiTriggerStatusBadge(%q) returned empty string", tt.status)
		}
	}
}

func TestCmpInt(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{1, 2, -1},
		{2, 1, 1},
		{1, 1, 0},
	}
	for _, tt := range tests {
		got := cmpInt(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("cmpInt(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSortItems_Namespace(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Namespace: "prod", Name: "api"},
		{Namespace: "default", Name: "web"},
	}
	m.sortField = "namespace"
	m.sortDescending = false
	m.sortItems()
	if m.items[0].Namespace != "default" {
		t.Fatalf("expected default first, got %s", m.items[0].Namespace)
	}
}

func TestSortItems_Issue(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "api", Issue: "ScalingLimited"},
		{Namespace: "default", Name: "web", Issue: "OK"},
	}
	m.sortField = "issue"
	m.sortDescending = false
	m.sortItems()
	if m.items[0].Name != "web" {
		t.Fatalf("expected web (OK) first alphabetically, got %s", m.items[0].Name)
	}
}

func TestSortItems_EmptySortField(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Name: "beta"}, {Name: "alpha"},
	}
	m.sortField = ""
	m.sortItems()
	// Should not reorder
	if m.items[0].Name != "beta" {
		t.Fatal("expected no sort when sortField is empty")
	}
}

func TestUpdate_TickWhilePaused(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.paused = true
	updated, cmd := m.Update(tickMsg{})
	m2 := updated.(Model)
	if !m2.paused {
		t.Fatal("expected still paused")
	}
	if cmd == nil {
		t.Fatal("expected tick cmd to continue even when paused")
	}
}

func TestUpdate_TickWhileNotPaused(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.paused = false
	updated, cmd := m.Update(tickMsg{})
	_ = updated.(Model)
	if cmd == nil {
		t.Fatal("expected batch cmd on tick")
	}
}

func TestUpdate_FetchResultClampsCursor(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.cursor = 5
	updated, _ := m.Update(fetchResultMsg{
		items: []hpaanalysis.ListItem{
			{Name: "web"}, {Name: "api"},
		},
		reports: map[string]*hpaanalysis.StatusReport{},
	})
	m2 := updated.(Model)
	if m2.cursor != 1 {
		t.Fatalf("expected cursor clamped to 1 (last item), got %d", m2.cursor)
	}
}

func TestUpdate_FetchResultClampsCursorNegative(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.cursor = -1
	updated, _ := m.Update(fetchResultMsg{
		items:   []hpaanalysis.ListItem{},
		reports: map[string]*hpaanalysis.StatusReport{},
	})
	m2 := updated.(Model)
	if m2.cursor != 0 {
		t.Fatalf("expected cursor clamped to 0, got %d", m2.cursor)
	}
}

// helper to avoid importing strings
func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
