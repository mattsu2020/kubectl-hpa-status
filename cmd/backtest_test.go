package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/promhistory"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/backtest"
)

func sampleBacktestTrace(ns, name string) hpaanalysis.TimelineTrace {
	t0 := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	return hpaanalysis.TimelineTrace{
		Namespace: ns,
		HPAName:   name,
		Start:     t0,
		Interval:  time.Minute,
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{
				Timestamp: t0,
				Current:   2,
				Desired:   2,
				MetricValues: []hpaanalysis.MetricReading{
					{Type: "Resource", Name: "cpu", Value: 50, Target: 70, Unit: "%"},
				},
			},
			{
				Timestamp: t0.Add(1 * time.Minute),
				Current:   2,
				Desired:   6,
				MetricValues: []hpaanalysis.MetricReading{
					{Type: "Resource", Name: "cpu", Value: 150, Target: 70, Unit: "%"},
				},
			},
			{
				Timestamp: t0.Add(2 * time.Minute),
				Current:   6,
				Desired:   2,
				MetricValues: []hpaanalysis.MetricReading{
					{Type: "Resource", Name: "cpu", Value: 30, Target: 70, Unit: "%"},
				},
			},
			{
				Timestamp: t0.Add(3 * time.Minute),
				Current:   2,
				Desired:   6,
				MetricValues: []hpaanalysis.MetricReading{
					{Type: "Resource", Name: "cpu", Value: 150, Target: 70, Unit: "%"},
				},
			},
		},
	}
}

func TestAlphaBacktest_FromRecordPresetsAndJSON(t *testing.T) {
	dir := t.TempDir()
	recPath := filepath.Join(dir, "record.jsonl")
	raw, err := json.Marshal(sampleBacktestTrace("prod", "web"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{Namespace: "prod"},
			OutputOptions:     OutputOptions{Output: "json"},
		},
	}
	cmd := newBacktestCommand(opts)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--from-record", recPath, "web"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("backtest command failed: %v", err)
	}

	var report backtest.Report
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("invalid backtest JSON: %v\noutput:\n%s", err, buf.String())
	}
	if report.Name != "web" || len(report.Candidates) != 4 {
		t.Fatalf("unexpected report: name=%q candidates=%d", report.Name, len(report.Candidates))
	}
	if report.BestCandidate == "" {
		t.Fatal("expected BestCandidate to be populated")
	}
}

func TestAlphaBacktest_CandidateFileAndPrescaleMarkdown(t *testing.T) {
	dir := t.TempDir()
	recPath := filepath.Join(dir, "record.jsonl")
	raw, err := json.Marshal(sampleBacktestTrace("prod", "web"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	candPath := filepath.Join(dir, "candidate.yaml")
	candYAML := `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: web
spec:
  minReplicas: 2
  maxReplicas: 12
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 60
`
	if err := os.WriteFile(candPath, []byte(candYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{Namespace: "prod"},
			OutputOptions:     OutputOptions{Output: "markdown"},
		},
	}
	cmd := newBacktestCommand(opts)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{
		"--from-record", recPath,
		"--candidate", candPath,
		"--scale-down-stabilization", "600",
		"--prescale", "08:30-10:00=6",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("backtest command failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "# HPA Backtest: prod/web") || !strings.Contains(out, "candidate") || !strings.Contains(out, "inline-candidate") {
		t.Fatalf("unexpected markdown output:\n%s", out)
	}
}

func TestTimelineAndAnalyzeRecord_FromPrometheus(t *testing.T) {
	orig := promLoadTrace
	defer func() { promLoadTrace = orig }()

	promLoadTrace = func(_ context.Context, cfg promhistory.QueryConfig) (*promhistory.ImportResult, error) {
		return &promhistory.ImportResult{
			Trace:      sampleBacktestTrace(cfg.Namespace, cfg.HPAName),
			Provenance: "prometheus-query-range",
			Confidence: promhistory.ConfidenceMedium,
		}, nil
	}

	t.Run("timeline --from-prometheus", func(t *testing.T) {
		var buf bytes.Buffer
		opts := &options{Common: commonOptions{ConnectionOptions: ConnectionOptions{Namespace: "prod"}}}
		cmd := newTimelineCommand(opts)
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"web", "--from-prometheus", "--prometheus-url", "http://prom.local:9090", "--since", "2h"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("timeline --from-prometheus failed: %v", err)
		}
		if !strings.Contains(buf.String(), "HPA Timeline: prod/web") {
			t.Fatalf("unexpected timeline output:\n%s", buf.String())
		}
	})

	t.Run("analyze-record --from-prometheus", func(t *testing.T) {
		var buf bytes.Buffer
		opts := &options{Common: commonOptions{ConnectionOptions: ConnectionOptions{Namespace: "prod"}}}
		cmd := newAnalyzeRecordCommand(opts)
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"--from-prometheus", "--prometheus-url", "http://prom.local:9090", "--name", "web"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("analyze-record --from-prometheus failed: %v", err)
		}
		if !strings.Contains(buf.String(), "prod/web") {
			t.Fatalf("unexpected analyze-record output:\n%s", buf.String())
		}
	})

	t.Run("alpha backtest --from-prometheus", func(t *testing.T) {
		var buf bytes.Buffer
		opts := &options{Common: commonOptions{ConnectionOptions: ConnectionOptions{Namespace: "prod"}}}
		cmd := newBacktestCommand(opts)
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"web", "--from-prometheus", "--prometheus-url", "http://prom.local:9090"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("alpha backtest --from-prometheus failed: %v", err)
		}
		if !strings.Contains(buf.String(), "HPA Backtest: prod/web") {
			t.Fatalf("unexpected backtest output:\n%s", buf.String())
		}
	})
}

func TestAlphaRegisteredBacktestCommand(t *testing.T) {
	alpha := newAlphaCommand(&options{})
	found := false
	for _, c := range alpha.Commands() {
		if c.Name() == "backtest" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected backtest to be registered under alpha")
	}
}
