package tui

import (
	"context"

	"charm.land/bubbles/v2/textinput"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/retrospective"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/simulate"
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
	result      *simulate.SimulationResult
	err         error
}

// simField represents one editable parameter in the simulation panel.
type simField struct {
	Label    string // display name, e.g. "maxReplicas"
	Path     string // override path, e.g. "maxReplicas"
	Value    string // current input value
	Original string // original HPA value for reference
}

// fixState holds the fix wizard state for a problematic HPA. The wizard
// targets a specific HPA identity (namespace/name plus the observed UID), not
// whatever row the cursor happens to sit on later: an auto-refresh can
// reorder the list between opening the wizard and pressing Enter, and a
// name-based match alone cannot tell that an HPA was deleted and recreated
// with fresh suggestions. Suggestions are regenerated from the latest report
// on every successful refresh.
type fixState struct {
	namespace    string
	name         string
	uid          string
	suggestions  []hpaanalysis.Suggestion
	selected     int
	applyConfirm bool
	applied      bool
	applyErr     error
	dryRunResult string
}

// key returns the "namespace/name" identity key of the targeted HPA.
func (s *fixState) key() string {
	return s.namespace + "/" + s.name
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
	result *simulate.SimulationResult
	err    error
}

// applyResultMsg carries the result of a patch apply operation. The epoch
// identifies the fix-wizard state that started the apply; results from an
// earlier epoch (e.g. after a refresh replaced the suggestions) are dropped.
type applyResultMsg struct {
	epoch int
	title string
	err   error
}

// dryRunResultMsg carries the result of a real server-side dry-run. It is
// separate from applyResultMsg so successful validation is never presented as
// a persisted change. The epoch guards against stale results the same way as
// applyResultMsg.
type dryRunResultMsg struct {
	epoch int
	title string
	err   error
}

// batchApplyResultMsg carries the outcome of a batch apply started from the
// list view. It is separate from applyResultMsg because it is not tied to the
// fix-wizard state; it is surfaced as a transient status message instead.
type batchApplyResultMsg struct {
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
