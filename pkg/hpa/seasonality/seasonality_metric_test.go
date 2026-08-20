package seasonality

import (
	"strings"
	"testing"
	"time"
)

// cpuSample is the typed reading attached to observations in these tests.
func cpuSample(value float64) MetricSample {
	return MetricSample{Name: "cpu", Value: value, Unit: "%"}
}

// withCPU copies obs and attaches a cpu utilization sample computed by the
// per-timestamp shape.
func withCPU(obs []Observation, sample func(ts time.Time) float64) []Observation {
	out := make([]Observation, len(obs))
	for i, ob := range obs {
		ob.Metrics = []MetricSample{cpuSample(sample(ob.Timestamp))}
		out[i] = ob
	}
	return out
}

// cpuShape maps a timestamp to a cpu utilization reading that ramps from base
// to peak between 09:00 and 17:00.
func cpuShape(base, peak float64) func(time.Time) float64 {
	return func(ts time.Time) float64 {
		minute := ts.Hour()*60 + ts.Minute()
		if minute >= 9*60 && minute < 17*60 {
			return peak
		}
		return base
	}
}

// weekdayShape variants ramp only on business days. The replica form matches
// buildObservations' (day, minute) signature; the cpu form takes the full
// timestamp because withCPU attaches samples per observation.
func weekdayReplicas(base, peak int32) func(time.Time, int) int32 {
	return func(day time.Time, minute int) int32 {
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			return base
		}
		if minute >= 9*60 && minute < 17*60 {
			return peak
		}
		return base
	}
}

func weekdayCPU(base, peak float64) func(time.Time) float64 {
	return func(ts time.Time) float64 {
		if ts.Weekday() == time.Saturday || ts.Weekday() == time.Sunday {
			return base
		}
		return cpuShape(base, peak)(ts)
	}
}

// TestAnalyze_MetricSignalProfilesTypedValues verifies that auto mode prefers
// the typed metric signal when the recording carries it, while the
// recommendation levels still come from observed desiredReplicas.
func TestAnalyze_MetricSignalProfilesTypedValues(t *testing.T) {
	obs := withCPU(buildObservations(mondayUTC, 5, businessHoursShape(3, 12)), cpuShape(20, 80))
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.Detected {
		t.Fatalf("expected a detected peak, notes=%v", got.Notes)
	}
	if got.Signal != "metric:cpu" {
		t.Errorf("Signal = %q, want metric:cpu", got.Signal)
	}
	if got.SignalUnit != "%" {
		t.Errorf("SignalUnit = %q, want %%", got.SignalUnit)
	}
	if got.Baseline != 20 {
		t.Errorf("Baseline = %v, want 20 (metric space)", got.Baseline)
	}
	if got.Threshold != 30 {
		t.Errorf("Threshold = %v, want 30 (1.5x metric baseline)", got.Threshold)
	}
	if got.Peak == nil {
		t.Fatal("Detected=true but Peak is nil")
	}
	if got.Peak.PeakSignal != 80 {
		t.Errorf("PeakSignal = %v, want 80", got.Peak.PeakSignal)
	}
	if got.Peak.PeakDesired != 12 {
		t.Errorf("PeakDesired = %d, want 12 (replica levels survive metric mode)", got.Peak.PeakDesired)
	}
	if got.Recommendation == nil || got.Recommendation.MinReplicas != 12 {
		t.Fatalf("MinReplicas = %+v, want 12", got.Recommendation)
	}
}

// TestAnalyze_AutoFallsBackToReplicasOnLegacyRecords keeps legacy JSONL
// records working: no typed readings means the replica signal, unchanged.
func TestAnalyze_AutoFallsBackToReplicasOnLegacyRecords(t *testing.T) {
	obs := buildObservations(mondayUTC, 5, businessHoursShape(3, 12))
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.Detected {
		t.Fatalf("expected detection on legacy records, notes=%v", got.Notes)
	}
	if got.Signal != "replicas" {
		t.Errorf("Signal = %q, want replicas", got.Signal)
	}
	if got.SignalUnit != "" {
		t.Errorf("SignalUnit = %q, want empty", got.SignalUnit)
	}
	if got.Peak != nil && got.Peak.PeakSignal != 0 {
		t.Errorf("PeakSignal = %v, want 0 in replica mode", got.Peak.PeakSignal)
	}
}

// TestAnalyze_ForcedMetricWithoutReadingsIsReported verifies that
// SignalMetric fails loudly instead of silently profiling replicas.
func TestAnalyze_ForcedMetricWithoutReadingsIsReported(t *testing.T) {
	obs := buildObservations(mondayUTC, 5, businessHoursShape(3, 12))
	got := Analyze(obs, Options{Location: time.UTC, Signal: SignalMetric})

	if got.Detected || got.InsufficientData {
		t.Fatalf("forced metric on a legacy record is a usability report, not a detection: %+v", got)
	}
	if got.Signal != "metric" {
		t.Errorf("Signal = %q, want metric", got.Signal)
	}
	joined := strings.Join(got.Notes, " ")
	if !strings.Contains(joined, "no typed metric values") {
		t.Errorf("notes should explain the missing metric signal, got %q", joined)
	}
}

// TestAnalyze_MetricRampWithinReplicaHeadroom verifies the guard: a demand
// ramp the HPA absorbed without moving desiredReplicas gets detected but not
// recommended, because there is no replica floor to pre-scale.
func TestAnalyze_MetricRampWithinReplicaHeadroom(t *testing.T) {
	// Replicas sit flat at 5 the whole time while cpu ramps 20 -> 80.
	obs := withCPU(buildObservations(mondayUTC, 5, flatShape(5)), cpuShape(20, 80))
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.Detected {
		t.Fatalf("the metric ramp itself is real and recurring, notes=%v", got.Notes)
	}
	if got.Peak == nil || got.Peak.Start != "09:00" {
		t.Fatalf("expected the 09:00 window from the metric signal, got %+v", got.Peak)
	}
	if got.Recommendation != nil {
		t.Errorf("no pre-scale should be proposed when desiredReplicas never moved, got %+v", got.Recommendation)
	}
	joined := strings.Join(got.Notes, " ")
	if !strings.Contains(joined, "headroom") {
		t.Errorf("notes should explain why no recommendation was made, got %q", joined)
	}
}

// TestAnalyze_MetricSignalWeeklyCycle verifies that a weekday-only metric
// ramp narrows the schedule and is labeled a weekly cycle.
func TestAnalyze_MetricSignalWeeklyCycle(t *testing.T) {
	replicas := buildObservations(mondayUTC, 14, weekdayReplicas(3, 12))
	obs := withCPU(replicas, weekdayCPU(20, 80))
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.Detected {
		t.Fatalf("expected a detected weekday pattern over two weeks, notes=%v", got.Notes)
	}
	if got.Cycle != CycleWeekly {
		t.Errorf("Cycle = %q, want %q", got.Cycle, CycleWeekly)
	}
	if got.Peak == nil {
		t.Fatal("Detected=true but Peak is nil")
	}
	if len(got.Peak.Weekdays) != 5 {
		t.Errorf("Weekdays = %v, want the five business days", got.Peak.Weekdays)
	}
	if got.Recommendation == nil {
		t.Fatal("expected a recommendation: replicas do ramp on business days")
	}
	if !strings.Contains(got.Recommendation.CronExpression, "1-5") {
		t.Errorf("cron should be narrowed to business days, got %q", got.Recommendation.CronExpression)
	}
}

func TestSelectSignal(t *testing.T) {
	day := mondayUTC
	mk := func(name string, minute int, value float64, withMetric bool) Observation {
		ob := Observation{Timestamp: day.Add(time.Duration(minute) * time.Minute), Desired: 3}
		if withMetric {
			ob.Metrics = []MetricSample{{Name: name, Value: value, Unit: "%"}}
		}
		return ob
	}

	t.Run("auto requires majority coverage", func(t *testing.T) {
		obs := []Observation{
			mk("cpu", 0, 10, true), mk("cpu", 5, 12, true),
			mk("", 10, 0, false), mk("", 15, 0, false), mk("", 20, 0, false),
		}
		name, _, useMetric, usable := selectSignal(obs, SignalAuto)
		if !usable || useMetric || name != "" {
			t.Errorf("5 observations with 2 carrying cpu: got (%q, metric=%v, usable=%v), want replica fallback", name, useMetric, usable)
		}
	})

	t.Run("auto prefers the dominant name", func(t *testing.T) {
		obs := []Observation{
			mk("cpu", 0, 10, true), mk("cpu", 5, 12, true), mk("cpu", 10, 11, true),
			mk("rps", 0, 100, true),
		}
		name, _, useMetric, usable := selectSignal(obs, SignalAuto)
		if !usable || !useMetric || name != "cpu" {
			t.Errorf("cpu covers 3 of 4 observations, got (%q, metric=%v, usable=%v)", name, useMetric, usable)
		}
	})

	t.Run("forced metric breaks coverage ties lexicographically", func(t *testing.T) {
		obs := []Observation{
			mk("rps", 0, 100, true), mk("rps", 5, 90, true),
			mk("cpu", 0, 10, true), mk("cpu", 5, 12, true),
		}
		name, _, useMetric, usable := selectSignal(obs, SignalMetric)
		if !usable || !useMetric || name != "cpu" {
			t.Errorf("tie between cpu and rps must break lexicographically, got (%q, metric=%v, usable=%v)", name, useMetric, usable)
		}
	})

	t.Run("forced metric accepts sparse coverage", func(t *testing.T) {
		obs := []Observation{mk("cpu", 0, 10, true), mk("cpu", 5, 12, false)}
		name, _, useMetric, usable := selectSignal(obs, SignalMetric)
		if !usable || !useMetric || name != "cpu" {
			t.Errorf("forced metric should use the only available name, got (%q, metric=%v, usable=%v)", name, useMetric, usable)
		}
	})

	t.Run("forced metric without any reading is unusable", func(t *testing.T) {
		obs := []Observation{mk("", 0, 0, false), mk("", 5, 0, false)}
		_, _, useMetric, usable := selectSignal(obs, SignalMetric)
		if usable || !useMetric {
			t.Errorf("expected (metric=true, usable=false), got (%v, %v)", useMetric, usable)
		}
	})

	t.Run("replicas ignores recorded metrics", func(t *testing.T) {
		obs := []Observation{mk("cpu", 0, 10, true), mk("cpu", 5, 12, true)}
		name, _, useMetric, usable := selectSignal(obs, SignalReplicas)
		if !usable || useMetric || name != "" {
			t.Errorf("expected forced replicas, got (%q, %v, %v)", name, useMetric, usable)
		}
	})
}
