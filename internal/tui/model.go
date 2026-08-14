// Package tui implements an interactive terminal dashboard for HPA monitoring.
package tui

import (
	"context"
	"maps"
	"slices"
	"time"

	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/client-go/kubernetes"
)

type viewMode int

const (
	listView viewMode = iota
	detailView
	helpView
	metricsView
	simView        // Interactive simulation panel
	fixView        // Fix wizard for problematic HPAs
	replayView     // Replay timeline visualization
	batchAuditView // Batch auditor results for selected HPAs
	historyView    // History/sparkline view for scaling trends
	overviewView   // Cluster-wide health overview
	hintsView      // Metric hints troubleshooting
	viewModeCount  // Sentinel used to verify controller registry coverage.
)

// Model is the top-level bubbletea model for the TUI dashboard.
type Model struct {
	client    kubernetes.Interface
	namespace string
	opts      Options
	ctx       context.Context

	items             []hpaanalysis.ListItem
	reports           map[string]*hpaanalysis.StatusReport
	cursor            int
	viewMode          viewMode
	paused            bool
	filter            string
	filterInput       textinput.Model
	filtering         bool
	interval          time.Duration
	lastRefresh       time.Time
	err               error
	width             int
	height            int
	loading           bool
	fetchRequestID    uint64
	sortField         string
	sortDescending    bool
	selected          map[string]bool
	initialFocused    bool
	batchApplyConfirm bool
	batchApplyPreview []string

	interactiveStates

	// replicaHistory holds recent replica snapshots per HPA for inline sparklines.
	// Keyed by "namespace/name", value is a slice of desired replica counts
	// from the last N refresh cycles.
	replicaHistory map[string][]float64

	keys keyMap
}

// interactiveStates groups the independent view-local submodels. Embedding
// keeps controller field access concise while each state owns its mutable
// containers and transitions.
type interactiveStates struct {
	simState        *simState
	fixState        *fixState
	replayState     *replayState
	batchAuditState *batchAuditState
	historyState    *historyState
	hintsState      *hintsState
}

// clone returns a copy of m whose mutable containers — the item slice, the
// selection set, the per-HPA replica history, the batch-apply preview, and
// the view-local interactive states — are independent of the receiver. This
// makes Model's value-receiver Update contract real: updating a returned
// model never mutates an older copy. The reports map is not cloned because
// fetches replace it wholesale and handlers only read entries.
func (m Model) clone() Model {
	m.items = slices.Clone(m.items)
	if m.selected != nil {
		selected := make(map[string]bool, len(m.selected))
		maps.Copy(selected, m.selected)
		m.selected = selected
	}
	if m.replicaHistory != nil {
		history := make(map[string][]float64, len(m.replicaHistory))
		for key, values := range m.replicaHistory {
			history[key] = slices.Clone(values)
		}
		m.replicaHistory = history
	}
	m.batchApplyPreview = slices.Clone(m.batchApplyPreview)
	m.interactiveStates = m.interactiveStates.clone()
	return m
}

// clone returns independent view-local state so Model's value-receiver Update
// contract is real: updating a returned model never mutates an older copy.
func (s interactiveStates) clone() interactiveStates {
	out := s
	if s.simState != nil {
		out.simState = s.simState.clone()
	}
	if s.fixState != nil {
		out.fixState = s.fixState.clone()
	}
	if s.replayState != nil {
		out.replayState = s.replayState.clone()
	}
	if s.batchAuditState != nil {
		out.batchAuditState = s.batchAuditState.clone()
	}
	if s.historyState != nil {
		out.historyState = s.historyState.clone()
	}
	if s.hintsState != nil {
		out.hintsState = s.hintsState.clone()
	}
	return out
}

// Options holds configuration for the TUI dashboard.
type Options struct {
	Namespace     string
	AllNamespaces bool
	ColorEnabled  bool
	Debug         bool
	ChunkSize     int64
	Interval      time.Duration
	InitialName   string
	InitialNS     string
	StartInDetail bool
	Now           func() time.Time

	// EnrichHPAs is an optional callback that applies KEDA/VPA enrichment
	// to a slice of HPAs. When set, fetchHPAs calls it after the initial
	// analysis pass to populate KEDAInfo and VPAConflict fields.
	EnrichHPAs func(ctx context.Context, hpas []autoscalingv2.HorizontalPodAutoscaler) (
		kedaResults map[string]*hpakeda.Analysis,
		vpaResults map[string]*hpavpa.ConflictInfo,
		warnings map[string][]string,
	)

	// HealthWeights holds user-configured penalty weights for enrichment
	// health score adjustments. When zero-valued, ApplyEnrichmentPenalties
	// uses its defaults.
	HealthWeights hpaanalysis.HealthWeights

	// ApplyFn is an optional callback for applying patches from the TUI.
	// It must only be set when the user explicitly enabled persistent changes.
	// When nil, the fix wizard live-apply action is disabled.
	ApplyFn ApplyFunc

	// DryRunFn validates suggestions with Kubernetes server-side dry-run. It is
	// available independently of ApplyFn so read-only TUI sessions can safely
	// validate a proposed change without enabling persistence.
	DryRunFn ApplyFunc

	// AuditFn is an optional callback for running the best-practice auditor
	// on an HPA. When nil, the batch auditor action is disabled.
	AuditFn AuditFunc
}

func (m Model) currentTime() time.Time {
	if m.opts.Now != nil {
		return m.opts.Now()
	}
	return time.Now()
}

// tickMsg is sent on each interval tick.
type tickMsg time.Time

// fetchResultMsg carries the result of a background data fetch.
type fetchResultMsg struct {
	requestID uint64
	items     []hpaanalysis.ListItem
	reports   map[string]*hpaanalysis.StatusReport
	err       error
}

// NewModel creates a new TUI Model.
func NewModel(client kubernetes.Interface, namespace string, opts Options) Model {
	ti := textinput.New()
	ti.Placeholder = "filter by name..."
	ti.CharLimit = 50

	interval := opts.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	return Model{
		client:         client,
		namespace:      namespace,
		opts:           opts,
		ctx:            context.Background(),
		items:          []hpaanalysis.ListItem{},
		reports:        map[string]*hpaanalysis.StatusReport{},
		replicaHistory: map[string][]float64{},
		cursor:         0,
		viewMode:       listView,
		interval:       interval,
		keys:           defaultKeys(),
		filterInput:    ti,
		loading:        true,
		fetchRequestID: 1,
		selected:       map[string]bool{},
	}
}

// WithContext returns a copy of the model bound to ctx. The context is
// propagated to in-flight background commands (fetch, enrich, audit, apply)
// so they are cancelled when the TUI exits or its watch deadline elapses,
// instead of using context.Background() which keeps them running.
func (m Model) WithContext(ctx context.Context) Model {
	m.ctx = ctx
	return m
}

// Init starts the first data fetch.
func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchHPAs(m), tickCmd(m.interval))
}
