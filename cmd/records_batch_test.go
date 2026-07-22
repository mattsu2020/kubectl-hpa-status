package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// This file holds the durable-record / replay smoke tests. They were split
// out of the former feature_batch_test.go grab-bag so the record/replay
// feature's tests live together next to replay_commands.go.

func TestLoadRecordedTrace_JSONL(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "hpa-history-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tmp.Close() }()

	first := hpaanalysis.TimelineTrace{
		Namespace: "default",
		HPAName:   "web",
		Start:     time.Now(),
		End:       time.Now(),
		Snapshots: []hpaanalysis.TimelineSnapshot{{Timestamp: time.Now(), Current: 2, Desired: 2, Health: "OK"}},
	}
	second := first
	second.Snapshots = []hpaanalysis.TimelineSnapshot{{Timestamp: time.Now().Add(time.Second), Current: 2, Desired: 5, Health: "LIMITED"}}
	if err := writeRecordLine(tmp, first); err != nil {
		t.Fatal(err)
	}
	if err := writeRecordLine(tmp, second); err != nil {
		t.Fatal(err)
	}

	trace, err := loadRecordedTrace(tmp.Name(), "default", "web")
	if err != nil {
		t.Fatalf("loadRecordedTrace returned error: %v", err)
	}
	if len(trace.Snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(trace.Snapshots))
	}
}

func TestRunAnalyzeRecordDetectsFlapping(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "hpa-history-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tmp.Close() }()

	trace := hpaanalysis.TimelineTrace{
		Namespace: "prod",
		HPAName:   "web",
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Desired: 2},
			{Desired: 5},
			{Desired: 3},
			{Desired: 6},
			{Desired: 3},
		},
	}
	if err := writeRecordLine(tmp, trace); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	opts := &options{}
	if err := runAnalyzeRecord(&buf, opts, tmp.Name(), "flapping"); err != nil {
		t.Fatalf("runAnalyzeRecord returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Detected HPA flapping") || !strings.Contains(output, "scale direction alternated") {
		t.Fatalf("expected flapping analysis, got:\n%s", output)
	}
}

func TestRunFlapFromRecordDetectsReplicaRange(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "hpa-history-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tmp.Close() }()

	trace := hpaanalysis.TimelineTrace{
		Namespace: "prod",
		HPAName:   "web",
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Desired: 4},
			{Desired: 9},
			{Desired: 5},
			{Desired: 10},
		},
	}
	if err := writeRecordLine(tmp, trace); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				Namespace: "prod",
			},
		},
	}
	if err := runFlapFromRecord(&buf, opts, "web", tmp.Name()); err != nil {
		t.Fatalf("runFlapFromRecord returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Flapping Analysis: prod/web") ||
		!strings.Contains(output, "direction changes: 2") ||
		!strings.Contains(output, "replica range: 4 -> 10") {
		t.Fatalf("expected flapping report, got:\n%s", output)
	}
}
