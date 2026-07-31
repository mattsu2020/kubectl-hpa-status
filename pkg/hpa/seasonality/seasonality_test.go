package seasonality

import (
	"strings"
	"testing"
	"time"
)

// mondayUTC is 2026-01-05, a Monday, used as a deterministic anchor so
// weekday assertions do not depend on the wall clock.
var mondayUTC = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

// buildObservations synthesizes one observation every 5 minutes for the given
// number of consecutive days. shape maps minute-of-day (and weekday) to the
// desired replica count, letting tests express traffic profiles declaratively.
func buildObservations(start time.Time, days int, shape func(day time.Time, minute int) int32) []Observation {
	var obs []Observation
	for d := range days {
		day := start.AddDate(0, 0, d)
		for minute := 0; minute < minutesPerDay; minute += 5 {
			ts := day.Add(time.Duration(minute) * time.Minute)
			obs = append(obs, Observation{Timestamp: ts, Desired: shape(day, minute)})
		}
	}
	return obs
}

func flatShape(replicas int32) func(time.Time, int) int32 {
	return func(time.Time, int) int32 { return replicas }
}

// businessHoursShape ramps from base to peak between 09:00 and 17:00.
func businessHoursShape(base, peak int32) func(time.Time, int) int32 {
	return func(_ time.Time, minute int) int32 {
		if minute >= 9*60 && minute < 17*60 {
			return peak
		}
		return base
	}
}

func TestAnalyze_EmptyInput(t *testing.T) {
	got := Analyze(nil, Options{})
	if got == nil {
		t.Fatal("Analyze returned nil; expected a non-nil insufficient-data result")
	}
	if !got.InsufficientData {
		t.Error("expected InsufficientData=true for empty input")
	}
	if got.Detected {
		t.Error("expected Detected=false for empty input")
	}
	if len(got.Notes) == 0 {
		t.Error("expected a note explaining why no analysis was possible")
	}
}

func TestAnalyze_SingleDayIsInsufficient(t *testing.T) {
	obs := buildObservations(mondayUTC, 1, businessHoursShape(3, 12))
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.InsufficientData {
		t.Fatal("a single day of data must not be enough to claim daily periodicity")
	}
	if got.Detected {
		t.Error("expected Detected=false with only one day observed")
	}
	if got.DaysObserved != 1 {
		t.Errorf("DaysObserved = %d, want 1", got.DaysObserved)
	}
	joined := strings.Join(got.Notes, " ")
	if !strings.Contains(joined, "2") {
		t.Errorf("note should state how many days are required, got %q", joined)
	}
}

func TestAnalyze_FlatTrafficHasNoPeak(t *testing.T) {
	obs := buildObservations(mondayUTC, 4, flatShape(5))
	got := Analyze(obs, Options{Location: time.UTC})

	if got.InsufficientData {
		t.Fatal("4 days of data should be sufficient")
	}
	if got.Detected {
		t.Errorf("flat traffic must not report a recurring peak, got peak %+v", got.Peak)
	}
	if got.Recommendation != nil {
		t.Error("no recommendation should be produced when nothing is detected")
	}
	if got.Baseline != 5 {
		t.Errorf("Baseline = %v, want 5", got.Baseline)
	}
}

func TestAnalyze_DetectsDailyBusinessHoursPeak(t *testing.T) {
	obs := buildObservations(mondayUTC, 5, businessHoursShape(3, 12))
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.Detected {
		t.Fatalf("expected a detected daily peak, notes=%v", got.Notes)
	}
	if got.Cycle != CycleDaily {
		t.Errorf("Cycle = %q, want %q", got.Cycle, CycleDaily)
	}
	if got.DaysObserved != 5 {
		t.Errorf("DaysObserved = %d, want 5", got.DaysObserved)
	}
	if got.Peak == nil {
		t.Fatal("Detected=true but Peak is nil")
	}
	if got.Peak.Start != "09:00" {
		t.Errorf("Peak.Start = %q, want %q", got.Peak.Start, "09:00")
	}
	if got.Peak.End != "17:00" {
		t.Errorf("Peak.End = %q, want %q", got.Peak.End, "17:00")
	}
	if got.Peak.PeakDesired != 12 {
		t.Errorf("Peak.PeakDesired = %d, want 12", got.Peak.PeakDesired)
	}
	if got.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want 1.0 (every day shows the pattern)", got.Confidence)
	}
	if got.Peak.DaysMatched != 5 {
		t.Errorf("Peak.DaysMatched = %d, want 5", got.Peak.DaysMatched)
	}
}

func TestAnalyze_RecommendationPrescalesAheadOfRamp(t *testing.T) {
	obs := buildObservations(mondayUTC, 5, businessHoursShape(3, 12))
	got := Analyze(obs, Options{Location: time.UTC, LeadTime: 15 * time.Minute})

	if got.Recommendation == nil {
		t.Fatal("expected a recommendation for a detected peak")
	}
	rec := got.Recommendation

	// The ramp starts at 09:00, so a 15m lead time must pre-scale at 08:45.
	if rec.PrescaleAt != "08:45" {
		t.Errorf("PrescaleAt = %q, want %q", rec.PrescaleAt, "08:45")
	}
	if rec.MinReplicas != 12 {
		t.Errorf("MinReplicas = %d, want 12 (the level needed at ramp onset)", rec.MinReplicas)
	}
	if rec.CronExpression != "45 8 * * *" {
		t.Errorf("CronExpression = %q, want %q", rec.CronExpression, "45 8 * * *")
	}
	if rec.Confidence != "high" {
		t.Errorf("Confidence = %q, want %q", rec.Confidence, "high")
	}
	if !strings.Contains(rec.KEDATrigger, "type: cron") {
		t.Errorf("KEDATrigger should contain a cron trigger, got:\n%s", rec.KEDATrigger)
	}
	if !strings.Contains(rec.KEDATrigger, `desiredReplicas: "12"`) {
		t.Errorf("KEDATrigger should carry the recommended replica count, got:\n%s", rec.KEDATrigger)
	}
	if !strings.Contains(rec.Patch, "minReplicas") {
		t.Errorf("Patch should set minReplicas, got %q", rec.Patch)
	}
}

func TestAnalyze_WeekdayOnlyPatternNarrowsCron(t *testing.T) {
	// Peak on Mon-Fri only; weekends stay at baseline.
	shape := func(day time.Time, minute int) int32 {
		switch day.Weekday() {
		case time.Saturday, time.Sunday:
			return 3
		default:
			if minute >= 9*60 && minute < 17*60 {
				return 12
			}
			return 3
		}
	}
	obs := buildObservations(mondayUTC, 14, shape)
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.Detected {
		t.Fatalf("expected detection across two weeks, notes=%v", got.Notes)
	}
	if got.Peak.DaysMatched != 10 {
		t.Errorf("Peak.DaysMatched = %d, want 10 weekdays", got.Peak.DaysMatched)
	}
	if got.Recommendation.CronExpression != "45 8 * * 1-5" {
		t.Errorf("CronExpression = %q, want weekday-only %q",
			got.Recommendation.CronExpression, "45 8 * * 1-5")
	}
	wantDays := "Mon,Tue,Wed,Thu,Fri"
	if strings.Join(got.Peak.Weekdays, ",") != wantDays {
		t.Errorf("Peak.Weekdays = %v, want %s", got.Peak.Weekdays, wantDays)
	}
}

func TestAnalyze_PeakReflectsMatchingDaysNotTheAverage(t *testing.T) {
	// Weekday ramp to 14, weekends flat. Averaging the peak across all 14
	// recorded days would report ~11, a level no day ever reached, and would
	// understate the replicas the workload actually needs.
	shape := func(day time.Time, minute int) int32 {
		switch day.Weekday() {
		case time.Saturday, time.Sunday:
			return 3
		default:
			if minute >= 9*60 && minute < 17*60 {
				return 14
			}
			return 3
		}
	}
	obs := buildObservations(mondayUTC, 14, shape)
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.Detected {
		t.Fatalf("expected detection, notes=%v", got.Notes)
	}
	if got.Peak.PeakDesired != 14 {
		t.Errorf("Peak.PeakDesired = %d, want 14 (the level reached on matching days)", got.Peak.PeakDesired)
	}
	if got.Peak.OnsetDesired != 14 {
		t.Errorf("Peak.OnsetDesired = %d, want 14", got.Peak.OnsetDesired)
	}
}

func TestAnalyze_PartialConsistencyLowersConfidence(t *testing.T) {
	// The peak appears on 3 of 4 days; the third day stays flat.
	shape := func(day time.Time, minute int) int32 {
		if day.Day() == mondayUTC.Day()+2 {
			return 3
		}
		if minute >= 9*60 && minute < 17*60 {
			return 12
		}
		return 3
	}
	obs := buildObservations(mondayUTC, 4, shape)
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.Detected {
		t.Fatalf("3 of 4 days should still be detected, notes=%v", got.Notes)
	}
	if got.Confidence != 0.75 {
		t.Errorf("Confidence = %v, want 0.75", got.Confidence)
	}
	if got.Recommendation.Confidence != "medium" {
		t.Errorf("Recommendation.Confidence = %q, want %q", got.Recommendation.Confidence, "medium")
	}
}

func TestAnalyze_InconsistentPatternIsNotDetected(t *testing.T) {
	// The peak appears on only 1 of 5 days: below the consistency floor.
	shape := func(day time.Time, minute int) int32 {
		if day.Weekday() == time.Monday && minute >= 9*60 && minute < 17*60 {
			return 12
		}
		return 3
	}
	obs := buildObservations(mondayUTC, 5, shape)
	got := Analyze(obs, Options{Location: time.UTC})

	if got.Detected {
		t.Errorf("a one-off spike must not be reported as recurring (confidence %v)", got.Confidence)
	}
	if len(got.Notes) == 0 {
		t.Error("expected a note explaining why the candidate peak was rejected")
	}
}

func TestAnalyze_HandlesOvernightWrapAround(t *testing.T) {
	// Batch window runs 22:00 -> 02:00, crossing midnight.
	shape := func(_ time.Time, minute int) int32 {
		if minute >= 22*60 || minute < 2*60 {
			return 10
		}
		return 2
	}
	obs := buildObservations(mondayUTC, 5, shape)
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.Detected {
		t.Fatalf("expected detection of an overnight window, notes=%v", got.Notes)
	}
	if got.Peak.Start != "22:00" {
		t.Errorf("Peak.Start = %q, want %q", got.Peak.Start, "22:00")
	}
	if got.Peak.End != "02:00" {
		t.Errorf("Peak.End = %q, want %q", got.Peak.End, "02:00")
	}
	if !got.Peak.CrossesMidnight {
		t.Error("expected CrossesMidnight=true for a 22:00-02:00 window")
	}
}

func TestAnalyze_RespectsLocation(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	// Build a peak at 00:00-08:00 UTC, which is 09:00-17:00 in Tokyo.
	shape := func(_ time.Time, minute int) int32 {
		if minute < 8*60 {
			return 12
		}
		return 3
	}
	obs := buildObservations(mondayUTC, 5, shape)
	got := Analyze(obs, Options{Location: tokyo})

	if !got.Detected {
		t.Fatalf("expected detection, notes=%v", got.Notes)
	}
	if got.Peak.Start != "09:00" {
		t.Errorf("Peak.Start = %q, want %q in Asia/Tokyo", got.Peak.Start, "09:00")
	}
	if !strings.Contains(got.Recommendation.KEDATrigger, "timezone: Asia/Tokyo") {
		t.Errorf("KEDATrigger should carry the timezone, got:\n%s", got.Recommendation.KEDATrigger)
	}
}

func TestOptions_Defaults(t *testing.T) {
	got := Options{}.withDefaults()

	if got.BucketMinutes != defaultBucketMinutes {
		t.Errorf("BucketMinutes = %d, want %d", got.BucketMinutes, defaultBucketMinutes)
	}
	if got.MinDays != defaultMinDays {
		t.Errorf("MinDays = %d, want %d", got.MinDays, defaultMinDays)
	}
	if got.LeadTime != defaultLeadTime {
		t.Errorf("LeadTime = %v, want %v", got.LeadTime, defaultLeadTime)
	}
	if got.PeakRatio != defaultPeakRatio {
		t.Errorf("PeakRatio = %v, want %v", got.PeakRatio, defaultPeakRatio)
	}
	if got.Location == nil {
		t.Error("Location must default to a non-nil location")
	}
}

func TestOptions_InvalidBucketMinutesFallsBack(t *testing.T) {
	// 7 does not divide 1440 evenly and must not be accepted.
	got := Options{BucketMinutes: 7}.withDefaults()
	if minutesPerDay%got.BucketMinutes != 0 {
		t.Errorf("BucketMinutes = %d does not divide %d evenly", got.BucketMinutes, minutesPerDay)
	}
}

func TestAnalyze_UnsortedInputIsHandled(t *testing.T) {
	obs := buildObservations(mondayUTC, 5, businessHoursShape(3, 12))
	// Reverse the input to prove Analyze does not rely on caller ordering.
	for i, j := 0, len(obs)-1; i < j; i, j = i+1, j-1 {
		obs[i], obs[j] = obs[j], obs[i]
	}
	got := Analyze(obs, Options{Location: time.UTC})

	if !got.Detected {
		t.Fatalf("expected detection regardless of input order, notes=%v", got.Notes)
	}
	if got.Peak.Start != "09:00" {
		t.Errorf("Peak.Start = %q, want %q", got.Peak.Start, "09:00")
	}
}

func TestAnalyze_DoesNotMutateInput(t *testing.T) {
	obs := buildObservations(mondayUTC, 3, businessHoursShape(3, 12))
	before := make([]Observation, len(obs))
	copy(before, obs)

	Analyze(obs, Options{Location: time.UTC})

	for i := range obs {
		if obs[i] != before[i] {
			t.Fatalf("Analyze mutated input at index %d: %+v != %+v", i, obs[i], before[i])
		}
	}
}

func TestConfidenceLabel(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{1.0, "high"},
		{0.85, "high"},
		{0.8, "high"},
		{0.75, "medium"},
		{0.5, "medium"},
		{0.49, "low"},
		{0.0, "low"},
	}
	for _, tt := range tests {
		if got := confidenceLabel(tt.score); got != tt.want {
			t.Errorf("confidenceLabel(%v) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestCronDayOfWeek(t *testing.T) {
	tests := []struct {
		name string
		days []time.Weekday
		want string
	}{
		{"all days", []time.Weekday{0, 1, 2, 3, 4, 5, 6}, "*"},
		{"weekdays", []time.Weekday{1, 2, 3, 4, 5}, "1-5"},
		{"weekend", []time.Weekday{0, 6}, "0,6"},
		{"single", []time.Weekday{3}, "3"},
		{"empty", nil, "*"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cronDayOfWeek(tt.days); got != tt.want {
				t.Errorf("cronDayOfWeek(%v) = %q, want %q", tt.days, got, tt.want)
			}
		})
	}
}

func TestFormatMinute(t *testing.T) {
	tests := []struct {
		minute int
		want   string
	}{
		{0, "00:00"},
		{525, "08:45"},
		{540, "09:00"},
		{1439, "23:59"},
		{1440, "00:00"},  // wraps
		{-15, "23:45"},   // negative lead time wraps backwards
		{-1455, "23:45"}, // multiple wraps
	}
	for _, tt := range tests {
		if got := formatMinute(tt.minute); got != tt.want {
			t.Errorf("formatMinute(%d) = %q, want %q", tt.minute, got, tt.want)
		}
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		want   float64
	}{
		{"empty", nil, 0},
		{"single", []float64{4}, 4},
		{"odd", []float64{3, 1, 2}, 2},
		{"even", []float64{4, 1, 3, 2}, 2.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := median(tt.values); got != tt.want {
				t.Errorf("median(%v) = %v, want %v", tt.values, got, tt.want)
			}
		})
	}
}

func TestMedian_DoesNotMutateInput(t *testing.T) {
	values := []float64{5, 1, 3}
	median(values)
	want := []float64{5, 1, 3}
	for i := range values {
		if values[i] != want[i] {
			t.Fatalf("median mutated input: %v, want %v", values, want)
		}
	}
}
