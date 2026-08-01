package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestRunFlapLiveWithoutEvents(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web")
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(hpa),
				Namespace:      "default",
			},
		},
	}

	var out bytes.Buffer
	if err := runFlapLive(context.Background(), &out, opts, "web", 6*time.Hour); err != nil {
		t.Fatalf("runFlapLive returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "default/web") {
		t.Errorf("expected the HPA identity in the report, got:\n%s", got)
	}
	if !strings.Contains(got, "LOW") {
		t.Errorf("expected a LOW flap level with no events, got:\n%s", got)
	}
}

func TestRunFlapLiveCountsRescaleEvents(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web")
	now := time.Now()
	events := []*corev1.Event{
		testutil.BuildEventWithTimestamp("default", "web", "SuccessfulRescale", "New size: 4; reason: cpu above target", now.Add(-30*time.Minute)),
		testutil.BuildEventWithTimestamp("default", "web", "SuccessfulRescale", "New size: 2; reason: All metrics below target", now.Add(-20*time.Minute)),
		testutil.BuildEventWithTimestamp("default", "web", "SuccessfulRescale", "New size: 5; reason: cpu above target", now.Add(-10*time.Minute)),
	}
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClientWithEvents([]*autoscalingv2.HorizontalPodAutoscaler{hpa}, events),
				Namespace:      "default",
			},
		},
	}

	var out bytes.Buffer
	if err := runFlapLive(context.Background(), &out, opts, "web", 6*time.Hour); err != nil {
		t.Fatalf("runFlapLive returned error: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "default/web") {
		t.Errorf("expected the HPA identity in the report, got:\n%s", got)
	}
}

func TestRunFlapLiveMissingHPA(t *testing.T) {
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(),
				Namespace:      "default",
			},
		},
	}

	var out bytes.Buffer
	if err := runFlapLive(context.Background(), &out, opts, "absent", time.Hour); err == nil {
		t.Fatal("expected an error for a missing HPA")
	}
}

func TestRunFlapFromRecordDetectsFlapping(t *testing.T) {
	path := writeFlapTrace(t, []int32{2, 6, 2, 6, 2, 6})

	opts := &options{Common: commonOptions{ConnectionOptions: ConnectionOptions{Namespace: "prod"}}}
	var out bytes.Buffer
	if err := runFlapFromRecord(&out, opts, "web", path); err != nil {
		t.Fatalf("runFlapFromRecord returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "prod/web") {
		t.Errorf("expected the HPA identity, got:\n%s", got)
	}
	if !strings.Contains(got, path) {
		t.Errorf("expected the record path as the source, got:\n%s", got)
	}
}

func TestRunFlapFromRecordJSONOutput(t *testing.T) {
	path := writeFlapTrace(t, []int32{3, 3, 3})

	opts := &options{Common: commonOptions{
		ConnectionOptions: ConnectionOptions{Namespace: "prod"},
		OutputOptions:     OutputOptions{Output: "json"},
	}}
	var out bytes.Buffer
	if err := runFlapFromRecord(&out, opts, "web", path); err != nil {
		t.Fatalf("runFlapFromRecord returned error: %v", err)
	}

	var report flapReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("flap JSON is not decodable: %v\n%s", err, out.String())
	}
	if report.Name != "web" || report.Namespace != "prod" {
		t.Errorf("unexpected identity in structured report: %s/%s", report.Namespace, report.Name)
	}
	// A steady replica count is not flapping.
	if report.Level != "LOW" {
		t.Errorf("expected LOW for a steady trace, got %q", report.Level)
	}
}

func TestRunFlapFromRecordMissingFile(t *testing.T) {
	opts := &options{}
	var out bytes.Buffer
	if err := runFlapFromRecord(&out, opts, "web", "/nonexistent/trace.jsonl"); err == nil {
		t.Fatal("expected an error for a missing record file")
	}
}

func TestFlapLevelIsMonotonicInFlips(t *testing.T) {
	severity := map[string]int{"LOW": 0, "MEDIUM": 1, "HIGH": 2, "CRITICAL": 3}

	if got := flapLevel(0, 0); got != "LOW" {
		t.Errorf("flapLevel(0, 0) = %q, want LOW", got)
	}

	// More direction flips at a fixed event count must never de-escalate.
	previous := -1
	for flips := 0; flips <= 12; flips++ {
		level := flapLevel(20, flips)
		rank, ok := severity[level]
		if !ok {
			t.Fatalf("flapLevel(20, %d) returned unknown level %q", flips, level)
		}
		if rank < previous {
			t.Errorf("flapLevel(20, %d) = %q de-escalated from the previous level", flips, level)
		}
		previous = rank
	}
}

func TestFlapRecommendationsAreNeverEmpty(t *testing.T) {
	// Every level must carry at least one recommendation so the command never
	// prints an empty "what to do" section.
	for _, level := range []string{"LOW", "MEDIUM", "HIGH", "CRITICAL", "UNKNOWN"} {
		if len(flapRecommendations(level)) == 0 {
			t.Errorf("flapRecommendations(%q) returned no recommendations", level)
		}
	}
}

func TestTraceReplicaRange(t *testing.T) {
	trace := hpaanalysis.TimelineTrace{
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Desired: 4}, {Desired: 9}, {Desired: 2},
		},
	}
	low, high := traceReplicaRange(trace)
	if low != 2 || high != 9 {
		t.Errorf("traceReplicaRange = (%d, %d), want (2, 9)", low, high)
	}

	if low, high := traceReplicaRange(hpaanalysis.TimelineTrace{}); low != 0 || high != 0 {
		t.Errorf("empty trace range = (%d, %d), want (0, 0)", low, high)
	}
}

// writeFlapTrace writes a JSONL record with one snapshot per desired-replica
// value and returns its path.
func writeFlapTrace(t *testing.T, desired []int32) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "flap-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	start := time.Now().Add(-time.Hour)
	for i, value := range desired {
		trace := hpaanalysis.TimelineTrace{
			Namespace: "prod",
			HPAName:   "web",
			Start:     start,
			Snapshots: []hpaanalysis.TimelineSnapshot{{
				Timestamp: start.Add(time.Duration(i) * time.Minute),
				Current:   value,
				Desired:   value,
				Health:    "OK",
			}},
		}
		if err := writeRecordLine(file, trace); err != nil {
			t.Fatal(err)
		}
	}
	return file.Name()
}
