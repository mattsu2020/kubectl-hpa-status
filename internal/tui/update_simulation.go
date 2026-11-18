package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/retrospective"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/simulate"
)

// This file holds the simulation/replay/fix handlers and their helpers,
// split from update.go to keep each file focused. All types (simState,
// fixState, replayState, simField, etc.) live in model.go.

func (m Model) handleSimulateKey() (tea.Model, tea.Cmd) {
	if m.viewMode == detailView {
		newM, _ := m.initSimState()
		return newM, nil
	}
	return m, nil
}

// handleFixKey activates the fix wizard from detail view.
func (m Model) handleFixKey() (tea.Model, tea.Cmd) {
	if m.viewMode != detailView {
		return m, nil
	}

	report := m.currentReport()
	if report == nil || len(report.Analysis.Actions.Suggestions) == 0 {
		m.err = fmt.Errorf("no suggestions available for this HPA")
		return m, nil
	}

	m.fixEpoch++
	m.fixState = &fixState{
		namespace:   report.Analysis.Meta.Namespace,
		name:        report.Analysis.Meta.Name,
		uid:         m.hpaUIDs[report.Analysis.Meta.Namespace+"/"+report.Analysis.Meta.Name],
		suggestions: slices.Clone(report.Analysis.Actions.Suggestions),
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

// initSimState creates simulation state from the currently selected HPA and
// returns the updated model.
//
//nolint:unparam // tea.Cmd slot reserved for future focus/refresh commands
func (m Model) initSimState() (tea.Model, tea.Cmd) {
	filtered := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(filtered) {
		return m, nil
	}

	report := m.currentReport()
	if report == nil {
		return m, nil
	}

	hpa := m.hpas[report.Analysis.Meta.Namespace+"/"+report.Analysis.Meta.Name]
	if hpa == nil {
		m.err = fmt.Errorf("original HPA is unavailable; refresh before simulation")
		return m, nil
	}
	hpa = hpa.DeepCopy()

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
	return m, nil
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
	weights := simulateWeights(m.opts.HealthWeights)

	return func() tea.Msg {
		result, err := simulate.HPA(hpa, overrides, weights)
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
	weights := simulateWeights(m.opts.HealthWeights)

	return func() tea.Msg {
		result, simErr := simulate.MetricChange(hpa, overrides, weights)
		return simResultMsg{result: result, err: simErr}
	}
}

// applyFix applies the currently selected fix suggestion. The apply always
// targets the identity the wizard was opened for (fixState.namespace/name),
// never the current cursor row: a background refresh may have reordered the
// list between opening the wizard and confirming.
func (m Model) applyFix() tea.Cmd {
	if m.fixState == nil || len(m.fixState.suggestions) == 0 || m.fixState.applying {
		return nil
	}
	if m.opts.ApplyFn == nil {
		m.fixState.applyErr = fmt.Errorf("apply not available (no Kubernetes client)")
		return nil
	}
	if m.fixState.selected < 0 || m.fixState.selected >= len(m.fixState.suggestions) {
		m.fixState.applyErr = fmt.Errorf("no suggestion selected")
		return nil
	}

	suggestion := m.fixState.suggestions[m.fixState.selected]
	if suggestion.Patch == "" {
		m.fixState.applyErr = fmt.Errorf("no patch available for this suggestion")
		return nil
	}
	if !suggestion.Apply {
		m.fixState.applyErr = fmt.Errorf("suggestion is advisory and is not approved for automatic apply")
		return nil
	}

	if err := m.fixState.verifyAgainstCurrentData(m.reports, m.hpaUIDs); err != nil {
		m.fixState.applyErr = err
		return nil
	}

	m.fixState.applying = true
	m.fixState.applyErr = nil
	namespace := m.fixState.namespace
	name := m.fixState.name
	applyFn := m.opts.ApplyFn
	epoch := m.fixEpoch
	ctx := m.ctx

	return func() tea.Msg {
		err := applyFn(ctx, namespace, name, []hpaanalysis.Suggestion{suggestion})
		return applyResultMsg{epoch: epoch, title: suggestion.Title, err: err}
	}
}

// loadReplayTrace loads a timeline trace JSON file as a background tea.Cmd.
func loadReplayTrace(path string) tea.Cmd {
	return func() tea.Msg {
		trace, err := hpaanalysis.LoadTimelineTrace(path)
		var analysis *retrospective.ReplayAnalysis
		if err == nil && trace != nil {
			analysis = analyzeTimelineTraceReplay(trace)
		}
		return replayLoadedMsg{trace: trace, analysis: analysis, err: err}
	}
}

func analyzeTimelineTraceReplay(trace *hpaanalysis.TimelineTrace) *retrospective.ReplayAnalysis {
	if trace == nil || len(trace.Snapshots) == 0 {
		return nil
	}
	tl := retrospective.Timeline{
		HPAName:   trace.HPAName,
		Namespace: trace.Namespace,
		Since:     trace.Start,
		Until:     trace.End,
	}
	if tl.Until.IsZero() {
		tl.Until = trace.Snapshots[len(trace.Snapshots)-1].Timestamp
	}
	for i, snap := range trace.Snapshots {
		if i > 0 {
			prev := trace.Snapshots[i-1]
			if snap.Desired != prev.Desired {
				from, to := prev.Desired, snap.Desired
				tl.Entries = append(tl.Entries, retrospective.Entry{
					Timestamp:     snap.Timestamp,
					Category:      "rescale",
					FromReplicas:  &from,
					ToReplicas:    &to,
					ParseValid:    true,
					MetricContext: snap.TopMetric,
				})
			}
		}
		if snap.Health == string(hpaanalysis.HealthError) {
			tl.Entries = append(tl.Entries, retrospective.Entry{
				Timestamp: snap.Timestamp,
				Category:  "metrics-unavailable",
				Message:   snap.Summary,
			})
		} else if snap.Health == string(hpaanalysis.HealthStabilized) {
			tl.Entries = append(tl.Entries, retrospective.Entry{
				Timestamp: snap.Timestamp,
				Category:  "stabilized",
				Message:   snap.Summary,
			})
		}
	}
	return retrospective.AnalyzeReplay(tl, nil)
}

// handleSimInput handles text input when in simulation metric mode.
func (m Model) handleSimInput(msg tea.KeyMsg) (Model, tea.Cmd) {
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
	return m.ensureReport(k)
}

// parseMetricInput parses a metric input string like "cpu=80%" into
// the format expected by SimulateMetricChange.
func parseMetricInput(input string) (map[string]string, error) {
	if strings.TrimSpace(input) == "" {
		return nil, fmt.Errorf("empty metric input")
	}
	result := make(map[string]string)
	for _, pair := range strings.Split(input, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" {
			result[k] = v
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("invalid metric format; use name=value (e.g. cpu=80%%)")
	}
	return result, nil
}

// simulateWeights adapts the configured penalty weights to the simulate
// package's representation.
func simulateWeights(w hpaanalysis.HealthWeights) simulate.HealthWeights {
	return simulate.HealthWeightsWithOverrides(w)
}
