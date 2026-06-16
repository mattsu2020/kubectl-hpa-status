package tui

import (
	"context"
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// This file holds the batch audit/apply handlers and their helpers, split
// from update.go to keep each file focused.

// handleBatchAuditKey starts the batch auditor for all selected HPAs.
func (m Model) handleBatchAuditKey() (tea.Model, tea.Cmd) {
	if m.viewMode != listView {
		return m, nil
	}
	if m.opts.AuditFn == nil {
		m.err = fmt.Errorf("auditor not available")
		return m, nil
	}
	selected := m.selectedHPANames()
	if len(selected) == 0 {
		m.err = fmt.Errorf("no HPAs selected; use space to select, a to select all")
		return m, nil
	}

	m.batchAuditState = &batchAuditState{
		loading: true,
		reports: map[string]*hpaanalysis.AuditReport{},
	}
	m.viewMode = batchAuditView

	auditFn := m.opts.AuditFn
	namespace := m.namespace

	return m, func() tea.Msg {
		reports := make(map[string]*hpaanalysis.AuditReport)
		var lastErr error
		for _, name := range selected {
			// `name` is the selection key. It is either a bare HPA name (when a
			// namespace filter is active) or "namespace/name" (all-namespaces
			// mode). Keep it untouched as the reports map key so entries stay
			// unique and consistent with the selection; derive the namespace
			// and short name separately for the audit call.
			ns := namespace
			shortName := name
			if ns == "" {
				parts := splitNamespaceName(name)
				if len(parts) == 2 {
					ns = parts[0]
					shortName = parts[1]
				}
			}
			report, err := auditFn(m.ctx, ns, shortName)
			if err != nil {
				lastErr = err
				continue
			}
			reports[name] = report
		}
		return batchAuditMsg{reports: reports, err: lastErr}
	}
}

// handleBatchApplyKey runs batch suggest+apply for all selected HPAs.
func (m Model) handleBatchApplyKey() (tea.Model, tea.Cmd) {
	if m.viewMode != listView {
		return m, nil
	}
	if m.opts.ApplyFn == nil {
		m.err = fmt.Errorf("apply not available (no Kubernetes client)")
		return m, nil
	}
	selected := m.selectedHPANames()
	if len(selected) == 0 {
		m.err = fmt.Errorf("no HPAs selected; use space to select, a to select all")
		return m, nil
	}

	patches := collectBatchApplyPatches(selected, m.reports)
	if len(patches) == 0 {
		m.err = fmt.Errorf("no applicable patches found in %d selected HPA(s)", len(selected))
		return m, nil
	}

	if !m.batchApplyConfirm {
		m.batchApplyConfirm = true
		m.batchApplyPreview = batchApplyPreviewLines(patches)
		return m, nil
	}

	applyFn := m.opts.ApplyFn
	m.batchApplyConfirm = false
	m.batchApplyPreview = nil
	return m, func() tea.Msg {
		return executeBatchApply(m.ctx, applyFn, patches)
	}
}

// batchApplyPatchEntry pairs an HPA location with a single applicable patch.
type batchApplyPatchEntry struct {
	namespace string
	name      string
	patch     string
	title     string
}

// collectBatchApplyPatches gathers all auto-applicable patches across the selected HPAs' reports.
func collectBatchApplyPatches(selected []string, reports map[string]*hpaanalysis.StatusReport) []batchApplyPatchEntry {
	var patches []batchApplyPatchEntry
	for _, itemKey := range selected {
		report, ok := reports[itemKey]
		if !ok || report == nil {
			continue
		}
		for _, s := range report.Analysis.Suggestions {
			if s.Apply && s.Patch != "" {
				patches = append(patches, batchApplyPatchEntry{
					namespace: report.Analysis.Namespace,
					name:      report.Analysis.Name,
					patch:     s.Patch,
					title:     s.Title,
				})
			}
		}
	}
	return patches
}

func batchApplyPreviewLines(patches []batchApplyPatchEntry) []string {
	preview := make([]string, 0, len(patches))
	for _, p := range patches {
		preview = append(preview, fmt.Sprintf("%s/%s: %s", p.namespace, p.name, p.title))
	}
	return preview
}

// executeBatchApply applies each patch via applyFn and aggregates per-HPA failures into a single applyResultMsg.
func executeBatchApply(ctx context.Context, applyFn ApplyFunc, patches []batchApplyPatchEntry) tea.Msg {
	var errs []string
	for _, p := range patches {
		if err := applyFn(ctx, p.namespace, p.name, p.patch); err != nil {
			errs = append(errs, fmt.Sprintf("%s/%s: %v", p.namespace, p.name, err))
		}
	}
	if len(errs) > 0 {
		return applyResultMsg{title: fmt.Sprintf("batch: %d/%d failed", len(errs), len(patches)), err: fmt.Errorf("%s", joinStrings(errs, "; "))}
	}
	return applyResultMsg{title: fmt.Sprintf("batch: %d patches applied", len(patches)), err: nil}
}

// selectedHPANames returns the keys of selected HPAs.
func (m Model) selectedHPANames() []string {
	var names []string
	for k, v := range m.selected {
		if v {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	return names
}

// splitNamespaceName splits "namespace/name" into [namespace, name].
func splitNamespaceName(key string) []string {
	for i, ch := range key {
		if ch == '/' {
			return []string{key[:i], key[i+1:]}
		}
	}
	return []string{key}
}

// buildBatchAuditEntries converts audit reports into display entries.
func buildBatchAuditEntries(reports map[string]*hpaanalysis.AuditReport) []batchAuditEntry {
	entries := make([]batchAuditEntry, 0, len(reports))
	for _, report := range reports {
		critical := 0
		warnings := 0
		for _, f := range report.Findings {
			switch f.Severity {
			case hpaanalysis.AuditCritical:
				critical++
			case hpaanalysis.AuditWarning:
				warnings++
			}
		}
		entries = append(entries, batchAuditEntry{
			Namespace: report.Namespace,
			Name:      report.Name,
			Score:     report.Score,
			Findings:  len(report.Findings),
			Critical:  critical,
			Warnings:  warnings,
			Summary:   report.Summary,
		})
	}
	return entries
}

// joinStrings concatenates strings with a separator.
func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += sep + s
	}
	return result
}
