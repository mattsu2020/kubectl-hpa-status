package hpa

import (
	"testing"
	"time"
)

func TestDetectAnomalies_TooFewSnapshots(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		snapshots []HealthSnapshot
	}{
		{name: "nil snapshots", snapshots: nil},
		{name: "empty snapshots", snapshots: []HealthSnapshot{}},
		{name: "one snapshot", snapshots: []HealthSnapshot{
			{Timestamp: now, HealthScore: 80, HealthState: "OK"},
		}},
		{name: "four snapshots", snapshots: []HealthSnapshot{
			{Timestamp: now.Add(-4 * time.Minute), HealthScore: 80, HealthState: "OK"},
			{Timestamp: now.Add(-3 * time.Minute), HealthScore: 80, HealthState: "OK"},
			{Timestamp: now.Add(-2 * time.Minute), HealthScore: 80, HealthState: "OK"},
			{Timestamp: now.Add(-1 * time.Minute), HealthScore: 80, HealthState: "OK"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectAnomalies(tt.snapshots)
			if result != nil {
				t.Errorf("DetectAnomalies() = %v, want nil", result)
			}
		})
	}
}

func TestDetectAnomalies_StableScores(t *testing.T) {
	now := time.Now()
	snapshots := make([]HealthSnapshot, 10)
	for i := 0; i < 10; i++ {
		snapshots[i] = HealthSnapshot{
			Timestamp:   now.Add(time.Duration(i) * time.Minute),
			HealthScore: 95,
			HealthState: "OK",
		}
	}

	result := DetectAnomalies(snapshots)
	if len(result) != 0 {
		t.Errorf("DetectAnomalies() returned %d anomalies for stable scores, want 0", len(result))
	}
}

func TestDetectAnomalies_SuddenDegradation(t *testing.T) {
	now := time.Now()
	snapshots := []HealthSnapshot{
		{Timestamp: now.Add(-6 * time.Minute), HealthScore: 90, HealthState: "OK"},
		{Timestamp: now.Add(-5 * time.Minute), HealthScore: 88, HealthState: "OK"},
		{Timestamp: now.Add(-4 * time.Minute), HealthScore: 85, HealthState: "OK"},
		{Timestamp: now.Add(-3 * time.Minute), HealthScore: 50, HealthState: "ERROR"},  // 35-point drop
		{Timestamp: now.Add(-2 * time.Minute), HealthScore: 48, HealthState: "ERROR"},
	}

	anomalies := DetectAnomalies(snapshots)

	var found bool
	for _, a := range anomalies {
		if a.Type == AnomalySuddenDegradation {
			found = true
			if a.ScoreBefore != 85 {
				t.Errorf("ScoreBefore = %d, want 85", a.ScoreBefore)
			}
			if a.ScoreAfter != 50 {
				t.Errorf("ScoreAfter = %d, want 50", a.ScoreAfter)
			}
			if a.Severity != "warning" {
				t.Errorf("Severity = %q, want %q", a.Severity, "warning")
			}
			if a.CauseEstimate == "" {
				t.Error("CauseEstimate should not be empty")
			}
			if a.Remediation == "" {
				t.Error("Remediation should not be empty")
			}
		}
	}
	if !found {
		t.Error("expected sudden degradation anomaly to be detected")
	}
}

func TestDetectAnomalies_SuddenDegradation_Critical(t *testing.T) {
	now := time.Now()
	snapshots := []HealthSnapshot{
		{Timestamp: now.Add(-5 * time.Minute), HealthScore: 95, HealthState: "OK"},
		{Timestamp: now.Add(-4 * time.Minute), HealthScore: 93, HealthState: "OK"},
		{Timestamp: now.Add(-3 * time.Minute), HealthScore: 90, HealthState: "OK"},
		{Timestamp: now.Add(-2 * time.Minute), HealthScore: 40, HealthState: "ERROR"},  // 50-point drop
		{Timestamp: now.Add(-1 * time.Minute), HealthScore: 38, HealthState: "ERROR"},
	}

	anomalies := DetectAnomalies(snapshots)

	var found bool
	for _, a := range anomalies {
		if a.Type == AnomalySuddenDegradation {
			found = true
			if a.Severity != "critical" {
				t.Errorf("Severity = %q, want %q for 50-point drop", a.Severity, "critical")
			}
			if a.ScoreBefore != 90 {
				t.Errorf("ScoreBefore = %d, want 90", a.ScoreBefore)
			}
			if a.ScoreAfter != 40 {
				t.Errorf("ScoreAfter = %d, want 40", a.ScoreAfter)
			}
		}
	}
	if !found {
		t.Error("expected critical sudden degradation anomaly")
	}
}

func TestDetectAnomalies_SuddenDegradation_OutsideTimeWindow(t *testing.T) {
	now := time.Now()
	// Drop of 30 points but more than 10 minutes apart.
	snapshots := []HealthSnapshot{
		{Timestamp: now.Add(-30 * time.Minute), HealthScore: 90, HealthState: "OK"},
		{Timestamp: now.Add(-25 * time.Minute), HealthScore: 88, HealthState: "OK"},
		{Timestamp: now.Add(-20 * time.Minute), HealthScore: 85, HealthState: "OK"},
		{Timestamp: now.Add(-5 * time.Minute), HealthScore: 55, HealthState: "ERROR"},  // 30-point drop but 15min gap
		{Timestamp: now, HealthScore: 53, HealthState: "ERROR"},
	}

	anomalies := DetectAnomalies(snapshots)
	for _, a := range anomalies {
		if a.Type == AnomalySuddenDegradation {
			t.Error("sudden degradation should not be detected when drop is outside 10-minute window")
		}
	}
}

func TestDetectAnomalies_StuckState(t *testing.T) {
	now := time.Now()
	// 25 snapshots all within 2 points of each other.
	snapshots := make([]HealthSnapshot, 25)
	for i := 0; i < 25; i++ {
		snapshots[i] = HealthSnapshot{
			Timestamp:   now.Add(time.Duration(i) * time.Minute),
			HealthScore: 80,
			HealthState: "OK",
		}
	}

	anomalies := DetectAnomalies(snapshots)

	var found bool
	for _, a := range anomalies {
		if a.Type == AnomalyStuckState {
			found = true
			if a.Severity != "info" {
				t.Errorf("Severity = %q, want %q", a.Severity, "info")
			}
			if a.Duration == "" {
				t.Error("Duration should not be empty")
			}
			if a.CauseEstimate == "" {
				t.Error("CauseEstimate should not be empty")
			}
		}
	}
	if !found {
		t.Error("expected stuck state anomaly to be detected")
	}
}

func TestDetectAnomalies_StuckState_WithinThreshold(t *testing.T) {
	now := time.Now()
	// 25 snapshots with scores varying by at most 1 (within threshold of 2).
	snapshots := make([]HealthSnapshot, 25)
	for i := 0; i < 25; i++ {
		score := 80 + i%2 // alternates between 80 and 81
		snapshots[i] = HealthSnapshot{
			Timestamp:   now.Add(time.Duration(i) * time.Minute),
			HealthScore: score,
			HealthState: "OK",
		}
	}

	anomalies := DetectAnomalies(snapshots)

	var found bool
	for _, a := range anomalies {
		if a.Type == AnomalyStuckState {
			found = true
		}
	}
	if !found {
		t.Error("expected stuck state anomaly for scores within 2-point threshold")
	}
}

func TestDetectAnomalies_StuckState_NotEnoughConsecutive(t *testing.T) {
	now := time.Now()
	// 19 identical scores followed by a change - not enough for stuck state.
	snapshots := make([]HealthSnapshot, 20)
	for i := 0; i < 19; i++ {
		snapshots[i] = HealthSnapshot{
			Timestamp:   now.Add(time.Duration(i) * time.Minute),
			HealthScore: 80,
			HealthState: "OK",
		}
	}
	snapshots[19] = HealthSnapshot{
		Timestamp:   now.Add(19 * time.Minute),
		HealthScore: 50,
		HealthState: "ERROR",
	}

	anomalies := DetectAnomalies(snapshots)
	for _, a := range anomalies {
		if a.Type == AnomalyStuckState {
			t.Error("stuck state should not be detected with only 19 consecutive identical scores")
		}
	}
}

func TestDetectAnomalies_OscillationEscalation(t *testing.T) {
	now := time.Now()

	// First 10 snapshots: stable around 90.
	// Last 10 snapshots: oscillating wildly.
	snapshots := make([]HealthSnapshot, 20)
	for i := 0; i < 10; i++ {
		snapshots[i] = HealthSnapshot{
			Timestamp:   now.Add(time.Duration(i) * time.Minute),
			HealthScore: 90,
			HealthState: "OK",
		}
	}
	for i := 10; i < 20; i++ {
		// Alternate between high and low scores.
		score := 90
		if i%2 == 0 {
			score = 50
		}
		snapshots[i] = HealthSnapshot{
			Timestamp:   now.Add(time.Duration(i) * time.Minute),
			HealthScore: score,
			HealthState: "LIMITED",
		}
	}

	anomalies := DetectAnomalies(snapshots)

	var found bool
	for _, a := range anomalies {
		if a.Type == AnomalyOscillationEscalation {
			found = true
			if a.Severity != "warning" {
				t.Errorf("Severity = %q, want %q", a.Severity, "warning")
			}
			if a.CauseEstimate == "" {
				t.Error("CauseEstimate should not be empty")
			}
			if a.Remediation == "" {
				t.Error("Remediation should not be empty")
			}
		}
	}
	if !found {
		t.Error("expected oscillation escalation anomaly to be detected")
	}
}

func TestDetectAnomalies_OscillationEscalation_Stable(t *testing.T) {
	now := time.Now()

	// All 20 snapshots stable - no oscillation.
	snapshots := make([]HealthSnapshot, 20)
	for i := 0; i < 20; i++ {
		snapshots[i] = HealthSnapshot{
			Timestamp:   now.Add(time.Duration(i) * time.Minute),
			HealthScore: 90,
			HealthState: "OK",
		}
	}

	anomalies := DetectAnomalies(snapshots)
	for _, a := range anomalies {
		if a.Type == AnomalyOscillationEscalation {
			t.Error("oscillation escalation should not be detected for stable scores")
		}
	}
}

func TestDetectAnomalies_OscillationEscalation_TooFewSnapshots(t *testing.T) {
	now := time.Now()

	// Only 15 snapshots - not enough for oscillation detection (needs 20).
	snapshots := make([]HealthSnapshot, 15)
	for i := 0; i < 15; i++ {
		snapshots[i] = HealthSnapshot{
			Timestamp:   now.Add(time.Duration(i) * time.Minute),
			HealthScore: 90,
			HealthState: "OK",
		}
	}

	anomalies := DetectAnomalies(snapshots)
	for _, a := range anomalies {
		if a.Type == AnomalyOscillationEscalation {
			t.Error("oscillation escalation should not be detected with fewer than 20 snapshots")
		}
	}
}

func TestDetectAnomalies_InputNotMutated(t *testing.T) {
	now := time.Now()
	snapshots := []HealthSnapshot{
		{Timestamp: now.Add(-4 * time.Minute), HealthScore: 90, HealthState: "OK"},
		{Timestamp: now.Add(-3 * time.Minute), HealthScore: 88, HealthState: "OK"},
		{Timestamp: now.Add(-2 * time.Minute), HealthScore: 50, HealthState: "ERROR"},
		{Timestamp: now.Add(-1 * time.Minute), HealthScore: 48, HealthState: "ERROR"},
		{Timestamp: now, HealthScore: 47, HealthState: "ERROR"},
	}

	original := make([]HealthSnapshot, len(snapshots))
	copy(original, snapshots)

	_ = DetectAnomalies(snapshots)

	for i := range snapshots {
		if snapshots[i] != original[i] {
			t.Errorf("snapshot[%d] was mutated: got %+v, want %+v", i, snapshots[i], original[i])
		}
	}
}
