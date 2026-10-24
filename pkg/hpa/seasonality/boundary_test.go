package seasonality

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestMetricSeasonalityIsIndependentOfScale(t *testing.T) {
	for _, scale := range []float64{1, 0.01, 0.000001} {
		obs := buildObservations(mondayUTC, 3, businessHoursShape(2, 10))
		for i := range obs {
			value := 10.0 * scale
			if obs[i].Timestamp.Hour() >= 9 && obs[i].Timestamp.Hour() < 17 {
				value = 50 * scale
			}
			obs[i].Metrics = []MetricSample{{Name: "cpu", Value: value}}
		}
		got := Analyze(obs, Options{Location: time.UTC, Signal: SignalMetric})
		if !got.Detected || got.Recommendation == nil || got.Peak.Start != "09:00" || got.Peak.End != "17:00" || got.Recommendation.MinReplicas != 10 {
			t.Fatalf("scale %g: unexpected analysis: %+v", scale, got)
		}
		if math.Abs(got.Threshold-15*scale) > 1e-10 {
			t.Fatalf("scale %g: threshold %g, want %g", scale, got.Threshold, 15*scale)
		}
	}
}

func TestMetricSeasonalityAtZeroBaseline(t *testing.T) {
	for _, peak := range []float64{0, 0.05} {
		obs := buildObservations(mondayUTC, 3, businessHoursShape(2, 10))
		for i := range obs {
			value := 0.0
			if obs[i].Timestamp.Hour() >= 9 && obs[i].Timestamp.Hour() < 17 {
				value = peak
			}
			obs[i].Metrics = []MetricSample{{Name: "cpu", Value: value}}
		}
		got := Analyze(obs, Options{Location: time.UTC, Signal: SignalMetric})
		if got.Detected != (peak > 0) {
			t.Fatalf("peak %g: detected=%v, notes=%v", peak, got.Detected, got.Notes)
		}
	}
}

func TestOvernightRecommendationUsesStartAndEndWeekdays(t *testing.T) {
	tests := []struct {
		name      string
		endMinute int
		startDay  time.Weekday
		endDay    time.Weekday
		wantStart string
		wantEnd   string
	}{
		{"weekdays overnight", 120, time.Monday, time.Friday, "45 22 * * 1-5", "0 2 * * 2-6"},
		{"ends at midnight", 0, time.Monday, time.Friday, "45 22 * * 1-5", "0 0 * * 2-6"},
		{"saturday wraps to sunday", 120, time.Saturday, time.Saturday, "45 22 * * 6", "0 2 * * 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := buildObservations(mondayUTC, 21, func(day time.Time, minute int) int32 {
				windowDay := day
				if minute < tt.endMinute {
					windowDay = day.AddDate(0, 0, -1)
				} else if minute < 23*60 {
					return 2
				}
				if windowDay.Weekday() >= tt.startDay && windowDay.Weekday() <= tt.endDay {
					return 10
				}
				return 2
			})
			got := Analyze(obs, Options{Location: time.UTC})
			if !got.Detected || got.Recommendation == nil || !got.Peak.CrossesMidnight || got.Cycle != CycleWeekly {
				t.Fatalf("expected weekly overnight pattern: %+v", got)
			}
			rec := got.Recommendation
			if rec.CronExpression != tt.wantStart || rec.ReleaseCronExpression != tt.wantEnd {
				t.Fatalf("schedules = %q / %q, want %q / %q", rec.CronExpression, rec.ReleaseCronExpression, tt.wantStart, tt.wantEnd)
			}
			if !strings.Contains(rec.KEDATrigger, "start: "+tt.wantStart) || !strings.Contains(rec.KEDATrigger, "end: "+tt.wantEnd) {
				t.Fatalf("KEDA schedules disagree: %s", rec.KEDATrigger)
			}
		})
	}
}
