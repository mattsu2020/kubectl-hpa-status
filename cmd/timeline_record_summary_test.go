package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestRecordSummaryBoundsChangesWithoutTruncatingFile(t *testing.T) {
	const polls = 251
	summary := newRecordSummary()
	path := filepath.Join(t.TempDir(), "history.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	for i := range polls {
		record := hpaanalysis.TimelineTrace{Namespace: "default", HPAName: "web", Snapshots: []hpaanalysis.TimelineSnapshot{{Timestamp: time.Unix(int64(i), 0), Desired: int32(i)}}}
		if err := writeRecordLine(file, record); err != nil {
			t.Fatal(err)
		}
		summary.track([]hpaanalysis.TimelineTrace{record})
	}
	const key = "default/web"
	if summary.counts[key] != polls || len(summary.changes[key]) != 100 || summary.omitted[key] != 150 {
		t.Fatalf("counts=%v changes=%d omitted=%v", summary.counts, len(summary.changes[key]), summary.omitted)
	}
	if !strings.Contains(summary.changes[key][0], "0/150 -> 0/151") || !strings.Contains(summary.changes[key][99], "0/249 -> 0/250") {
		t.Fatalf("summary did not retain the latest changes: %v", summary.changes[key])
	}
	var out bytes.Buffer
	if err := syncAndWriteRecordSummary(file, &out, path, summary, time.Hour); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Recorded 251 snapshots for 1 HPAs") || !strings.Contains(out.String(), "150 earlier change(s) omitted") {
		t.Fatalf("incomplete summary: %s", out.String())
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		var trace hpaanalysis.TimelineTrace
		if err := json.Unmarshal(scanner.Bytes(), &trace); err != nil {
			t.Fatal(err)
		}
		if trace.Snapshots[0].Desired != int32(count) {
			t.Fatalf("missing snapshot %d: %+v", count, trace)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != polls {
		t.Fatalf("saved %d snapshots, want %d", count, polls)
	}
}

func TestRecordSummaryKeepsHPAsIndependentAndDropsInactiveSnapshots(t *testing.T) {
	summary := newRecordSummary()
	for i := range 151 {
		summary.track([]hpaanalysis.TimelineTrace{
			{Namespace: "z", HPAName: "web", Snapshots: []hpaanalysis.TimelineSnapshot{{Desired: int32(i)}}},
			{Namespace: "a", HPAName: "web", Snapshots: []hpaanalysis.TimelineSnapshot{{Desired: 1}}},
		})
	}
	summary.track(nil)
	if len(summary.previous) != 0 {
		t.Fatalf("retained inactive snapshots: %v", summary.previous)
	}
	summary.track([]hpaanalysis.TimelineTrace{{Namespace: "a", HPAName: "web", Snapshots: []hpaanalysis.TimelineSnapshot{{Desired: 9}}}})
	if len(summary.changes["a/web"]) != 0 || summary.omitted["z/web"] != 50 {
		t.Fatalf("mixed histories: changes=%v omitted=%v", summary.changes, summary.omitted)
	}
	summary.addChange("a/web", "reappeared")
	var out bytes.Buffer
	if err := summary.write(&out, "history.jsonl", time.Hour); err != nil {
		t.Fatal(err)
	}
	if strings.Index(out.String(), "- a/web") > strings.Index(out.String(), "- z/web") {
		t.Fatalf("unstable order: %s", out.String())
	}
}

func TestRecordSummaryPropagatesWriteErrors(t *testing.T) {
	failure := errors.New("write failed")
	for _, changed := range []bool{false, true} {
		summary := newRecordSummary()
		if changed {
			summary.addChange("default/web", "change")
			summary.omitted["default/web"] = 1
		}
		for failAt := 1; failAt <= 5; failAt++ {
			writer := &summaryFailWriter{failAt: failAt, err: failure}
			err := summary.write(writer, "history.jsonl", time.Hour)
			if writer.calls >= failAt && !errors.Is(err, failure) {
				t.Fatalf("changed=%v failAt=%d: %v", changed, failAt, err)
			}
		}
	}
}

type summaryFailWriter struct {
	calls, failAt int
	err           error
}

func (w *summaryFailWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return 0, fmt.Errorf("summary: %w", w.err)
	}
	return len(p), nil
}
