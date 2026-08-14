package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	analysisservice "github.com/mattsu2020/kubectl-hpa-status/internal/analysis"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

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
