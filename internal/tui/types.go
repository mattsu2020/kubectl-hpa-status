package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/textinput"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// ApplyFunc applies a JSON merge patch to an HPA and returns any error.
// This callback is injected from the cmd layer to keep the TUI package
// free of direct Kubernetes patch imports.
type ApplyFunc func(ctx context.Context, namespace, name, patch string) error

// AuditFunc runs the best-practice auditor on an HPA and returns the report.
// Injected from the cmd layer to keep the TUI package free of direct
// Kubernetes client dependencies.
type AuditFunc func(ctx context.Context, namespace, name string) (*hpaanalysis.AuditReport, error)

// simState holds the interactive simulation panel state.
type simState struct {
	hpa        *autoscalingv2.HorizontalPodAutoscaler
	fields     []simField
	metricMode bool
	metricInput textinput.Model
	focusIndex  int
	result      *hpaanalysis.SimulationResult
	err         error
}

// simField represents one editable parameter in the simulation panel.
type simField struct {
	Label    string // display name, e.g. "maxReplicas"
	Path     string // override path, e.g. "maxReplicas"
	Value    string // current input value
	Original string // original HPA value for reference
}

// fixState holds the fix wizard state for a problematic HPA.
type fixState struct {
	suggestions  []hpaanalysis.Suggestion
	selected     int
	preview      string
	applied      bool
	applyErr     error
	dryRunResult string
}

// replayState holds the replay timeline viewer state.
type replayState struct {
	trace          *hpaanalysis.TimelineTrace
	replayAnalysis *hpaanalysis.ReplayAnalysis
	scrollPos      int
	err            error
	loading        bool
	filePath       string
}

// batchAuditState holds the batch auditor results for selected HPAs.
type batchAuditState struct {
	reports   map[string]*hpaanalysis.AuditReport
	results   []batchAuditEntry
	scrollPos int
	err       error
	loading   bool
}

// batchAuditEntry is a single entry in the batch audit results.
type batchAuditEntry struct {
	Namespace string
	Name      string
	Score     int
	Findings  int
	Critical  int
	Warnings  int
	Summary   string
}

// hintsState holds the metric hints troubleshooting view state.
type hintsState struct {
	flows      []hpaanalysis.MetricHintTroubleshooting
	selected   int
	stepScroll int
}

// Bubble Tea message types for asynchronous operations.

// simResultMsg carries simulation results from a background computation.
type simResultMsg struct {
	result *hpaanalysis.SimulationResult
	err    error
}

// applyResultMsg carries the result of a patch apply operation.
type applyResultMsg struct {
	title string
	err   error
}

// replayLoadedMsg carries a loaded timeline trace.
type replayLoadedMsg struct {
	trace *hpaanalysis.TimelineTrace
	err   error
}

// batchAuditMsg carries the results of a batch audit operation.
type batchAuditMsg struct {
	reports map[string]*hpaanalysis.AuditReport
	err     error
}
