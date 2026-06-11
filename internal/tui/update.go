package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Update handles all bubbletea messages.
// Value receivers are intentional here: Bubbletea's architecture uses an
// immutable model pattern where each message produces a new model state
// rather than mutating the existing one. All methods on Model (Update, View,
// Init, filteredItems) use value receivers for consistency with this pattern.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// If filter input is active, handle filter input keys.
		if m.filtering {
			return m.handleFilterInput(msg)
		}
		// If in simulation view and textinput is focused, delegate.
		if m.viewMode == simView && m.simState != nil && m.simState.metricMode && m.simState.metricInput.Focused() {
			return m.handleSimInput(msg)
		}
		if m.viewMode == simView && m.simState != nil && !m.simState.metricMode {
			if updated, cmd, handled := m.handleSimFieldInput(msg); handled {
				return updated, cmd
			}
		}
		return m.handleKey(msg)

	case tickMsg:
		if m.paused {
			return m, tickCmd(m.interval)
		}
		return m, tea.Batch(fetchHPAs(m), tickCmd(m.interval))

	case fetchResultMsg:
		m.loading = false
		m.lastRefresh = time.Now()
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.items = msg.items
		m.reports = msg.reports
		m.err = nil

		// Update replica history for sparklines.
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

		if m.sortField != "" {
			m.sortItems()
		}
		if !m.initialFocused {
			m.focusInitialItem()
			m.initialFocused = true
		}
		// Clamp cursor.
		filtered := m.filteredItems()
		if m.cursor >= len(filtered) {
			m.cursor = len(filtered) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, nil

	case simResultMsg:
		if m.simState != nil {
			m.simState.result = msg.result
			m.simState.err = msg.err
		}
		return m, nil

	case applyResultMsg:
		if m.fixState != nil {
			m.fixState.applied = true
			m.fixState.applyErr = msg.err
		}
		return m, nil

	case replayLoadedMsg:
		if m.replayState != nil {
			m.replayState.loading = false
			m.replayState.trace = msg.trace
			m.replayState.err = msg.err
		}
		return m, nil

	case batchAuditMsg:
		if m.batchAuditState != nil {
			m.batchAuditState.loading = false
			if msg.err != nil {
				m.batchAuditState.err = msg.err
			} else {
				m.batchAuditState.reports = msg.reports
				m.batchAuditState.results = buildBatchAuditEntries(msg.reports)
			}
		}
		return m, nil
	}

	return m, nil
}

//nolint:gocyclo // Key dispatch table: each case is a flat, independent key binding handler.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Up):
		switch m.viewMode {
		case listView:
			m.cursor--
			if m.cursor < 0 {
				m.cursor = 0
			}
		case fixView:
			if m.fixState != nil && m.fixState.selected > 0 {
				m.fixState.selected--
			}
		case replayView:
			if m.replayState != nil && m.replayState.scrollPos > 0 {
				m.replayState.scrollPos--
			}
		case historyView:
			if m.historyState != nil && m.historyState.scrollPos > 0 {
				m.historyState.scrollPos--
			}
		case hintsView:
			if m.hintsState != nil && m.hintsState.selected > 0 {
				m.hintsState.selected--
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		switch m.viewMode {
		case listView:
			filtered := m.filteredItems()
			m.cursor++
			if m.cursor >= len(filtered) {
				m.cursor = len(filtered) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
		case fixView:
			if m.fixState != nil && m.fixState.selected < len(m.fixState.suggestions)-1 {
				m.fixState.selected++
			}
		case replayView:
			if m.replayState != nil && m.replayState.trace != nil {
				maxScroll := len(m.replayState.trace.Snapshots) - 1
				if m.replayState.scrollPos < maxScroll {
					m.replayState.scrollPos++
				}
			}
		case historyView:
			if m.historyState != nil {
				maxScroll := len(m.historyState.snapshots) - 1
				if m.historyState.scrollPos < maxScroll {
					m.historyState.scrollPos++
				}
			}
		case hintsView:
			if m.hintsState != nil && m.hintsState.selected < len(m.hintsState.flows)-1 {
				m.hintsState.selected++
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		return m.handleEnter()

	case key.Matches(msg, m.keys.Escape):
		return m.handleEscape()

	case key.Matches(msg, m.keys.Refresh):
		m.loading = true
		return m, fetchHPAs(m)

	case key.Matches(msg, m.keys.Pause):
		m.paused = !m.paused
		return m, nil

	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		m.filterInput.Focus()
		return m, nil

	case key.Matches(msg, m.keys.Help):
		if m.viewMode == helpView {
			m.viewMode = listView
		} else {
			m.viewMode = helpView
		}
		return m, nil

	case key.Matches(msg, m.keys.Sort):
		sortCycle := []string{"name", "health-score", "issue", "namespace"}
		found := false
		for i, f := range sortCycle {
			if m.sortField == f {
				m.sortField = sortCycle[(i+1)%len(sortCycle)]
				found = true
				break
			}
		}
		if !found {
			m.sortField = "health-score"
		}
		m.sortDescending = !m.sortDescending
		m.sortItems()
		m.cursor = 0
		return m, nil

	case key.Matches(msg, m.keys.JumpProblem):
		filtered := m.filteredItems()
		for i, item := range filtered {
			if item.Health != "OK" {
				m.cursor = i
				break
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Metrics):
		if m.viewMode == detailView || m.viewMode == listView {
			m.viewMode = metricsView
		}
		return m, nil

	case key.Matches(msg, m.keys.ToggleSelect):
		if m.viewMode == listView {
			filtered := m.filteredItems()
			if m.cursor >= 0 && m.cursor < len(filtered) {
				k := filtered[m.cursor].Namespace + "/" + filtered[m.cursor].Name
				m.selected[k] = !m.selected[k]
				m.batchApplyConfirm = false
				m.batchApplyPreview = nil
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.SelectAll):
		if m.viewMode == listView {
			for _, item := range m.filteredItems() {
				m.selected[item.Namespace+"/"+item.Name] = true
			}
			m.batchApplyConfirm = false
			m.batchApplyPreview = nil
		}
		return m, nil

	case key.Matches(msg, m.keys.DeselectAll):
		if m.viewMode == listView {
			m.selected = map[string]bool{}
			m.batchApplyConfirm = false
			m.batchApplyPreview = nil
		}
		return m, nil

	case key.Matches(msg, m.keys.History):
		if m.viewMode == detailView {
			m.viewMode = historyView
			m.historyState = &historyState{}
		}
		return m, nil

	case key.Matches(msg, m.keys.Hints):
		if m.viewMode == detailView {
			filtered := m.filteredItems()
			if m.cursor >= 0 && m.cursor < len(filtered) {
				item := filtered[m.cursor]
				k := item.Namespace + "/" + item.Name
				if report, ok := m.reports[k]; ok && report.Analysis.MetricHints != nil {
					flows := hpaanalysis.BuildTroubleshootingFlows(report.Analysis.MetricHints.Hints)
					if len(flows) > 0 {
						m.hintsState = &hintsState{flows: flows}
						m.viewMode = hintsView
					}
				}
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Overview):
		if m.viewMode == listView {
			m.viewMode = overviewView
		}
		return m, nil

	case key.Matches(msg, m.keys.Simulate):
		return m.handleSimulateKey()

	case key.Matches(msg, m.keys.Fix):
		return m.handleFixKey()

	case key.Matches(msg, m.keys.Replay):
		return m.handleReplayKey()

	case key.Matches(msg, m.keys.BatchAudit):
		return m.handleBatchAuditKey()

	case key.Matches(msg, m.keys.BatchApply):
		return m.handleBatchApplyKey()

	case key.Matches(msg, m.keys.MetricMode):
		if m.viewMode == simView && m.simState != nil {
			m.simState.metricMode = !m.simState.metricMode
			if m.simState.metricMode {
				m.simState.metricInput.Focus()
			} else {
				m.simState.metricInput.Blur()
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.DryRun):
		if m.viewMode == fixView && m.fixState != nil && len(m.fixState.suggestions) > 0 {
			suggestion := m.fixState.suggestions[m.fixState.selected]
			switch {
			case suggestion.Patch != "":
				m.fixState.dryRunResult = "patch preview: " + suggestion.Patch
			case suggestion.Command != "":
				m.fixState.dryRunResult = "command preview: " + suggestion.Command
			default:
				m.fixState.dryRunResult = "no patch or command available for this suggestion"
			}
			m.fixState.applied = false
			m.fixState.applyErr = nil
		}
		return m, nil

	case key.Matches(msg, m.keys.TabField):
		if m.viewMode == simView && m.simState != nil && !m.simState.metricMode {
			m.simState.focusIndex++
			if m.simState.focusIndex >= len(m.simState.fields) {
				m.simState.focusIndex = 0
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.ShiftTabField):
		if m.viewMode == simView && m.simState != nil && !m.simState.metricMode {
			m.simState.focusIndex--
			if m.simState.focusIndex < 0 {
				m.simState.focusIndex = len(m.simState.fields) - 1
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.IntervalUp):
		step := m.interval / 2
		if step < time.Second {
			step = time.Second
		}
		newInterval := m.interval - step
		if newInterval < time.Second {
			newInterval = time.Second
		}
		m.interval = newInterval
		return m, nil

	case key.Matches(msg, m.keys.IntervalDown):
		step := m.interval / 2
		if step < time.Second {
			step = time.Second
		}
		newInterval := m.interval + step
		if newInterval > 60*time.Second {
			newInterval = 60 * time.Second
		}
		m.interval = newInterval
		return m, nil
	}

	return m, nil
}

func (m Model) handleSimFieldInput(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if m.simState == nil || len(m.simState.fields) == 0 {
		return m, nil, false
	}
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyCtrlH:
		field := &m.simState.fields[m.simState.focusIndex]
		if len(field.Value) > 0 {
			field.Value = field.Value[:len(field.Value)-1]
		}
		return m, nil, true
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return m, nil, false
		}
		changed := false
		for _, r := range msg.Runes {
			if (r >= '0' && r <= '9') || r == '-' || r == '.' {
				field := &m.simState.fields[m.simState.focusIndex]
				field.Value += string(r)
				changed = true
			}
		}
		return m, nil, changed
	}
	return m, nil, false
}

// handleEnter processes the Enter key based on the current view mode.
func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.viewMode {
	case listView:
		filtered := m.filteredItems()
		if m.cursor >= 0 && m.cursor < len(filtered) {
			m.viewMode = detailView
		}
		return m, nil

	case simView:
		if m.simState == nil {
			return m, nil
		}
		return m, m.runSimulation()

	case fixView:
		if m.fixState == nil || len(m.fixState.suggestions) == 0 {
			return m, nil
		}
		return m, m.applyFix()
	}

	return m, nil
}

// handleEscape processes the Escape key based on the current view mode.
func (m Model) handleEscape() (tea.Model, tea.Cmd) {
	switch m.viewMode {
	case helpView, detailView, metricsView:
		m.viewMode = listView
		m.batchApplyConfirm = false
		m.batchApplyPreview = nil
	case simView, fixView, replayView:
		m.simState = nil
		m.fixState = nil
		m.replayState = nil
		m.viewMode = detailView
	case historyView:
		m.historyState = nil
		m.viewMode = detailView
	case hintsView:
		m.hintsState = nil
		m.viewMode = detailView
	case batchAuditView:
		m.batchAuditState = nil
		m.viewMode = listView
	case overviewView:
		m.viewMode = listView
	default:
		return m, nil
	}
	return m, nil
}

// handleSimulateKey activates the simulation panel from detail view.
func (m Model) handleSimulateKey() (tea.Model, tea.Cmd) {
	if m.viewMode == detailView {
		return m, m.initSimState()
	}
	return m, nil
}

// handleFixKey activates the fix wizard from detail view.
func (m Model) handleFixKey() (tea.Model, tea.Cmd) {
	if m.viewMode != detailView {
		return m, nil
	}

	report := m.currentReport()
	if report == nil || len(report.Analysis.Suggestions) == 0 {
		m.err = fmt.Errorf("no suggestions available for this HPA")
		return m, nil
	}

	m.fixState = &fixState{
		suggestions: report.Analysis.Suggestions,
		selected:    0,
	}
	m.viewMode = fixView
	return m, nil
}

// handleReplayKey activates the replay viewer from detail view.
func (m Model) handleReplayKey() (tea.Model, tea.Cmd) {
	if m.viewMode != detailView {
		return m, nil
	}

	m.replayState = &replayState{
		loading:  true,
		filePath: "hpa-trace.json",
	}
	m.viewMode = replayView

	// Attempt to load the default trace file.
	return m, loadReplayTrace(m.replayState.filePath)
}

// initSimState creates simulation state from the currently selected HPA.
func (m Model) initSimState() tea.Cmd {
	filtered := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(filtered) {
		return nil
	}

	report := m.currentReport()
	if report == nil {
		return nil
	}

	// Get the original HPA from the report's analysis.
	hpa := buildHPAFromAnalysis(report.Analysis)

	fields := []simField{
		{Label: "maxReplicas", Path: "maxReplicas", Value: "", Original: fmt.Sprintf("%d", hpa.Spec.MaxReplicas)},
	}
	if hpa.Spec.MinReplicas != nil {
		fields = append(fields, simField{Label: "minReplicas", Path: "minReplicas", Value: "", Original: fmt.Sprintf("%d", *hpa.Spec.MinReplicas)})
	} else {
		fields = append(fields, simField{Label: "minReplicas", Path: "minReplicas", Value: "", Original: "1"})
	}

	mi := textinput.New()
	mi.Placeholder = "cpu=80%, memory=4Gi"
	mi.CharLimit = 100

	m.simState = &simState{
		hpa:         hpa,
		fields:      fields,
		metricInput: mi,
		focusIndex:  0,
	}
	m.viewMode = simView
	return nil
}

// runSimulation executes the simulation as a background tea.Cmd.
func (m Model) runSimulation() tea.Cmd {
	if m.simState == nil || m.simState.hpa == nil {
		return nil
	}

	if m.simState.metricMode {
		return m.runMetricSimulation()
	}
	return m.runParamSimulation()
}

// runParamSimulation runs a parameter-based simulation.
func (m Model) runParamSimulation() tea.Cmd {
	overrides := make(map[string]string)
	for _, field := range m.simState.fields {
		if field.Value != "" && field.Value != field.Original {
			overrides[field.Path] = field.Value
		}
	}

	if len(overrides) == 0 {
		return func() tea.Msg {
			return simResultMsg{err: fmt.Errorf("no parameters changed")}
		}
	}

	hpa := m.simState.hpa.DeepCopy()
	weights := m.opts.HealthWeights

	return func() tea.Msg {
		result, err := hpaanalysis.SimulateHPA(hpa, overrides, weights)
		return simResultMsg{result: result, err: err}
	}
}

// runMetricSimulation runs a metric value simulation.
func (m Model) runMetricSimulation() tea.Cmd {
	input := m.simState.metricInput.Value()
	if input == "" {
		return func() tea.Msg {
			return simResultMsg{err: fmt.Errorf("no metric override provided")}
		}
	}

	overrides, err := parseMetricInput(input)
	if err != nil {
		return func() tea.Msg {
			return simResultMsg{err: err}
		}
	}

	hpa := m.simState.hpa.DeepCopy()
	weights := m.opts.HealthWeights

	return func() tea.Msg {
		result, simErr := hpaanalysis.SimulateMetricChange(hpa, overrides, weights)
		return simResultMsg{result: result, err: simErr}
	}
}

// applyFix applies the currently selected fix suggestion.
func (m Model) applyFix() tea.Cmd {
	if m.fixState == nil || len(m.fixState.suggestions) == 0 {
		return nil
	}
	if m.opts.ApplyFn == nil {
		m.fixState.applyErr = fmt.Errorf("apply not available (no Kubernetes client)")
		return nil
	}

	suggestion := m.fixState.suggestions[m.fixState.selected]
	if suggestion.Patch == "" {
		m.fixState.applyErr = fmt.Errorf("no patch available for this suggestion")
		return nil
	}

	filtered := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(filtered) {
		return nil
	}

	namespace := filtered[m.cursor].Namespace
	name := filtered[m.cursor].Name
	patch := suggestion.Patch
	applyFn := m.opts.ApplyFn

	return func() tea.Msg {
		err := applyFn(context.Background(), namespace, name, patch)
		return applyResultMsg{title: suggestion.Title, err: err}
	}
}

// loadReplayTrace loads a timeline trace JSON file as a background tea.Cmd.
func loadReplayTrace(path string) tea.Cmd {
	return func() tea.Msg {
		trace, err := hpaanalysis.LoadTimelineTrace(path)
		return replayLoadedMsg{trace: trace, err: err}
	}
}

// handleSimInput handles text input when in simulation metric mode.
func (m Model) handleSimInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.simState == nil {
		return m, nil
	}

	switch msg.String() {
	case "enter":
		m.simState.metricInput.Blur()
		return m, m.runSimulation()
	case "esc":
		m.simState.metricInput.Blur()
		m.simState.metricMode = false
		return m, nil
	default:
		var cmd tea.Cmd
		m.simState.metricInput, cmd = m.simState.metricInput.Update(msg)
		return m, cmd
	}
}

// currentReport returns the report for the currently selected item.
func (m Model) currentReport() *hpaanalysis.StatusReport {
	filtered := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(filtered) {
		return nil
	}
	item := filtered[m.cursor]
	k := item.Namespace + "/" + item.Name
	return m.reports[k]
}

// buildHPAFromAnalysis creates a minimal HPA object from analysis data
// for use in simulation. The HPA will have correct spec fields but
// simplified status.
func buildHPAFromAnalysis(a hpaanalysis.Analysis) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: a.Namespace,
			Name:      a.Name,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: a.Max,
			MinReplicas: int32Ptr(a.Min),
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: a.Current,
			DesiredReplicas: a.Desired,
		},
	}
	return hpa
}

func int32Ptr(v int32) *int32 {
	return &v
}

// parseMetricInput parses a metric input string like "cpu=80%" into
// the format expected by SimulateMetricChange.
func parseMetricInput(input string) (map[string]string, error) {
	if input == "" {
		return nil, fmt.Errorf("empty metric input")
	}
	parts := splitPairs(input)
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid metric format; use name=value (e.g. cpu=80%%)")
	}
	return parts, nil
}

func splitPairs(input string) map[string]string {
	result := make(map[string]string)
	current := ""
	for _, ch := range input {
		if ch == ',' {
			pair := splitKeyValue(current)
			if pair != nil {
				result[pair[0]] = pair[1]
			}
			current = ""
		} else {
			current += string(ch)
		}
	}
	pair := splitKeyValue(current)
	if pair != nil {
		result[pair[0]] = pair[1]
	}
	return result
}

func splitKeyValue(s string) []string {
	s = trimSpaces(s)
	for i, ch := range s {
		if ch == '=' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return nil
}

func trimSpaces(s string) string {
	start := 0
	for start < len(s) && s[start] == ' ' {
		start++
	}
	end := len(s)
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

func (m Model) handleFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		m.filter = m.filterInput.Value()
		m.filterInput.Blur()
		m.cursor = 0
		return m, nil
	case "esc":
		m.filtering = false
		m.filterInput.Blur()
		return m, nil
	default:
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		return m, cmd
	}
}

// handleBatchAuditKey starts the batch auditor for all selected HPAs.
func (m Model) handleBatchAuditKey() (tea.Model, tea.Cmd) {
	if m.viewMode != listView {
		return m, nil
	}
	if m.opts.AuditFn == nil {
		m.err = fmt.Errorf("auditor not available")
		return m, nil
	}
	selected := m.selectedHPANames()
	if len(selected) == 0 {
		m.err = fmt.Errorf("no HPAs selected; use space to select, a to select all")
		return m, nil
	}

	m.batchAuditState = &batchAuditState{
		loading: true,
		reports: map[string]*hpaanalysis.AuditReport{},
	}
	m.viewMode = batchAuditView

	auditFn := m.opts.AuditFn
	namespace := m.namespace

	return m, func() tea.Msg {
		reports := make(map[string]*hpaanalysis.AuditReport)
		var lastErr error
		for _, name := range selected {
			ns := namespace
			if ns == "" {
				parts := splitNamespaceName(name)
				if len(parts) == 2 {
					ns = parts[0]
					name = parts[1]
				}
			}
			report, err := auditFn(context.Background(), ns, name)
			if err != nil {
				lastErr = err
				continue
			}
			reports[name] = report
		}
		return batchAuditMsg{reports: reports, err: lastErr}
	}
}

// handleBatchApplyKey runs batch suggest+apply for all selected HPAs.
func (m Model) handleBatchApplyKey() (tea.Model, tea.Cmd) {
	if m.viewMode != listView {
		return m, nil
	}
	if m.opts.ApplyFn == nil {
		m.err = fmt.Errorf("apply not available (no Kubernetes client)")
		return m, nil
	}
	selected := m.selectedHPANames()
	if len(selected) == 0 {
		m.err = fmt.Errorf("no HPAs selected; use space to select, a to select all")
		return m, nil
	}

	type patchEntry struct {
		namespace string
		name      string
		patch     string
		title     string
	}
	var patches []patchEntry
	for _, itemKey := range selected {
		report, ok := m.reports[itemKey]
		if !ok || report == nil {
			continue
		}
		for _, s := range report.Analysis.Suggestions {
			if s.Apply && s.Patch != "" {
				patches = append(patches, patchEntry{
					namespace: report.Analysis.Namespace,
					name:      report.Analysis.Name,
					patch:     s.Patch,
					title:     s.Title,
				})
			}
		}
	}

	if len(patches) == 0 {
		m.err = fmt.Errorf("no applicable patches found in %d selected HPA(s)", len(selected))
		return m, nil
	}

	if !m.batchApplyConfirm {
		m.batchApplyConfirm = true
		m.batchApplyPreview = make([]string, 0, len(patches))
		for _, p := range patches {
			m.batchApplyPreview = append(m.batchApplyPreview, fmt.Sprintf("%s/%s: %s", p.namespace, p.name, p.title))
		}
		return m, nil
	}

	applyFn := m.opts.ApplyFn
	m.batchApplyConfirm = false
	m.batchApplyPreview = nil
	return m, func() tea.Msg {
		var errs []string
		for _, p := range patches {
			if err := applyFn(context.Background(), p.namespace, p.name, p.patch); err != nil {
				errs = append(errs, fmt.Sprintf("%s/%s: %v", p.namespace, p.name, err))
			}
		}
		if len(errs) > 0 {
			return applyResultMsg{title: fmt.Sprintf("batch: %d/%d failed", len(errs), len(patches)), err: fmt.Errorf("%s", joinStrings(errs, "; "))}
		}
		return applyResultMsg{title: fmt.Sprintf("batch: %d patches applied", len(patches)), err: nil}
	}
}

// selectedHPANames returns the keys of selected HPAs.
func (m Model) selectedHPANames() []string {
	var names []string
	for k, v := range m.selected {
		if v {
			names = append(names, k)
		}
	}
	return names
}

// splitNamespaceName splits "namespace/name" into [namespace, name].
func splitNamespaceName(key string) []string {
	for i, ch := range key {
		if ch == '/' {
			return []string{key[:i], key[i+1:]}
		}
	}
	return []string{key}
}

// buildBatchAuditEntries converts audit reports into display entries.
func buildBatchAuditEntries(reports map[string]*hpaanalysis.AuditReport) []batchAuditEntry {
	entries := make([]batchAuditEntry, 0, len(reports))
	for _, report := range reports {
		critical := 0
		warnings := 0
		for _, f := range report.Findings {
			switch f.Severity {
			case hpaanalysis.AuditCritical:
				critical++
			case hpaanalysis.AuditWarning:
				warnings++
			}
		}
		entries = append(entries, batchAuditEntry{
			Namespace: report.Namespace,
			Name:      report.Name,
			Score:     report.Score,
			Findings:  len(report.Findings),
			Critical:  critical,
			Warnings:  warnings,
			Summary:   report.Summary,
		})
	}
	return entries
}

// joinStrings concatenates strings with a separator.
func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += sep + s
	}
	return result
}
