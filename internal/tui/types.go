package tui

import (
	"context"

	"charm.land/bubbles/v2/textinput"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/retrospective"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// ApplyFunc validates or applies a group of suggestions for one HPA and
// returns any error. Grouping is intentional: the cmd layer can merge all
// changes for an HPA into one atomic patch and enforce the same safety policy
// as the non-interactive CLI.
type ApplyFunc func(ctx context.Context, namespace, name string, suggestions []hpaanalysis.Suggestion) error

// AuditFunc runs the best-practice auditor on an HPA and returns the report.
// Injected from the cmd layer to keep the TUI package free of direct
// Kubernetes client dependencies.
type AuditFunc func(ctx context.Context, namespace, name string) (*audit.Report, error)

// simState holds the interactive simulation panel state.
type simState struct {
	hpa         *autoscalingv2.HorizontalPodAutoscaler
	fields      []simField
	metricMode  bool
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
	applyConfirm bool
	applied      bool
	applyErr     error
	dryRunResult string
}

// replayState holds the replay timeline viewer state.
type replayState struct {
	trace          *hpaanalysis.TimelineTrace
	replayAnalysis *retrospective.ReplayAnalysis
	scrollPos      int
	err            error
	loading        bool
	filePath       string
}

// batchAuditState holds the batch auditor results for selected HPAs.
type batchAuditState struct {
	reports   map[string]*audit.Report
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

// dryRunResultMsg carries the result of a real server-side dry-run. It is
// separate from applyResultMsg so successful validation is never presented as
// a persisted change.
type dryRunResultMsg struct {
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
	reports map[string]*audit.Report
	err     error
}
