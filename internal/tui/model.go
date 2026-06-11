// Package tui implements an interactive terminal dashboard for HPA monitoring.
package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
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

// maxReplicaHistoryPoints is the rolling window size for inline sparklines.
const maxReplicaHistoryPoints = 15

// Model is the top-level bubbletea model for the TUI dashboard.
type Model struct {
	client    kubernetes.Interface
	namespace string
	opts      Options

	items          []hpaanalysis.ListItem
	reports        map[string]*hpaanalysis.StatusReport
	cursor         int
	viewMode       viewMode
	paused         bool
	filter         string
	filterInput    textinput.Model
	filtering      bool
	interval       time.Duration
	lastRefresh    time.Time
	err            error
	width          int
	height         int
	loading        bool
	sortField      string
	sortDescending bool
	selected       map[string]bool
	initialFocused bool

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
		kedaResults map[string]*hpaanalysis.KEDAAnalysis,
		vpaResults map[string]*hpaanalysis.VPAConflictInfo,
	)

	// HealthWeights holds user-configured penalty weights for enrichment
	// health score adjustments. When zero-valued, ApplyEnrichmentPenalties
	// uses its defaults.
	HealthWeights hpaanalysis.HealthWeights

	// ApplyFn is an optional callback for applying patches from the TUI.
	// When nil, the fix wizard apply action is disabled.
	ApplyFn ApplyFunc

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

func defaultKeys() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "detail"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Pause: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "pause"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Sort: key.NewBinding(
			key.WithKeys("S"),
			key.WithHelp("S", "sort cycle"),
		),
		JumpProblem: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "jump to problems"),
		),
		Metrics: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "metrics detail"),
		),
		ToggleSelect: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "toggle select"),
		),
		SelectAll: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "select all"),
		),
		DeselectAll: key.NewBinding(
			key.WithKeys("A"),
			key.WithHelp("A", "deselect all"),
		),
		Simulate: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "simulate"),
		),
		Fix: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "fix wizard"),
		),
		Replay: key.NewBinding(
			key.WithKeys("T"),
			key.WithHelp("T", "replay timeline"),
		),
		MetricMode: key.NewBinding(
			key.WithKeys("M"),
			key.WithHelp("M", "metric simulation"),
		),
		TabField: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next field"),
		),
		ShiftTabField: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "previous field"),
		),
		DryRun: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "preview dry-run"),
		),
		IntervalUp: key.NewBinding(
			key.WithKeys("+", "="),
			key.WithHelp("+/=", "faster refresh"),
		),
		IntervalDown: key.NewBinding(
			key.WithKeys("-"),
			key.WithHelp("-", "slower refresh"),
		),
		BatchAudit: key.NewBinding(
			key.WithKeys("B"),
			key.WithHelp("B", "batch auditor"),
		),
		BatchApply: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "batch apply"),
		),
		History: key.NewBinding(
			key.WithKeys("H"),
			key.WithHelp("H", "history/sparkline"),
		),
		Hints: key.NewBinding(
			key.WithKeys("h"),
			key.WithHelp("h", "metric hints"),
		),
		Overview: key.NewBinding(
			key.WithKeys("O"),
			key.WithHelp("O", "cluster overview"),
		),
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

// fetchHPAs fetches all HPA items in the background.
func fetchHPAs(m Model) tea.Cmd {
	return func() tea.Msg {
		ns := m.namespace
		if m.opts.AllNamespaces {
			ns = metav1.NamespaceAll
		}

		hpas, err := listHPAs(context.Background(), m.client, ns, metav1.ListOptions{}, m.opts.ChunkSize)
		if err != nil {
			return fetchResultMsg{err: err}
		}

		// Run optional batched KEDA/VPA enrichment.
		var kedaResults map[string]*hpaanalysis.KEDAAnalysis
		var vpaResults map[string]*hpaanalysis.VPAConflictInfo
		if m.opts.EnrichHPAs != nil {
			kedaResults, vpaResults = m.opts.EnrichHPAs(context.Background(), hpas.Items)
		}

		items := make([]hpaanalysis.ListItem, 0, len(hpas.Items))
		reports := make(map[string]*hpaanalysis.StatusReport, len(hpas.Items))
		for i := range hpas.Items {
			analysis := hpaanalysis.AnalyzeWithOptions(&hpas.Items[i], true, hpaanalysis.AnalysisOptions{
				Debug: m.opts.Debug,
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
				hpaanalysis.ApplyEnrichmentPenalties(&analysis, m.opts.HealthWeights)
			}

			item := hpaanalysis.NewListItem(analysis)
			items = append(items, item)
			reports[key] = &hpaanalysis.StatusReport{Analysis: analysis}
		}

		return fetchResultMsg{items: items, reports: reports}
	}
}

func listHPAs(ctx context.Context, client kubernetes.Interface, namespace string, opts metav1.ListOptions, chunkSize int64) (*autoscalingv2.HorizontalPodAutoscalerList, error) {
	if chunkSize <= 0 {
		return client.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, opts)
	}
	opts.Limit = chunkSize
	opts.Continue = ""
	all := &autoscalingv2.HorizontalPodAutoscalerList{}
	for {
		page, err := client.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		all.Items = append(all.Items, page.Items...)
		if page.Continue == "" {
			return all, nil
		}
		opts.Continue = page.Continue
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
	case "OK":
		return okStyle
	case "ERROR":
		return errorStyle
	default:
		return warnStyle
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}
