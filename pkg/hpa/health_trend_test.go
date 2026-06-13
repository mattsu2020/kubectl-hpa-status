package hpa

import (
	"testing"
	"time"
)

func TestAnalyzeHealthTrend(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		snapshots []HealthSnapshot
		check     func(t *testing.T, result HealthTrendResult)
	}{
		{
			name:      "empty snapshots returns empty result",
			snapshots: nil,
			check: func(t *testing.T, result HealthTrendResult) {
				if len(result.Snapshots) != 0 {
					t.Error("expected empty snapshots")
				}
			},
		},
		{
			name: "stable health returns low variance",
			snapshots: []HealthSnapshot{
				{Timestamp: now.Add(-4 * time.Hour), HealthScore: 100, HealthState: "OK"},
				{Timestamp: now.Add(-3 * time.Hour), HealthScore: 100, HealthState: "OK"},
				{Timestamp: now.Add(-2 * time.Hour), HealthScore: 95, HealthState: "OK"},
				{Timestamp: now.Add(-1 * time.Hour), HealthScore: 100, HealthState: "OK"},
			},
			check: func(t *testing.T, result HealthTrendResult) {
				if result.MinScore != 95 {
					t.Errorf("MinScore = %d, want 95", result.MinScore)
				}
				if result.MaxScore != 100 {
					t.Errorf("MaxScore = %d, want 100", result.MaxScore)
				}
				if result.FlappingDetected {
					t.Error("should not detect flapping for stable health")
				}
				if result.Sparkline == "" {
					t.Error("expected sparkline output")
				}
			},
		},
		{
			name: "degrading health returns negative degradation rate",
			snapshots: []HealthSnapshot{
				{Timestamp: now.Add(-4 * time.Hour), HealthScore: 100, HealthState: "OK"},
				{Timestamp: now.Add(-3 * time.Hour), HealthScore: 80, HealthState: "LIMITED"},
				{Timestamp: now.Add(-2 * time.Hour), HealthScore: 60, HealthState: "LIMITED"},
				{Timestamp: now.Add(-1 * time.Hour), HealthScore: 40, HealthState: "ERROR"},
			},
			check: func(t *testing.T, result HealthTrendResult) {
				if result.DegradationRate >= 0 {
					t.Errorf("DegradationRate = %.2f, want negative", result.DegradationRate)
				}
				if result.MeanScore < 50 || result.MeanScore > 90 {
					t.Errorf("MeanScore = %.1f, want between 50 and 90", result.MeanScore)
				}
			},
		},
		{
			name: "flapping health is detected",
			snapshots: []HealthSnapshot{
				{Timestamp: now.Add(-7 * time.Hour), HealthScore: 100, HealthState: "OK"},
				{Timestamp: now.Add(-6 * time.Hour), HealthScore: 55, HealthState: "LIMITED"},
				{Timestamp: now.Add(-5 * time.Hour), HealthScore: 100, HealthState: "OK"},
				{Timestamp: now.Add(-4 * time.Hour), HealthScore: 55, HealthState: "LIMITED"},
				{Timestamp: now.Add(-3 * time.Hour), HealthScore: 100, HealthState: "OK"},
				{Timestamp: now.Add(-2 * time.Hour), HealthScore: 55, HealthState: "LIMITED"},
				{Timestamp: now.Add(-1 * time.Hour), HealthScore: 100, HealthState: "OK"},
			},
			check: func(t *testing.T, result HealthTrendResult) {
				if !result.FlappingDetected {
					t.Error("expected flapping to be detected")
				}
				if result.FlappingSeverity == "" {
					t.Error("expected flapping severity to be set")
				}
			},
		},
		{
			name: "snapshots are sorted by timestamp",
			snapshots: []HealthSnapshot{
				{Timestamp: now.Add(-1 * time.Hour), HealthScore: 80, HealthState: "OK"},
				{Timestamp: now.Add(-3 * time.Hour), HealthScore: 100, HealthState: "OK"},
				{Timestamp: now.Add(-2 * time.Hour), HealthScore: 90, HealthState: "OK"},
			},
			check: func(t *testing.T, result HealthTrendResult) {
				if len(result.Snapshots) != 3 {
					t.Fatalf("expected 3 snapshots, got %d", len(result.Snapshots))
				}
				if result.Snapshots[0].HealthScore != 100 {
					t.Errorf("first snapshot score = %d, want 100 (oldest)", result.Snapshots[0].HealthScore)
				}
				if result.Snapshots[2].HealthScore != 80 {
					t.Errorf("last snapshot score = %d, want 80 (newest)", result.Snapshots[2].HealthScore)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AnalyzeHealthTrend(tt.snapshots)
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestDetectFlapping(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		snapshots    []HealthSnapshot
		wantFlapping bool
		wantSeverity string
	}{
		{
			name:         "less than 3 snapshots no flapping",
			snapshots:    []HealthSnapshot{{Timestamp: now, HealthState: "OK"}},
			wantFlapping: false,
		},
		{
			name: "stable states no flapping",
			snapshots: []HealthSnapshot{
				{Timestamp: now.Add(-2 * time.Hour), HealthState: "OK"},
				{Timestamp: now.Add(-1 * time.Hour), HealthState: "OK"},
				{Timestamp: now, HealthState: "OK"},
			},
			wantFlapping: false,
		},
		{
			name: "frequent oscillation detects flapping",
			snapshots: []HealthSnapshot{
				{Timestamp: now.Add(-6 * time.Hour), HealthState: "OK"},
				{Timestamp: now.Add(-5 * time.Hour), HealthState: "LIMITED"},
				{Timestamp: now.Add(-4 * time.Hour), HealthState: "OK"},
				{Timestamp: now.Add(-3 * time.Hour), HealthState: "LIMITED"},
				{Timestamp: now.Add(-2 * time.Hour), HealthState: "OK"},
				{Timestamp: now.Add(-1 * time.Hour), HealthState: "LIMITED"},
				{Timestamp: now, HealthState: "OK"},
			},
			wantFlapping: true,
			wantSeverity: "CRITICAL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flapping, severity := DetectFlapping(tt.snapshots)
			if flapping != tt.wantFlapping {
				t.Errorf("DetectFlapping() = %v, want %v", flapping, tt.wantFlapping)
			}
			if tt.wantSeverity != "" && severity != tt.wantSeverity {
				t.Errorf("severity = %q, want %q", severity, tt.wantSeverity)
			}
		})
	}
}

func TestComputeHealthVariance(t *testing.T) {
	tests := []struct {
		name   string
		scores []int
		want   float64
	}{
		{name: "empty returns 0", scores: nil, want: 0},
		{name: "single value returns 0", scores: []int{100}, want: 0},
		{name: "all same returns 0", scores: []int{80, 80, 80}, want: 0},
		{name: "different values returns positive", scores: []int{60, 80, 100}, want: 266.67},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeHealthVariance(tt.scores)
			if tt.want == 0 && got != 0 {
				t.Errorf("ComputeHealthVariance() = %.2f, want 0", got)
			}
			if tt.want > 0 && got < tt.want-1 {
				t.Errorf("ComputeHealthVariance() = %.2f, want ~%.2f", got, tt.want)
			}
		})
	}
}

func TestFormatHealthSparkline(t *testing.T) {
	tests := []struct {
		name   string
		scores []int
		width  int
		check  func(t *testing.T, got string)
	}{
		{name: "empty returns empty", scores: nil, width: 10, check: func(t *testing.T, got string) {
			if got != "" {
				t.Errorf("expected empty, got %q", got)
			}
		}},
		{name: "single score renders one char", scores: []int{50}, width: 10, check: func(t *testing.T, got string) {
			if len(got) == 0 {
				t.Error("expected non-empty sparkline")
			}
		}},
		{name: "multiple scores renders multiple chars", scores: []int{20, 40, 60, 80, 100}, width: 5, check: func(t *testing.T, got string) {
			if len([]rune(got)) != 5 {
				t.Errorf("expected 5 chars, got %d", len([]rune(got)))
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatHealthSparkline(tt.scores, tt.width)
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}
