package hpa

import (
	"testing"
)

func fmtIntPtr(v int64) *int64  { return &v }
func fmtInt32Ptr(v int32) *int32 { return &v }

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds  int64
		expected string
	}{
		{0, "0s"},
		{-5, "0s"},
		{1, "1s"},
		{30, "30s"},
		{59, "59s"},
		{60, "1m 0s"},
		{90, "1m 30s"},
		{252, "4m 12s"},
		{300, "5m 0s"},
		{900, "15m 0s"},
		{3600, "1h 0m"},
		{5010, "1h 23m"},
		{7200, "2h 0m"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			got := FormatDuration(tc.seconds)
			if got != tc.expected {
				t.Errorf("FormatDuration(%d) = %q, want %q", tc.seconds, got, tc.expected)
			}
		})
	}
}

func TestFormatStabilizationRemaining(t *testing.T) {
	tests := []struct {
		name     string
		remaining *int64
		expected  string
	}{
		{"nil", nil, ""},
		{"zero", fmtIntPtr(0), ""},
		{"negative", fmtIntPtr(-5), ""},
		{"30 seconds", fmtIntPtr(30), "30s"},
		{"4m12s", fmtIntPtr(252), "4m 12s"},
		{"1h", fmtIntPtr(3600), "1h 0m"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatStabilizationRemaining(tc.remaining)
			if got != tc.expected {
				t.Errorf("FormatStabilizationRemaining() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestFormatStabilizationProgress(t *testing.T) {
	tests := []struct {
		name      string
		remaining *int64
		window    *int32
		expected  string
	}{
		{"nil remaining", nil, fmtInt32Ptr(300), ""},
		{"zero remaining", fmtIntPtr(0), fmtInt32Ptr(300), ""},
		{"nil window", fmtIntPtr(252), nil, "4m 12s remaining"},
		{"both present", fmtIntPtr(252), fmtInt32Ptr(300), "4m 12s remaining (of 5m 0s)"},
		{"zero window", fmtIntPtr(100), fmtInt32Ptr(0), "1m 40s remaining"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatStabilizationProgress(tc.remaining, tc.window)
			if got != tc.expected {
				t.Errorf("FormatStabilizationProgress() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestStabilizationProgressRatio(t *testing.T) {
	tests := []struct {
		name      string
		remaining *int64
		window    *int32
		expected  float64
	}{
		{"nil remaining", nil, fmtInt32Ptr(300), 0},
		{"nil window", fmtIntPtr(100), nil, 0},
		{"zero window", fmtIntPtr(100), fmtInt32Ptr(0), 0},
		{"half elapsed", fmtIntPtr(150), fmtInt32Ptr(300), 0.5},
		{"fully elapsed", fmtIntPtr(0), fmtInt32Ptr(300), 1.0},
		{"just started", fmtIntPtr(299), fmtInt32Ptr(300), 0.0033333333333333335},
		{"overshoot clamped", fmtIntPtr(-100), fmtInt32Ptr(300), 1.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StabilizationProgressRatio(tc.remaining, tc.window)
			if tc.expected == 0 && got != 0 {
				t.Errorf("StabilizationProgressRatio() = %f, want 0", got)
			} else if tc.expected > 0 && (got < tc.expected-0.01 || got > tc.expected+0.01) {
				t.Errorf("StabilizationProgressRatio() = %f, want ~%f", got, tc.expected)
			}
		})
	}
}

func TestFormatCountdownBadge(t *testing.T) {
	tests := []struct {
		name     string
		remaining *int64
		expected  string
	}{
		{"nil", nil, ""},
		{"zero", fmtIntPtr(0), ""},
		{"30 seconds", fmtIntPtr(30), "⏳ 30s"},
		{"4m12s", fmtIntPtr(252), "⏳ 4m12s"},
		{"1h23m", fmtIntPtr(4980), "⏳ 1h23m"},
		{"5m0s", fmtIntPtr(300), "⏳ 5m0s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatCountdownBadge(tc.remaining)
			if got != tc.expected {
				t.Errorf("FormatCountdownBadge() = %q, want %q", got, tc.expected)
			}
		})
	}
}
