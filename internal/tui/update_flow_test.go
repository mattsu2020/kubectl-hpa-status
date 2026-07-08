package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
)

// pressTUIKey feeds a single key press through Update and returns the new model.
func pressTUIKey(t *testing.T, m Model, k string) (Model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyPressMsg
	switch k {
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEsc}
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	default:
		msg = tea.KeyPressMsg{Text: k, Code: rune(k[0])}
	}
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

// detailModel builds a model focused on one HPA in detail view with a report.
func detailModel(opts Options) Model {
	m := NewModel(nil, "default", opts)
	m.items = []hpaanalysis.ListItem{{Namespace: "default", Name: "web", Health: "OK"}}
	m.reports = map[string]*hpaanalysis.StatusReport{
		"default/web": {Analysis: hpaanalysis.Analysis{Name: "web", Namespace: "default"}},
	}
	m.viewMode = detailView
	m.width = 120
	m.height = 40
	return m
}

// --- async result messages -------------------------------------------------

func TestUpdate_SimResultMsg(t *testing.T) {
	m := detailModel(Options{})
	m.simState = &simState{}
	result := &hpaanalysis.SimulationResult{Parameter: "maxReplicas"}
	updated, _ := m.Update(simResultMsg{result: result, err: errors.New("boom")})
	m2 := updated.(Model)
	if m2.simState.result != result || m2.simState.err == nil {
		t.Fatal("simResultMsg not applied to simState")
	}

	// Nil state must not panic.
	m.simState = nil
	if updated, _ := m.Update(simResultMsg{}); updated == nil {
		t.Fatal("expected model back")
	}
}

func TestUpdate_ApplyResultMsg(t *testing.T) {
	m := detailModel(Options{})
	m.fixState = &fixState{}
	updated, _ := m.Update(applyResultMsg{title: "tune", err: errors.New("denied")})
	m2 := updated.(Model)
	if !m2.fixState.applied || m2.fixState.applyErr == nil {
		t.Fatal("applyResultMsg not applied to fixState")
	}
}

func TestUpdate_ReplayLoadedMsg(t *testing.T) {
	m := detailModel(Options{})
	m.replayState = &replayState{loading: true}
	trace := &hpaanalysis.TimelineTrace{HPAName: "web"}
	updated, _ := m.Update(replayLoadedMsg{trace: trace})
	m2 := updated.(Model)
	if m2.replayState.loading || m2.replayState.trace != trace {
		t.Fatal("replayLoadedMsg not applied to replayState")
	}
}

func TestUpdate_BatchAuditMsg(t *testing.T) {
	m := detailModel(Options{})
	m.batchAuditState = &batchAuditState{loading: true}
	reports := map[string]*audit.Report{
		"default/web": {Namespace: "default", Name: "web", Score: 80, Summary: "good"},
	}
	updated, _ := m.Update(batchAuditMsg{reports: reports})
	m2 := updated.(Model)
	if m2.batchAuditState.loading || len(m2.batchAuditState.results) != 1 {
		t.Fatal("batchAuditMsg not applied to batchAuditState")
	}

	// Error path keeps results empty.
	m.batchAuditState = &batchAuditState{loading: true}
	updated, _ = m.Update(batchAuditMsg{err: errors.New("rbac")})
	m2 = updated.(Model)
	if m2.batchAuditState.err == nil || len(m2.batchAuditState.results) != 0 {
		t.Fatal("batchAuditMsg error not recorded")
	}

	// Nil state is a no-op.
	m.batchAuditState = nil
	if updated, _ := m.Update(batchAuditMsg{}); updated == nil {
		t.Fatal("expected model back")
	}
}

// --- key flows ---------------------------------------------------------------

func TestKeyFlow_HistoryOpenAndClose(t *testing.T) {
	m := detailModel(Options{})
	m2, _ := pressTUIKey(t, m, "H")
	if m2.viewMode != historyView || m2.historyState == nil {
		t.Fatal("expected historyView after H in detail view")
	}
	m3, _ := pressTUIKey(t, m2, "esc")
	if m3.viewMode != detailView || m3.historyState != nil {
		t.Fatal("expected detailView and cleared history state after esc")
	}
}

func TestKeyFlow_HintsOpenAndClose(t *testing.T) {
	m := detailModel(Options{})
	// Without metric hints the key is a no-op.
	m2, _ := pressTUIKey(t, m, "h")
	if m2.viewMode != detailView {
		t.Fatal("expected no-op without metric hints")
	}

	m.reports["default/web"].Analysis.MetricHints = &hpaanalysis.MetricHintsReport{
		Hints: []hpaanalysis.MetricHint{
			{Pattern: "external-metric-missing", Severity: "warning", Title: "missing external metric", MetricType: "External", MetricName: "queue_depth"},
		},
	}
	m3, _ := pressTUIKey(t, m, "h")
	if m3.viewMode != hintsView || m3.hintsState == nil || len(m3.hintsState.flows) == 0 {
		t.Fatal("expected hintsView with flows after h")
	}
	m4, _ := pressTUIKey(t, m3, "esc")
	if m4.viewMode != detailView || m4.hintsState != nil {
		t.Fatal("expected detailView after esc from hints")
	}
}

func TestKeyFlow_OverviewOpenAndClose(t *testing.T) {
	m := detailModel(Options{})
	m.viewMode = listView
	m2, _ := pressTUIKey(t, m, "O")
	if m2.viewMode != overviewView {
		t.Fatal("expected overviewView after O in list view")
	}
	m3, _ := pressTUIKey(t, m2, "esc")
	if m3.viewMode != listView {
		t.Fatal("expected listView after esc from overview")
	}
}

func TestKeyFlow_SimulationLifecycle(t *testing.T) {
	m := detailModel(Options{})

	// Open the simulation panel.
	m2, _ := pressTUIKey(t, m, "s")
	if m2.viewMode != simView || m2.simState == nil {
		t.Fatal("expected simView with state after s")
	}
	if len(m2.simState.fields) != 2 {
		t.Fatalf("expected maxReplicas+minReplicas fields, got %d", len(m2.simState.fields))
	}

	// Type digits into the focused field; letters are rejected.
	m3, _ := pressTUIKey(t, m2, "1")
	m3, _ = pressTUIKey(t, m3, "0")
	if got := m3.simState.fields[0].Value; got != "10" {
		t.Fatalf("expected field value 10, got %q", got)
	}
	m3b, _ := pressTUIKey(t, m3, "z")
	if got := m3b.simState.fields[0].Value; got != "10" {
		t.Fatalf("letters must be ignored, got %q", got)
	}

	// Backspace deletes.
	m4, _ := pressTUIKey(t, m3, "backspace")
	if got := m4.simState.fields[0].Value; got != "1" {
		t.Fatalf("expected 1 after backspace, got %q", got)
	}

	// Tab cycles focus forward and wraps; shift+tab goes back and wraps.
	m5, _ := pressTUIKey(t, m4, "tab")
	if m5.simState.focusIndex != 1 {
		t.Fatalf("expected focusIndex 1 after tab, got %d", m5.simState.focusIndex)
	}
	m5, _ = pressTUIKey(t, m5, "tab")
	if m5.simState.focusIndex != 0 {
		t.Fatalf("expected wrap to 0, got %d", m5.simState.focusIndex)
	}
	m5, _ = pressTUIKey(t, m5, "shift+tab")
	if m5.simState.focusIndex != 1 {
		t.Fatalf("expected wrap back to last field, got %d", m5.simState.focusIndex)
	}

	// Toggle metric mode on and off.
	m6, _ := pressTUIKey(t, m5, "M")
	if !m6.simState.metricMode || !m6.simState.metricInput.Focused() {
		t.Fatal("expected metric mode with focused input after M")
	}
	// Typing while metric input is focused goes to the textinput.
	m6b, _ := pressTUIKey(t, m6, "c")
	if got := m6b.simState.metricInput.Value(); got != "c" {
		t.Fatalf("expected metric input to receive rune, got %q", got)
	}
	// Escape while the metric input is focused leaves metric mode but keeps
	// the simulation view open (handled by handleSimInput, not handleEscape).
	m7, _ := pressTUIKey(t, m6b, "esc")
	if m7.simState.metricMode || m7.viewMode != simView {
		t.Fatal("expected metric mode off and simView retained after esc")
	}

	// Enter runs the simulation (async command).
	_, cmd := pressTUIKey(t, m7, "enter")
	if cmd == nil {
		t.Fatal("expected simulation command from enter")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected simulation message")
	}

	// Escape returns to detail view and clears state.
	m8, _ := pressTUIKey(t, m7, "esc")
	if m8.viewMode != detailView || m8.simState != nil {
		t.Fatal("expected detailView and cleared sim state after esc")
	}
}

func TestKeyFlow_MetricSimulation(t *testing.T) {
	m := detailModel(Options{})
	m2, _ := pressTUIKey(t, m, "s")
	m3, _ := pressTUIKey(t, m2, "M")
	for _, r := range "cpu=80" {
		m3, _ = pressTUIKey(t, m3, string(r))
	}
	_, cmd := pressTUIKey(t, m3, "enter")
	if cmd == nil {
		t.Fatal("expected metric simulation command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected simulation result message")
	}
}

func TestKeyFlow_FixWizard(t *testing.T) {
	m := detailModel(Options{})

	// No suggestions: key sets an error and stays in detail view.
	m2, _ := pressTUIKey(t, m, "f")
	if m2.viewMode != detailView || m2.err == nil {
		t.Fatal("expected error for fix key without suggestions")
	}

	m.reports["default/web"].Analysis.Suggestions = []hpaanalysis.Suggestion{
		{Title: "patch it", Description: "apply patch", Patch: `{"spec":{"maxReplicas":10}}`},
		{Title: "run cmd", Description: "run kubectl", Command: "kubectl scale"},
		{Title: "advice", Description: "read docs"},
	}
	m3, _ := pressTUIKey(t, m, "f")
	if m3.viewMode != fixView || m3.fixState == nil {
		t.Fatal("expected fixView after f with suggestions")
	}

	// Dry run for each suggestion flavor. Note fixState is a shared pointer,
	// so set selected explicitly for each flavor instead of relying on copies.
	m3.fixState.selected = 0
	m5, _ := pressTUIKey(t, m3, "d")
	if !strings.Contains(m5.fixState.dryRunResult, "patch preview") {
		t.Fatalf("expected patch preview, got %q", m5.fixState.dryRunResult)
	}
	m5.fixState.selected = 1
	m5, _ = pressTUIKey(t, m5, "d")
	if !strings.Contains(m5.fixState.dryRunResult, "command preview") {
		t.Fatalf("expected command preview, got %q", m5.fixState.dryRunResult)
	}
	m5.fixState.selected = 2
	m5, _ = pressTUIKey(t, m5, "d")
	if !strings.Contains(m5.fixState.dryRunResult, "no patch or command") {
		t.Fatalf("expected fallback preview, got %q", m5.fixState.dryRunResult)
	}

	// Cursor moves through suggestions.
	m5.fixState.selected = 0
	m5b, _ := pressTUIKey(t, m5, "j")
	if m5b.fixState.selected != 1 {
		t.Fatalf("expected selected 1, got %d", m5b.fixState.selected)
	}

	// Enter without ApplyFn records an error instead of crashing.
	m6, cmd := pressTUIKey(t, m3, "enter")
	if cmd != nil {
		t.Fatal("expected no async cmd without ApplyFn")
	}
	_ = m6

	// Escape returns to detail view.
	m7, _ := pressTUIKey(t, m3, "esc")
	if m7.viewMode != detailView || m7.fixState != nil {
		t.Fatal("expected detailView after esc from fix view")
	}
}

func TestKeyFlow_FixApplyWithApplyFn(t *testing.T) {
	applied := false
	m := detailModel(Options{
		ApplyFn: func(_ context.Context, namespace, name, patch string) error {
			applied = true
			if namespace != "default" || name != "web" || patch == "" {
				t.Errorf("unexpected apply args: %s/%s %q", namespace, name, patch)
			}
			return nil
		},
	})
	m.reports["default/web"].Analysis.Suggestions = []hpaanalysis.Suggestion{
		{Title: "patch it", Patch: `{"spec":{"maxReplicas":10}}`},
	}
	m2, _ := pressTUIKey(t, m, "f")
	_, cmd := pressTUIKey(t, m2, "enter")
	if cmd == nil {
		t.Fatal("expected apply command")
	}
	msg := cmd()
	if _, ok := msg.(applyResultMsg); !ok {
		t.Fatalf("expected applyResultMsg, got %T", msg)
	}
	if !applied {
		t.Fatal("ApplyFn was not invoked")
	}
}

func TestKeyFlow_ReplayOpenScrollClose(t *testing.T) {
	m := detailModel(Options{})
	m2, cmd := pressTUIKey(t, m, "T")
	if m2.viewMode != replayView || m2.replayState == nil || cmd == nil {
		t.Fatal("expected replayView with load command after T")
	}
	// The default trace file does not exist; the load command reports an error.
	if msg, ok := cmd().(replayLoadedMsg); !ok || msg.err == nil {
		t.Fatal("expected replayLoadedMsg with error for missing file")
	}

	// Feed a loaded trace and scroll.
	m2.replayState.trace = &hpaanalysis.TimelineTrace{
		HPAName: "web",
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Current: 1, Desired: 2, Health: "OK"},
			{Current: 2, Desired: 2, Health: "OK"},
		},
	}
	m3, _ := pressTUIKey(t, m2, "j")
	if m3.replayState.scrollPos != 1 {
		t.Fatalf("expected scrollPos 1, got %d", m3.replayState.scrollPos)
	}
	m4, _ := pressTUIKey(t, m3, "esc")
	if m4.viewMode != detailView || m4.replayState != nil {
		t.Fatal("expected detailView after esc from replay")
	}
}

func TestKeyFlow_BatchAudit(t *testing.T) {
	// AuditFn missing: error recorded.
	m := detailModel(Options{})
	m.viewMode = listView
	m2, _ := pressTUIKey(t, m, "B")
	if m2.err == nil {
		t.Fatal("expected error without AuditFn")
	}

	auditFn := func(_ context.Context, namespace, name string) (*audit.Report, error) {
		return &audit.Report{Namespace: namespace, Name: name, Score: 90, Summary: "ok"}, nil
	}
	m = detailModel(Options{AuditFn: auditFn})
	m.viewMode = listView

	// No selection: error recorded.
	m3, _ := pressTUIKey(t, m, "B")
	if m3.err == nil {
		t.Fatal("expected error without selection")
	}

	// Select the row (space) then audit.
	m4, _ := pressTUIKey(t, m, " ")
	if !m4.selected["default/web"] {
		t.Fatal("expected row selected after space")
	}
	m5, cmd := pressTUIKey(t, m4, "B")
	if m5.viewMode != batchAuditView || cmd == nil {
		t.Fatal("expected batchAuditView with async command")
	}
	msg, ok := cmd().(batchAuditMsg)
	if !ok || len(msg.reports) != 1 {
		t.Fatalf("expected one audit report, got %#v", msg)
	}
	updated, _ := m5.Update(msg)
	m6 := updated.(Model)
	if len(m6.batchAuditState.results) != 1 {
		t.Fatal("expected one batch audit entry")
	}
	m7, _ := pressTUIKey(t, m6, "esc")
	if m7.viewMode != listView || m7.batchAuditState != nil {
		t.Fatal("expected listView after esc from batch audit")
	}
}

func TestKeyFlow_IntervalAdjust(t *testing.T) {
	m := detailModel(Options{Interval: 8 * time.Second})
	m.viewMode = listView

	faster, _ := pressTUIKey(t, m, "+")
	if faster.interval >= 8*time.Second {
		t.Fatalf("expected faster interval, got %v", faster.interval)
	}

	slower, _ := pressTUIKey(t, m, "-")
	if slower.interval <= 8*time.Second {
		t.Fatalf("expected slower interval, got %v", slower.interval)
	}

	// Clamps at 1s and 60s.
	m.interval = time.Second
	atMin, _ := pressTUIKey(t, m, "+")
	if atMin.interval != time.Second {
		t.Fatalf("expected clamp at 1s, got %v", atMin.interval)
	}
	m.interval = 60 * time.Second
	atMax, _ := pressTUIKey(t, m, "-")
	if atMax.interval != 60*time.Second {
		t.Fatalf("expected clamp at 60s, got %v", atMax.interval)
	}
}

func TestMoveCursor_HistoryAndHints(t *testing.T) {
	m := detailModel(Options{})

	m.viewMode = historyView
	m.historyState = &historyState{snapshots: []hpaanalysis.TimelineSnapshot{{}, {}, {}}}
	m2, _ := pressTUIKey(t, m, "j")
	if m2.historyState.scrollPos != 1 {
		t.Fatalf("expected history scrollPos 1, got %d", m2.historyState.scrollPos)
	}

	m.viewMode = hintsView
	m.hintsState = &hintsState{flows: []hpaanalysis.MetricHintTroubleshooting{{}, {}}}
	m3, _ := pressTUIKey(t, m, "j")
	if m3.hintsState.selected != 1 {
		t.Fatalf("expected hints selected 1, got %d", m3.hintsState.selected)
	}
}

// --- pure helpers ------------------------------------------------------------

func TestSplitNamespaceName(t *testing.T) {
	t.Parallel()
	if got := splitNamespaceName("ns/name"); len(got) != 2 || got[0] != "ns" || got[1] != "name" {
		t.Errorf("splitNamespaceName(ns/name) = %v", got)
	}
	if got := splitNamespaceName("bare"); len(got) == 2 {
		t.Errorf("expected no split for bare name, got %v", got)
	}
}

func TestBuildBatchAuditEntries_SortedByScore(t *testing.T) {
	t.Parallel()
	reports := map[string]*audit.Report{
		"a/high": {Namespace: "a", Name: "high", Score: 95, Summary: "fine"},
		"a/low": {Namespace: "a", Name: "low", Score: 20, Summary: "bad",
			Findings: []audit.Finding{
				{Severity: audit.AuditCritical},
				{Severity: audit.AuditWarning},
			}},
	}
	entries := buildBatchAuditEntries(reports)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "low" {
		t.Errorf("expected worst score first, got %q", entries[0].Name)
	}
	if entries[0].Critical != 1 || entries[0].Warnings != 1 || entries[0].Findings != 2 {
		t.Errorf("finding counts wrong: %+v", entries[0])
	}
}

func TestParseMetricInput(t *testing.T) {
	t.Parallel()
	got, err := parseMetricInput("cpu=80%, memory=4Gi")
	if err != nil {
		t.Fatalf("parseMetricInput: %v", err)
	}
	if got["cpu"] != "80%" || got["memory"] != "4Gi" {
		t.Errorf("parsed = %v", got)
	}

	if _, err := parseMetricInput("   "); err == nil {
		t.Error("expected error for empty input")
	}
	if _, err := parseMetricInput("novalue"); err == nil {
		t.Error("expected error for input without pairs")
	}
}

func TestBuildHPAFromAnalysis(t *testing.T) {
	t.Parallel()
	a := hpaanalysis.Analysis{Name: "web", Namespace: "default"}
	hpa := buildHPAFromAnalysis(a)
	if hpa == nil || hpa.Name != "web" || hpa.Namespace != "default" {
		t.Fatalf("buildHPAFromAnalysis = %+v", hpa)
	}
}

func TestMaxInt(t *testing.T) {
	t.Parallel()
	if maxInt(3, 5) != 5 || maxInt(5, 3) != 5 {
		t.Error("maxInt broken")
	}
}

func TestCurrentReport_OutOfRange(t *testing.T) {
	m := detailModel(Options{})
	m.cursor = 99
	if m.currentReport() != nil {
		t.Error("expected nil report for out-of-range cursor")
	}
}

// --- view rendering ----------------------------------------------------------

func TestView_SimView(t *testing.T) {
	m := detailModel(Options{})
	m2, _ := pressTUIKey(t, m, "s")
	out := m2.View().Content
	if !strings.Contains(out, "maxReplicas") || !strings.Contains(out, "minReplicas") {
		t.Errorf("expected sim fields in view, got:\n%s", out)
	}

	// Metric mode input rendering.
	m3, _ := pressTUIKey(t, m2, "M")
	if !strings.Contains(m3.View().Content, "metric") && !strings.Contains(strings.ToLower(m3.View().Content), "metric") {
		t.Error("expected metric input hint in metric mode view")
	}

	// Result rendering.
	m3.simState.result = &hpaanalysis.SimulationResult{
		Parameter:      "maxReplicas",
		OriginalValue:  "5",
		SimulatedValue: "10",
		Interpretation: []string{"scale ceiling doubled"},
	}
	if out := m3.View().Content; !strings.Contains(out, "maxReplicas") {
		t.Errorf("expected result parameter in view, got:\n%s", out)
	}
	m3.simState.err = errors.New("sim failed")
	if out := m3.View().Content; !strings.Contains(out, "sim failed") {
		t.Error("expected simulation error in view")
	}
}

func TestView_FixView(t *testing.T) {
	m := detailModel(Options{})
	m.viewMode = fixView
	m.fixState = &fixState{
		suggestions: []hpaanalysis.Suggestion{
			{Title: "tune window", Description: "add stabilization", Patch: `{"spec":{}}`, Risk: "low"},
			{Title: "cmd", Description: "run", Command: "kubectl patch"},
		},
		dryRunResult: "patch preview: {}",
	}
	out := m.View().Content
	for _, want := range []string{"tune window", "add stabilization", "patch preview"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in fix view", want)
		}
	}

	// Applied status rendering (success and error).
	m.fixState.applied = true
	if out := m.View().Content; out == "" {
		t.Error("expected non-empty applied view")
	}
	m.fixState.applyErr = errors.New("forbidden")
	if out := m.View().Content; !strings.Contains(out, "forbidden") {
		t.Error("expected apply error in fix view")
	}
}

func TestView_ReplayView(t *testing.T) {
	m := detailModel(Options{})
	m.viewMode = replayView

	// Loading state.
	m.replayState = &replayState{loading: true}
	if out := m.View().Content; out == "" {
		t.Error("expected loading view")
	}

	// Error state.
	m.replayState = &replayState{err: errors.New("no trace file")}
	if out := m.View().Content; !strings.Contains(out, "no trace file") {
		t.Error("expected error in replay view")
	}

	// Loaded trace with analysis and bottleneck.
	now := time.Date(2025, 1, 2, 3, 0, 0, 0, time.UTC)
	m.replayState = &replayState{
		trace: &hpaanalysis.TimelineTrace{
			HPAName:   "web",
			Namespace: "default",
			Snapshots: []hpaanalysis.TimelineSnapshot{
				{Timestamp: now, Current: 2, Desired: 2, Health: "OK", Summary: "steady"},
				{Timestamp: now.Add(time.Minute), Current: 2, Desired: 5, Health: "WARNING", Summary: "scaling",
					Conditions: []hpaanalysis.Condition{{Type: "AbleToScale", Status: "True"}}},
				{Timestamp: now.Add(2 * time.Minute), Current: 5, Desired: 5, Health: "OK", Summary: "done"},
			},
		},
		replayAnalysis: &hpaanalysis.ReplayAnalysis{
			Summary: "one scale-up detected",
			Bottlenecks: []hpaanalysis.BottleneckMarker{
				{Timestamp: now.Add(time.Minute), Type: "scheduling", Message: "pods pending", Severity: "high"},
			},
		},
	}
	out := m.View().Content
	// The analysis summary shows bottleneck counts, not the Summary text;
	// the bottleneck section shows individual messages.
	for _, want := range []string{"web", "Replay Analysis:", "1 HIGH", "pods pending"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in replay view, got:\n%s", want, firstN(out, 400))
		}
	}
}

func TestView_HistoryView(t *testing.T) {
	m := detailModel(Options{})
	m.viewMode = historyView

	// Empty history.
	m.historyState = &historyState{}
	if out := m.View().Content; !strings.Contains(out, "web") {
		t.Error("expected HPA name in empty history view")
	}

	// Populated snapshots derive churn analysis.
	now := time.Date(2025, 1, 2, 3, 0, 0, 0, time.UTC)
	var snapshots []hpaanalysis.TimelineSnapshot
	for i, desired := range []int32{2, 5, 2, 6, 2, 7} {
		snapshots = append(snapshots, hpaanalysis.TimelineSnapshot{
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			Current:   desired, Desired: desired,
			Health: "OK", HealthScore: 90, Summary: "s",
			Events: []hpaanalysis.Event{{Reason: "SuccessfulRescale", Message: "scaled"}},
		})
	}
	m.historyState = &historyState{snapshots: snapshots}
	out := m.View().Content
	if !strings.Contains(out, "web") {
		t.Errorf("expected header in history view, got:\n%s", firstN(out, 200))
	}
}

func TestView_OverviewView(t *testing.T) {
	m := detailModel(Options{})
	m.viewMode = overviewView
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK"},
		{Namespace: "default", Name: "api", Health: "ERROR"},
		{Namespace: "default", Name: "worker", Health: "WARNING"},
	}
	out := m.View().Content
	if out == "" {
		t.Fatal("expected non-empty overview")
	}
}

func TestView_BatchAuditView(t *testing.T) {
	m := detailModel(Options{})
	m.viewMode = batchAuditView

	m.batchAuditState = &batchAuditState{loading: true}
	if out := m.View().Content; out == "" {
		t.Error("expected loading batch audit view")
	}

	m.batchAuditState = &batchAuditState{
		results: []batchAuditEntry{
			{Namespace: "default", Name: "web", Score: 40, Findings: 3, Critical: 1, Warnings: 2, Summary: "needs work"},
		},
	}
	out := m.View().Content
	if !strings.Contains(out, "web") {
		t.Errorf("expected entry in batch audit view, got:\n%s", firstN(out, 200))
	}
}

func TestView_HintsView(t *testing.T) {
	m := detailModel(Options{})
	m.viewMode = hintsView
	m.hintsState = &hintsState{
		flows: []hpaanalysis.MetricHintTroubleshooting{
			{Pattern: "external-metric-missing", Severity: "warning", Title: "External metric missing",
				MetricType: "External", MetricName: "queue_depth",
				Steps: []hpaanalysis.MetricHintFix{{Description: "check adapter"}}},
		},
	}
	out := m.View().Content
	if !strings.Contains(out, "External metric missing") {
		t.Errorf("expected flow title in hints view, got:\n%s", firstN(out, 300))
	}
}

func TestWithContextAndInit(t *testing.T) {
	m := detailModel(Options{})
	ctx := context.Background()
	m2 := m.WithContext(ctx)
	if m2.ctx != ctx {
		t.Fatal("WithContext did not set context")
	}
	if cmd := m2.Init(); cmd == nil {
		t.Fatal("Init should return startup command")
	}
}

// firstN truncates s to at most n bytes for failure messages.
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
