package backtest

import (
	"bytes"
	"strings"
	"testing"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"k8s.io/utils/ptr"
)

func TestRun_StabilizationReducesFlappingAndRanksHigher(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	// Oscillating demand every minute across 6 snapshots.
	snaps := []hpaanalysis.TimelineSnapshot{
		{Timestamp: t0, Current: 4, Desired: 4},
		{Timestamp: t0.Add(1 * time.Minute), Current: 4, Desired: 8},
		{Timestamp: t0.Add(2 * time.Minute), Current: 8, Desired: 3},
		{Timestamp: t0.Add(3 * time.Minute), Current: 3, Desired: 8},
		{Timestamp: t0.Add(4 * time.Minute), Current: 8, Desired: 3},
		{Timestamp: t0.Add(5 * time.Minute), Current: 3, Desired: 8},
	}

	report, err := Run(Input{
		Trace: hpaanalysis.TimelineTrace{
			Namespace: "prod",
			HPAName:   "api",
			Interval:  time.Minute,
			Snapshots: snaps,
		},
		Source: "record",
		Candidates: []CandidateSpec{
			{
				Name:                          "no-stabilization",
				MinReplicas:                   ptr.To(int32(2)),
				MaxReplicas:                   10,
				ScaleDownStabilizationSeconds: 0,
			},
			{
				Name:                          "stabilized-5m",
				MinReplicas:                   ptr.To(int32(2)),
				MaxReplicas:                   10,
				ScaleDownStabilizationSeconds: 300,
			},
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.Confidence != ConfidenceLow {
		t.Errorf("expected low confidence when no metricValues present, got %q", report.Confidence)
	}
	if report.BestCandidate != "stabilized-5m" {
		t.Fatalf("expected stabilized-5m to rank first, got %q (candidates=%+v)", report.BestCandidate, report.Candidates)
	}
	if report.Candidates[0].Rank != 1 || report.Candidates[0].Summary.DirectionFlips >= report.Candidates[1].Summary.DirectionFlips {
		t.Errorf("expected stabilized candidate to have fewer flips: %+v", report.Candidates)
	}
}

func TestRun_MetricValuesRecomputeAndPrescaleEliminatesUnderProvision(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 5, 1, 8, 55, 0, 0, time.UTC)
	snaps := []hpaanalysis.TimelineSnapshot{
		{
			Timestamp: t0,
			Current:   2,
			Desired:   2,
			MetricValues: []hpaanalysis.MetricReading{
				{Type: "Resource", Name: "cpu", Value: 50, Target: 70, Unit: "%"},
			},
		},
		{
			Timestamp: t0.Add(5 * time.Minute), // 09:00 spike
			Current:   2,
			Desired:   4,
			MetricValues: []hpaanalysis.MetricReading{
				{Type: "Resource", Name: "cpu", Value: 140, Target: 70, Unit: "%"},
			},
		},
		{
			Timestamp: t0.Add(10 * time.Minute),
			Current:   4,
			Desired:   4,
			MetricValues: []hpaanalysis.MetricReading{
				{Type: "Resource", Name: "cpu", Value: 70, Target: 70, Unit: "%"},
			},
		},
	}

	report, err := Run(Input{
		Trace: hpaanalysis.TimelineTrace{
			Namespace: "prod",
			HPAName:   "web",
			Interval:  5 * time.Minute,
			Snapshots: snaps,
		},
		Candidates: []CandidateSpec{
			{
				Name:        "tight-cap",
				MinReplicas: ptr.To(int32(2)),
				MaxReplicas: 3,
			},
			{
				Name:              "prescale-50pct",
				MinReplicas:       ptr.To(int32(2)),
				MaxReplicas:       10,
				TargetUtilization: ptr.To(int32(50)),
				PrescaleWindows: []PrescaleWindow{
					{StartMinute: 8 * 60, EndMinute: 10 * 60, MinReplicas: 6},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.Confidence != ConfidenceHigh {
		t.Errorf("expected high confidence when all snapshots have metrics, got %q", report.Confidence)
	}
	if report.BestCandidate != "prescale-50pct" {
		t.Fatalf("expected prescale-50pct to win, got %q", report.BestCandidate)
	}
	best := report.Candidates[0]
	if best.Summary.UnderProvisionedSnapshots != 0 {
		t.Errorf("expected 0 under-provisioned snapshots for prescale-50pct, got %d", best.Summary.UnderProvisionedSnapshots)
	}
	if best.Summary.PeakReplicas < 6 {
		t.Errorf("expected prescale window to lift peak >= 6, got %d", best.Summary.PeakReplicas)
	}

	var textBuf, mdBuf bytes.Buffer
	if err := WriteText(&textBuf, report); err != nil {
		t.Fatalf("WriteText failed: %v", err)
	}
	if !strings.Contains(textBuf.String(), "prescale-50pct") {
		t.Errorf("expected candidate name in text output:\n%s", textBuf.String())
	}
	if err := WriteMarkdown(&mdBuf, report); err != nil {
		t.Fatalf("WriteMarkdown failed: %v", err)
	}
	if !strings.Contains(mdBuf.String(), "| 1 | prescale-50pct |") {
		t.Errorf("expected ranked markdown row:\n%s", mdBuf.String())
	}
}

func TestRun_ValidationErrors(t *testing.T) {
	t.Parallel()

	snap := []hpaanalysis.TimelineSnapshot{{Timestamp: time.Now(), Current: 2, Desired: 2}}
	cases := []struct {
		name  string
		input Input
	}{
		{
			name:  "empty trace",
			input: Input{Candidates: []CandidateSpec{{MaxReplicas: 5}}},
		},
		{
			name:  "no candidates",
			input: Input{Trace: hpaanalysis.TimelineTrace{Snapshots: snap}},
		},
		{
			name: "min exceeds max",
			input: Input{
				Trace:      hpaanalysis.TimelineTrace{Snapshots: snap},
				Candidates: []CandidateSpec{{MinReplicas: ptr.To(int32(6)), MaxReplicas: 4}},
			},
		},
		{
			name: "stabilization above 3600",
			input: Input{
				Trace:      hpaanalysis.TimelineTrace{Snapshots: snap},
				Candidates: []CandidateSpec{{MaxReplicas: 4, ScaleDownStabilizationSeconds: 3601}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Run(tc.input); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestRunMixedMetricCPUTargetAndPrescaleBounds(t *testing.T) {
	input := Input{
		Trace: hpaanalysis.TimelineTrace{Snapshots: []hpaanalysis.TimelineSnapshot{{
			Current: 4, Desired: 4,
			MetricValues: []hpaanalysis.MetricReading{
				{Type: "Resource", Name: "cpu", Value: 25, Target: 50, Unit: "%"},
				{Type: "Resource", Name: "memory", Value: 80, Target: 80, Unit: "%"},
			},
		}}},
		Candidates: []CandidateSpec{{MaxReplicas: 10, TargetUtilization: ptr.To(int32(50))}},
	}
	report, err := Run(input)
	if err != nil {
		t.Fatal(err)
	}
	if peak := report.Candidates[0].Summary.PeakReplicas; peak != 4 {
		t.Fatalf("peak = %d, want 4", peak)
	}
	input.Candidates[0].MaxReplicas = 3
	input.Candidates[0].PrescaleWindows = []PrescaleWindow{{StartMinute: 540, EndMinute: 600, MinReplicas: 6}}
	if _, err := Run(input); err == nil || !strings.Contains(err.Error(), "prescale minReplicas") {
		t.Fatalf("expected invalid prescale error, got %v", err)
	}
}
