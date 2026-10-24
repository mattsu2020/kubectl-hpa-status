package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestResourceFreshnessReportsObservationFailures(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		deploymentStatus int
		selector         string
		metricsStatus    int
		metrics          string
		wantStatus       hpaanalysis.MetricFreshnessStatus
		wantWarning      string
	}{
		{"selector denied", 403, `{"app":"web"}`, 200, `{"items":[]}`, hpaanalysis.FreshnessUnknown, "cannot resolve scale target selector"},
		{"empty selector", 200, `{}`, 200, `{"items":[]}`, hpaanalysis.FreshnessUnknown, "no supported Pod selector"},
		{"metrics denied", 200, `{"app":"web"}`, 403, `{}`, hpaanalysis.FreshnessUnknown, "cannot read PodMetrics"},
		{"invalid metrics", 200, `{"app":"web"}`, 200, `{`, hpaanalysis.FreshnessUnknown, "cannot read PodMetrics"},
		{"empty metrics", 200, `{"app":"web"}`, 200, `{"items":[]}`, hpaanalysis.FreshnessUnknown, "no timestamped PodMetrics sample"},
		{"other resource only", 200, `{"app":"web"}`, 200, `{"items":[{"timestamp":"2026-10-24T12:00:00Z","window":"30s","containers":[{"name":"web","usage":{"memory":"1Mi"}}]}]}`, hpaanalysis.FreshnessUnknown, "no timestamped PodMetrics sample"},
		{"fresh metrics", 200, `{"app":"web"}`, 200, `{"items":[{"timestamp":"2026-10-24T12:00:00Z","window":"30s","containers":[{"name":"web","usage":{"cpu":"100m"}}]}]}`, hpaanalysis.FreshnessOK, ""},
		{"old metrics without window", 200, `{"app":"web"}`, 200, `{"items":[{"timestamp":"2026-10-24T11:50:00Z","containers":[{"name":"web","usage":{"cpu":"100m"}}]}]}`, hpaanalysis.FreshnessStale, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/apis/apps/v1/namespaces/default/deployments/web":
					w.WriteHeader(tt.deploymentStatus)
					_, _ = fmt.Fprintf(w, `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"default"},"spec":{"selector":{"matchLabels":%s}}}`, tt.selector)
				case "/apis/metrics.k8s.io/v1beta1/namespaces/default/pods":
					if r.URL.Query().Get("labelSelector") != "app=web" {
						t.Errorf("wrong metrics selector: %s", r.URL.RawQuery)
					}
					w.WriteHeader(tt.metricsStatus)
					_, _ = fmt.Fprint(w, tt.metrics)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			hpa := testutil.BuildHPA("default", "web", testutil.WithResourceMetric("cpu", 80, 75))
			report := hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Metrics: hpaanalysis.MetricsView{MetricFreshness: hpaanalysis.AnalyzeMetricFreshness(hpa, nil)}}}
			enrichResourceMetricSamples(context.Background(), &kube.Client{Interface: client}, hpa, &report, now)
			entry := report.Analysis.Metrics.MetricFreshness[0]
			if entry.Status != string(tt.wantStatus) {
				t.Fatalf("status=%s, want %s; entry=%+v", entry.Status, tt.wantStatus, entry)
			}
			if tt.wantWarning != "" {
				if !strings.Contains(strings.Join(report.Analysis.Actions.Warnings, "\n"), tt.wantWarning) || !strings.Contains(strings.Join(entry.Evidence, "\n"), tt.wantWarning) {
					t.Fatalf("missing failure evidence: warnings=%v entry=%+v", report.Analysis.Actions.Warnings, entry)
				}
			} else if len(report.Analysis.Actions.Warnings) != 0 || entry.LastSeen == nil {
				t.Fatalf("unexpected warnings or missing sample: warnings=%v entry=%+v", report.Analysis.Actions.Warnings, entry)
			}
		})
	}
}

func TestResourceFreshnessWarningPreservesObservedFailures(t *testing.T) {
	for _, status := range []hpaanalysis.MetricFreshnessStatus{hpaanalysis.FreshnessMissing, hpaanalysis.FreshnessStale} {
		report := hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Metrics: hpaanalysis.MetricsView{MetricFreshness: []hpaanalysis.MetricFreshness{
			{Name: "cpu", Type: "Resource", Status: string(status), Risk: "observed failure"},
			{Name: "queue", Type: "External", Status: string(hpaanalysis.FreshnessOK)},
		}}}}
		recordResourceFreshnessWarning(&report, "access denied")
		entries := report.Analysis.Metrics.MetricFreshness
		if entries[0].Status != string(status) || entries[0].Risk != "observed failure" || entries[1].Status != string(hpaanalysis.FreshnessOK) || len(entries[1].Evidence) != 0 {
			t.Fatalf("overwrote unrelated or stronger evidence: %+v", entries)
		}
	}
}
