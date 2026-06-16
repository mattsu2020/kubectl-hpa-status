package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		err := applyFn(m.ctx, namespace, name, patch)
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
