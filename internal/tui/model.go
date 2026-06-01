// Package tui implements an interactive terminal dashboard for HPA monitoring.
package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type viewMode int

const (
	listView viewMode = iota
	detailView
)

// Model is the top-level bubbletea model for the TUI dashboard.
type Model struct {
	client    kubernetes.Interface
	namespace string
	opts      Options

	items       []hpaanalysis.ListItem
	reports     map[string]*hpaanalysis.StatusReport
	cursor      int
	viewMode    viewMode
	paused      bool
	filter      string
	filterInput textinput.Model
	filtering   bool
	interval    time.Duration
	lastRefresh time.Time
	err         error
	width       int
	height      int
	loading     bool

	keys keyMap
}

// Options holds configuration for the TUI dashboard.
type Options struct {
	Namespace     string
	AllNamespaces bool
	ColorEnabled  bool
	Debug         bool
}

// keyMap defines the keyboard shortcuts.
type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Escape  key.Binding
	Quit    key.Binding
	Refresh key.Binding
	Pause   key.Binding
	Filter  key.Binding
	Help    key.Binding
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

	return Model{
		client:      client,
		namespace:   namespace,
		opts:        opts,
		items:       []hpaanalysis.ListItem{},
		reports:     map[string]*hpaanalysis.StatusReport{},
		cursor:      0,
		viewMode:    listView,
		interval:    5 * time.Second,
		keys:        defaultKeys(),
		filterInput: ti,
		loading:     true,
	}
}

// Init starts the first data fetch.
func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchHPAs(m), tickCmd(m.interval))
}

// filteredItems returns items matching the current filter text.
func (m *Model) filteredItems() []hpaanalysis.ListItem {
	if m.filter == "" {
		return m.items
	}
	filtered := make([]hpaanalysis.ListItem, 0, len(m.items))
	for _, item := range m.items {
		if containsIgnoreCase(item.Name, m.filter) || containsIgnoreCase(item.Namespace, m.filter) {
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

// fetchHPAs fetches all HPA items in the background.
func fetchHPAs(m Model) tea.Cmd {
	return func() tea.Msg {
		ns := m.namespace
		if m.opts.AllNamespaces {
			ns = metav1.NamespaceAll
		}

		hpas, err := m.client.AutoscalingV2().HorizontalPodAutoscalers(ns).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return fetchResultMsg{err: err}
		}

		items := make([]hpaanalysis.ListItem, 0, len(hpas.Items))
		reports := make(map[string]*hpaanalysis.StatusReport, len(hpas.Items))
		for i := range hpas.Items {
			analysis := hpaanalysis.AnalyzeWithOptions(&hpas.Items[i], false, hpaanalysis.AnalysisOptions{
				Debug: m.opts.Debug,
			})
			item := hpaanalysis.NewListItem(analysis)
			items = append(items, item)
			key := item.Namespace + "/" + item.Name
			reports[key] = &hpaanalysis.StatusReport{Analysis: analysis}
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
