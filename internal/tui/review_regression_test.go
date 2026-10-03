package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func TestSimulationUsesFetchedHPAThroughMetricInput(t *testing.T) {
	window := int32(90)
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(1, 10), testutil.WithReplicas(2, 2), testutil.WithResourceMetric("cpu", 60, 70), testutil.WithBehavior(&autoscalingv2.HorizontalPodAutoscalerBehavior{ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window}}))
	m := NewModel(testutil.NewFakeClient(hpa), "default", Options{})
	fetched := fetchHPAs(m)().(fetchResultMsg)
	updated, _ := m.Update(fetched)
	m = updated.(Model)
	m.viewMode = detailView
	m, _ = pressTUIKey(t, m, "s")
	if m.simState == nil || !reflect.DeepEqual(m.simState.hpa.Spec, hpa.Spec) || !reflect.DeepEqual(m.simState.hpa.Status, hpa.Status) {
		t.Fatal("original HPA information lost")
	}
	if m.simState.hpa == fetched.hpas["default/web"] {
		t.Fatal("simulation shares mutable original")
	}
	m, _ = pressTUIKey(t, m, "M")
	for _, r := range "cpu=80%" {
		m, _ = pressTUIKey(t, m, string(r))
	}
	_, cmd := pressTUIKey(t, m, "enter")
	if cmd == nil {
		t.Fatal("missing simulation command")
	}
	result := cmd().(simResultMsg)
	if result.err != nil || result.result == nil {
		t.Fatalf("metric simulation failed: %+v", result)
	}
	if !reflect.DeepEqual(fetched.hpas["default/web"], hpa) {
		t.Fatal("simulation mutated fetched HPA")
	}
}

func TestHistoryStaysAnchoredAfterRefreshAndRemoval(t *testing.T) {
	m := detailModel(Options{})
	m.viewMode = historyView
	m.historyState = &historyState{key: "default/web", snapshots: []hpaanalysis.TimelineSnapshot{{Current: 2, Desired: 2}}}
	items := []hpaanalysis.ListItem{{Namespace: "default", Name: "api"}, {Namespace: "default", Name: "web"}}
	newer, _ := m.Update(fetchResultMsg{items: items, reports: m.reports})
	m = newer.(Model)
	got := m.renderHistoryView()
	if !strings.Contains(got, "HPA History: default/web") || strings.Contains(got, "HPA History: default/api") {
		t.Fatalf("history relabeled: %s", got)
	}
	m.items = nil
	if got = m.renderHistoryView(); !strings.Contains(got, "default/web") || !strings.Contains(got, "no longer available") {
		t.Fatalf("missing-target message: %s", got)
	}
}

func TestBatchAuditRetainsPartialSuccessAndScrolls(t *testing.T) {
	m := detailModel(Options{AuditFn: func(_ context.Context, ns, name string) (*audit.Report, error) {
		if name == "bad" {
			return nil, errors.New("not found")
		}
		return &audit.Report{Namespace: ns, Name: name}, nil
	}})
	m.viewMode = listView
	m.selected = map[string]bool{"default/web": true, "default/bad": true}
	m, cmd := pressTUIKey(t, m, "B")
	if cmd == nil {
		t.Fatal("missing audit")
	}
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if len(m.batchAuditState.results) != 1 || m.batchAuditState.err == nil {
		t.Fatalf("partial results lost: %+v", m.batchAuditState)
	}
	if got := m.renderBatchAuditView(); !strings.Contains(got, "web") || !strings.Contains(got, "bad") || !strings.Contains(got, "Audited: 1") {
		t.Fatalf("partial result not visible: %s", got)
	}
	m.batchAuditState.update(batchAuditMsg{reports: map[string]*audit.Report{"default/web": {}}})
	if m.batchAuditState.err != nil {
		t.Fatal("successful retry retained error")
	}
	m.height = 13
	m.batchAuditState.results = nil
	for i := range 12 {
		m.batchAuditState.results = append(m.batchAuditState.results, batchAuditEntry{Namespace: "default", Name: fmt.Sprintf("row-%02d", i)})
	}
	before := m
	for range 20 {
		m, _ = pressTUIKey(t, m, "j")
	}
	if m.batchAuditState.scrollPos != 11 || before.batchAuditState.scrollPos != 0 {
		t.Fatal("scroll or model isolation failed")
	}
	if got := m.renderBatchAuditView(); !strings.Contains(got, "row-11") || strings.Contains(got, "row-00") {
		t.Fatalf("last page inaccessible: %s", got)
	}
	for range 20 {
		m, _ = pressTUIKey(t, m, "k")
	}
	if m.batchAuditState.scrollPos != 0 {
		t.Fatal("negative scroll")
	}
}

func BenchmarkUpdateLargeFleet(b *testing.B) {
	for _, size := range []int{1000, 10000} {
		for _, fullCopy := range []bool{true, false} {
			b.Run(fmt.Sprintf("%d/full-copy=%t", size, fullCopy), func(b *testing.B) {
				m := NewModel(nil, "default", Options{})
				m.items = make([]hpaanalysis.ListItem, size)
				m.replicaHistory = make(map[string][]float64, size)
				for i := range size {
					name := fmt.Sprintf("hpa-%d", i)
					m.items[i] = hpaanalysis.ListItem{Namespace: "default", Name: name}
					m.replicaHistory["default/"+name] = []float64{1, 2, 3}
				}
				msg := tea.KeyPressMsg{Code: tea.KeyDown}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if fullCopy {
						copyModel := m.clone()
						_, _ = copyModel.updateKeyMsg(msg)
					} else {
						_, _ = m.Update(msg)
					}
				}
			})
		}
	}
}

func TestIdleKEDATriggerIsNotDisplayedAsFailure(t *testing.T) {
	got := tuiTriggerStatusBadge("Inactive")
	if !strings.Contains(got, "idle") || strings.Contains(got, "✗") {
		t.Fatalf("idle trigger shown as failure: %q", got)
	}
}
