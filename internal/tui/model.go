// Package tui implements an interactive terminal dashboard for HPA monitoring.
package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
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
	sortField         string
	sortDescending    bool
	selected          map[string]bool
	initialFocused    bool
	batchApplyConfirm bool
	batchApplyPreview []string

	// Interactive mode states (nil when inactive).
	simState        *simState
	fixState        *fixState
	replayState     *replayState
	batchAuditState *batchAuditState
	historyState    *historyState
	hintsState      *hintsState

	// replicaHistory holds recent replica snapshots per HPA for inline sparklines.
	// Keyed by "namespace/name", value is a slice of desired replica counts
	// from the last N refresh cycles.
	replicaHistory map[string][]float64

	keys keyMap
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

	// EnrichHPAs is an optional callback that applies KEDA/VPA enrichment
	// to a slice of HPAs. When set, fetchHPAs calls it after the initial
	// analysis pass to populate KEDAInfo and VPAConflict fields.
	EnrichHPAs func(ctx context.Context, hpas []autoscalingv2.HorizontalPodAutoscaler) (
		kedaResults map[string]*hpakeda.Analysis,
		vpaResults map[string]*hpavpa.ConflictInfo,
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

// keyDef is a single row in the defaultKeys table.
type keyDef struct {
	keys []string
	help string
	desc string
}

// defaultKeyTable lists every binding in display order. The field order must
// match keyMap's struct field order so the loop in defaultKeys can assign by
// index.
var defaultKeyTable = []keyDef{
	{[]string{"up", "k"}, "↑/k", "up"},
	{[]string{"down", "j"}, "↓/j", "down"},
	{[]string{"enter"}, "enter", "detail"},
	{[]string{"esc"}, "esc", "back"},
	{[]string{"q", "ctrl+c"}, "q", "quit"},
	{[]string{"r"}, "r", "refresh"},
	{[]string{"p"}, "p", "pause"},
	{[]string{"/"}, "/", "filter"},
	{[]string{"?"}, "?", "help"},
	{[]string{"S"}, "S", "sort cycle"},
	{[]string{"g"}, "g", "jump to problems"},
	{[]string{"m"}, "m", "metrics detail"},
	{[]string{"space", " "}, "space", "toggle select"},
	{[]string{"a"}, "a", "select all"},
	{[]string{"A"}, "A", "deselect all"},
	{[]string{"s"}, "s", "simulate"},
	{[]string{"f"}, "f", "fix wizard"},
	{[]string{"T"}, "T", "replay timeline"},
	{[]string{"M"}, "M", "metric simulation"},
	{[]string{"tab"}, "tab", "next field"},
	{[]string{"shift+tab"}, "shift+tab", "previous field"},
	{[]string{"d"}, "d", "server dry-run"},
	{[]string{"+", "="}, "+/=", "faster refresh"},
	{[]string{"-"}, "-", "slower refresh"},
	{[]string{"B"}, "B", "batch auditor"},
	{[]string{"x"}, "x", "preview/confirm batch apply"},
	{[]string{"H"}, "H", "history/sparkline"},
	{[]string{"h"}, "h", "metric hints"},
	{[]string{"O"}, "O", "cluster overview"},
}

func defaultKeys() keyMap {
	bindings := make([]key.Binding, len(defaultKeyTable))
	for i, def := range defaultKeyTable {
		bindings[i] = key.NewBinding(
			key.WithKeys(def.keys...),
			key.WithHelp(def.help, def.desc),
		)
	}
	return keyMap{
		Up:            bindings[0],
		Down:          bindings[1],
		Enter:         bindings[2],
		Escape:        bindings[3],
		Quit:          bindings[4],
		Refresh:       bindings[5],
		Pause:         bindings[6],
		Filter:        bindings[7],
		Help:          bindings[8],
		Sort:          bindings[9],
		JumpProblem:   bindings[10],
		Metrics:       bindings[11],
		ToggleSelect:  bindings[12],
		SelectAll:     bindings[13],
		DeselectAll:   bindings[14],
		Simulate:      bindings[15],
		Fix:           bindings[16],
		Replay:        bindings[17],
		MetricMode:    bindings[18],
		TabField:      bindings[19],
		ShiftTabField: bindings[20],
		DryRun:        bindings[21],
		IntervalUp:    bindings[22],
		IntervalDown:  bindings[23],
		BatchAudit:    bindings[24],
		BatchApply:    bindings[25],
		History:       bindings[26],
		Hints:         bindings[27],
		Overview:      bindings[28],
	}
}

// tickMsg is sent on each interval tick.
type tickMsg time.Time

// fetchResultMsg carries the result of a background data fetch.
type fetchResultMsg struct {
	items   []hpaanalysis.ListItem
	reports map[string]*hpaanalysis.StatusReport
	err     error
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
	ctx       context.Context
	client    kubernetes.Interface
	namespace string
	opts      Options
}

// newFetchConfig snapshots the minimal inputs required by fetchHPAs.
func (m Model) newFetchConfig() fetchConfig {
	return fetchConfig{
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
			return fetchResultMsg{err: err}
		}

		// Run optional batched KEDA/VPA enrichment.
		var kedaResults map[string]*hpakeda.Analysis
		var vpaResults map[string]*hpavpa.ConflictInfo
		if cfg.opts.EnrichHPAs != nil {
			kedaResults, vpaResults = cfg.opts.EnrichHPAs(cfg.ctx, hpas.Items)
		}

		items := make([]hpaanalysis.ListItem, 0, len(hpas.Items))
		reports := make(map[string]*hpaanalysis.StatusReport, len(hpas.Items))
		for i := range hpas.Items {
			analysis := hpaanalysis.AnalyzeWithOptions(&hpas.Items[i], true, hpaanalysis.AnalysisOptions{
				Debug:         cfg.opts.Debug,
				HealthWeights: cfg.opts.HealthWeights,
			})

			// Apply enrichment data from batched results.
			key := analysis.Namespace + "/" + analysis.Name
			if kedaResults != nil {
				if keda, ok := kedaResults[key]; ok {
					analysis.KEDAInfo = keda
				}
			}
			if vpaResults != nil {
				if vpa, ok := vpaResults[key]; ok {
					analysis.VPAConflict = vpa
				}
			}
			if analysis.KEDAInfo != nil || analysis.VPAConflict != nil {
				hpaanalysis.ApplyEnrichmentPenalties(&analysis, cfg.opts.HealthWeights)
			}
			analysis = hpaanalysis.FinalizeAnalysis(analysis)

			item := hpaanalysis.NewListItem(analysis)
			items = append(items, item)
			reports[key] = &hpaanalysis.StatusReport{APIVersion: hpaanalysis.SchemaVersion, Analysis: analysis}
		}

		return fetchResultMsg{items: items, reports: reports}
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
