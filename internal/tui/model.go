// Package tui implements an interactive terminal dashboard for HPA monitoring.
package tui

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	analysisservice "github.com/mattsu2020/kubectl-hpa-status/internal/analysis"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// interactiveStates owns view-local state. It is embedded during the
// migration so existing field access remains source-compatible while
// controllers progressively become independent submodels.
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
		v := *s.simState
		v.fields = slices.Clone(s.simState.fields)
		if s.simState.hpa != nil {
			v.hpa = s.simState.hpa.DeepCopy()
		}
		out.simState = &v
	}
	if s.fixState != nil {
		v := *s.fixState
		v.suggestions = slices.Clone(s.fixState.suggestions)
		out.fixState = &v
	}
	if s.replayState != nil {
		v := *s.replayState
		out.replayState = &v
	}
	if s.batchAuditState != nil {
		v := *s.batchAuditState
		v.results = slices.Clone(s.batchAuditState.results)
		v.reports = make(map[string]*audit.Report, len(s.batchAuditState.reports))
		for key, report := range s.batchAuditState.reports {
			v.reports[key] = report
		}
		out.batchAuditState = &v
	}
	if s.historyState != nil {
		v := *s.historyState
		v.snapshots = slices.Clone(s.historyState.snapshots)
		out.historyState = &v
	}
	if s.hintsState != nil {
		v := *s.hintsState
		v.flows = slices.Clone(s.hintsState.flows)
		out.hintsState = &v
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

// keyMap defines the keyboard shortcuts.
type keyMap struct {
	Up            key.Binding
	Down          key.Binding
	Enter         key.Binding
	Escape        key.Binding
	Quit          key.Binding
	Refresh       key.Binding
	Pause         key.Binding
	Filter        key.Binding
	Help          key.Binding
	Sort          key.Binding
	JumpProblem   key.Binding
	Metrics       key.Binding
	ToggleSelect  key.Binding
	SelectAll     key.Binding
	DeselectAll   key.Binding
	Simulate      key.Binding
	Fix           key.Binding
	Replay        key.Binding
	MetricMode    key.Binding
	TabField      key.Binding
	ShiftTabField key.Binding
	DryRun        key.Binding
	IntervalUp    key.Binding
	IntervalDown  key.Binding
	BatchAudit    key.Binding
	BatchApply    key.Binding
	History       key.Binding
	Hints         key.Binding
	Overview      key.Binding
}

func defaultKeys() keyMap {
	b := func(keys []string, help, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	return keyMap{
		Up: b([]string{"up", "k"}, "↑/k", "up"), Down: b([]string{"down", "j"}, "↓/j", "down"),
		Enter: b([]string{"enter"}, "enter", "detail"), Escape: b([]string{"esc"}, "esc", "back"),
		Quit: b([]string{"q", "ctrl+c"}, "q", "quit"), Refresh: b([]string{"r"}, "r", "refresh"),
		Pause: b([]string{"p"}, "p", "pause"), Filter: b([]string{"/"}, "/", "filter"), Help: b([]string{"?"}, "?", "help"),
		Sort: b([]string{"S"}, "S", "sort cycle"), JumpProblem: b([]string{"g"}, "g", "jump to problems"), Metrics: b([]string{"m"}, "m", "metrics detail"),
		ToggleSelect: b([]string{"space", " "}, "space", "toggle select"), SelectAll: b([]string{"a"}, "a", "select all"), DeselectAll: b([]string{"A"}, "A", "deselect all"),
		Simulate: b([]string{"s"}, "s", "simulate"), Fix: b([]string{"f"}, "f", "fix wizard"), Replay: b([]string{"T"}, "T", "replay timeline"),
		MetricMode: b([]string{"M"}, "M", "metric simulation"), TabField: b([]string{"tab"}, "tab", "next field"), ShiftTabField: b([]string{"shift+tab"}, "shift+tab", "previous field"),
		DryRun: b([]string{"d"}, "d", "server dry-run"), IntervalUp: b([]string{"+", "="}, "+/=", "faster refresh"), IntervalDown: b([]string{"-"}, "-", "slower refresh"),
		BatchAudit: b([]string{"B"}, "B", "batch auditor"), BatchApply: b([]string{"x"}, "x", "preview/confirm batch apply"), History: b([]string{"H"}, "H", "history/sparkline"),
		Hints: b([]string{"h"}, "h", "metric hints"), Overview: b([]string{"O"}, "O", "cluster overview"),
	}
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

// filteredItems returns items matching the current filter text.
// Uses a value receiver since it does not mutate state.
func (m Model) filteredItems() []hpaanalysis.ListItem {
	if m.filter == "" {
		return m.items
	}
	filtered := make([]hpaanalysis.ListItem, 0, len(m.items))
	for _, item := range m.items {
		if containsIgnoreCase(item.Name, m.filter) ||
			containsIgnoreCase(item.Namespace, m.filter) ||
			containsIgnoreCase(item.Health, m.filter) ||
			containsIgnoreCase(item.Issue, m.filter) ||
			containsIgnoreCase(item.Summary, m.filter) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func containsIgnoreCase(s, substr string) bool {
	return len(substr) == 0 ||
		(len(s) >= len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	sl := len(s)
	subl := len(substr)
	for i := 0; i <= sl-subl; i++ {
		match := true
		for j := 0; j < subl; j++ {
			sc := s[i+j]
			bc := substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if bc >= 'A' && bc <= 'Z' {
				bc += 32
			}
			if sc != bc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// sortItems sorts the item list by the current sort field.
func (m *Model) sortItems() {
	if m.sortField == "" {
		return
	}
	slices.SortStableFunc(m.items, func(a, b hpaanalysis.ListItem) int {
		var cmp int
		switch m.sortField {
		case "name":
			cmp = strings.Compare(a.Name, b.Name)
		case "namespace":
			cmp = strings.Compare(a.Namespace, b.Namespace)
		case "health-score":
			cmp = cmpInt(a.HealthScore, b.HealthScore)
		case "issue":
			cmp = strings.Compare(a.Issue, b.Issue)
		}
		if m.sortDescending {
			return -cmp
		}
		return cmp
	})
}

func (m *Model) focusInitialItem() {
	if m.opts.InitialName == "" {
		return
	}
	for i, item := range m.items {
		if item.Name != m.opts.InitialName {
			continue
		}
		if m.opts.InitialNS != "" && item.Namespace != m.opts.InitialNS {
			continue
		}
		m.cursor = i
		if m.opts.StartInDetail {
			m.viewMode = detailView
		}
		return
	}
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// fetchConfig carries only the fields fetchHPAs needs, so the background
// command closure captures this small value instead of the full Model (which
// includes large slices/maps like items, reports, and replicaHistory) on every
// refresh tick.
type fetchConfig struct {
	requestID uint64
	ctx       context.Context
	client    kubernetes.Interface
	namespace string
	opts      Options
}

// newFetchConfig snapshots the minimal inputs required by fetchHPAs.
func (m Model) newFetchConfig() fetchConfig {
	return fetchConfig{
		requestID: m.fetchRequestID,
		ctx:       m.ctx,
		client:    m.client,
		namespace: m.namespace,
		opts:      m.opts,
	}
}

// fetchHPAs fetches all HPA items in the background.
func fetchHPAs(m Model) tea.Cmd {
	cfg := m.newFetchConfig()
	return func() tea.Msg {
		ns := cfg.namespace
		if cfg.opts.AllNamespaces {
			ns = metav1.NamespaceAll
		}

		hpas, err := kube.ListHPAsFromInterface(cfg.ctx, cfg.client, ns, metav1.ListOptions{}, cfg.opts.ChunkSize)
		if err != nil {
			return fetchResultMsg{requestID: cfg.requestID, err: err}
		}

		// Run optional batched KEDA/VPA enrichment.
		var kedaResults map[string]*hpakeda.Analysis
		var vpaResults map[string]*hpavpa.ConflictInfo
		var enrichmentWarnings map[string][]string
		if cfg.opts.EnrichHPAs != nil {
			kedaResults, vpaResults, enrichmentWarnings = cfg.opts.EnrichHPAs(cfg.ctx, hpas.Items)
		}

		analyzed := analysisservice.AnalyzeBatch(hpas.Items, analysisservice.Options{
			IncludeInterpretation: true,
			Debug:                 cfg.opts.Debug,
			HealthWeights:         cfg.opts.HealthWeights,
		}, analysisservice.BatchEnrichment{
			KEDA:     kedaResults,
			VPA:      vpaResults,
			Warnings: enrichmentWarnings,
		})
		items := make([]hpaanalysis.ListItem, 0, len(analyzed))
		reports := make(map[string]*hpaanalysis.StatusReport, len(analyzed))
		for i := range analyzed {
			items = append(items, analyzed[i].ListItem)
			report := analyzed[i].Report
			reports[analyzed[i].Key] = &report
		}

		return fetchResultMsg{requestID: cfg.requestID, items: items, reports: reports}
	}
}

func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Styles for the TUI.
var (
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	cursorStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	okStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	errorStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	warnStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func healthStyle(health string) lipgloss.Style {
	switch health {
	case string(hpaanalysis.HealthOK):
		return okStyle
	case string(hpaanalysis.HealthError):
		return errorStyle
	default:
		return warnStyle
	}
}

func truncate(s string, maxLen int) string {
	return rendutil.TruncateDisplayWidth(s, maxLen, "…")
}

func padRight(s string, width int) string {
	return rendutil.FitDisplayWidth(s, width)
}
