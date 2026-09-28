package tui

import (
	tea "charm.land/bubbletea/v2"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	hpachurn "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
	hpamodel "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/model"
)

// historyLoadedMsg carries the result of the background history load keyed to
// the "namespace/name" identity it was requested for.
type historyLoadedMsg struct {
	key       string
	snapshots []hpaanalysis.TimelineSnapshot
	err       error
}

// loadHistorySnapshots starts the background history load for the currently
// selected HPA. The load is anchored to the HPA identity and the UID observed
// for it, so the store never merges histories across a delete+recreate under
// the same name. A nil LoadHistoryFn leaves the view in its empty state.
func (m Model) loadHistorySnapshots() tea.Cmd {
	if m.opts.LoadHistoryFn == nil {
		return nil
	}
	items := m.filteredItems()
	if m.cursor >= len(items) || m.historyState == nil {
		return nil
	}
	item := items[m.cursor]
	key := item.Namespace + "/" + item.Name
	m.historyState.key = key
	load := m.opts.LoadHistoryFn
	ctx := m.ctx
	uid := m.hpaUIDs[key]
	return func() tea.Msg {
		snapshots, err := load(ctx, item.Namespace, item.Name, uid)
		return historyLoadedMsg{key: key, snapshots: snapshots, err: err}
	}
}

// updateHistoryLoaded attaches a completed history load to the open history
// view. Results for another HPA identity (the cursor moved and history was
// re-entered) are dropped.
func (m Model) updateHistoryLoaded(msg historyLoadedMsg) (tea.Model, tea.Cmd) {
	if m.viewMode != historyView || m.historyState == nil {
		return m, nil
	}
	if m.historyState.key != "" && m.historyState.key != msg.key {
		return m, nil
	}
	m.historyState.loading = false
	m.historyState.loadErr = msg.err
	if msg.err == nil {
		m.historyState.snapshots = msg.snapshots
		// Compute the churn analysis once per load; the shared pointer is
		// safe because historyState treats it as read-only after this
		// assignment. Without this the view recomputed it every frame.
		m.historyState.churnAnalysis = churnFromSnapshots(msg.snapshots)
	}
	return m, nil
}

// churnFromSnapshots projects history snapshots into rescale data and runs the
// canonical churn analyzer once, so the history view does not recompute it on
// every render.
func churnFromSnapshots(snapshots []hpaanalysis.TimelineSnapshot) *hpachurn.ChurnAnalysis {
	if len(snapshots) == 0 {
		return nil
	}
	rescales := make([]hpamodel.RescaleData, 0, len(snapshots))
	for _, snap := range snapshots {
		rescales = append(rescales, hpamodel.RescaleData{
			Timestamp: snap.Timestamp,
			NewSize:   snap.Desired,
		})
	}
	return hpachurn.AnalyzeFromRescales(rescales, nil)
}
