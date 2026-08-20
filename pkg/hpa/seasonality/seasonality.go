package seasonality

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/confidence"
)

// dayProfile holds the per-bucket peak readings for one local day.
type dayProfile struct {
	date    string
	weekday time.Weekday
	// signal[b] is the highest profiled signal value observed in bucket b
	// (desiredReplicas, or the typed metric reading), or -1 when the bucket
	// holds no observation (partial first/last day, gaps in the recording).
	signal []float64
	// desired[b] is the highest desiredReplicas observed in bucket b, or -1
	// when empty. Recommendation levels always come from replicas, even when
	// detection ran on metric values: pre-scaling changes replica floors, not
	// metrics.
	desired []int32
}

// Analyze looks for a recurring peak in the recorded demand history and, when
// it finds one, proposes a pre-scaling schedule.
//
// The profiled signal is chosen by Options.Signal: typed metric values when
// the recording carries them (auto), or desiredReplicas. Metric values see
// demand ramps that replicas hide — headroom between min and max, controller
// tolerance, and whole-pod quantization all flatten the replica signal.
//
// It is pure: the input slice is neither reordered nor modified. The returned
// Analysis is always non-nil; check Detected and InsufficientData to interpret
// it, and Notes for the reasoning behind either outcome.
func Analyze(obs []Observation, opts Options) *Analysis {
	o := opts.withDefaults()
	buckets := minutesPerDay / o.BucketMinutes

	metricName, metricUnit, useMetric, usable := selectSignal(obs, o.Signal)
	result := &Analysis{
		Cycle:         CycleDaily,
		Timezone:      o.Location.String(),
		BucketMinutes: o.BucketMinutes,
		Signal:        signalLabel(useMetric, metricName),
		SignalUnit:    metricUnit,
	}

	if !usable {
		result.Signal = string(SignalMetric)
		result.Notes = append(result.Notes,
			confidence.BadgeObserved+" the recording carries no typed metric values; the metric signal needs records written after typed metric capture landed.",
			confidence.BadgeEstimated+" profile desiredReplicas instead with --signal replicas, or re-record with a current build.")
		return result
	}

	if len(obs) == 0 {
		result.InsufficientData = true
		result.Notes = append(result.Notes,
			confidence.BadgeObserved+" no recorded observations; capture history with `record` before analyzing seasonality.")
		return result
	}

	days := buildDayProfiles(obs, o, buckets, metricName, useMetric)
	result.DaysObserved = len(days)
	result.SpanHours = spanHours(obs)

	if len(days) < o.MinDays {
		result.InsufficientData = true
		result.Notes = append(result.Notes,
			fmt.Sprintf(confidence.BadgeObserved+" only %d distinct day(s) across %.1fh of recording; daily periodicity needs at least %d days.",
				len(days), result.SpanHours, o.MinDays),
			fmt.Sprintf(confidence.BadgeEstimated+" record for at least %d days (for example `record --duration 48h --interval 1m --output-file hpa.jsonl`) before re-running this detector.",
				o.MinDays))
		return result
	}

	signalProfile, signalCovered := averageBuckets(days, buckets, func(d dayProfile) []float64 { return d.signal })
	desiredProfile, desiredCovered := averageBuckets(days, buckets, func(d dayProfile) []int32 { return d.desired })
	result.Baseline = median(coveredValues(signalProfile, signalCovered))
	result.Threshold = math.Max(result.Baseline*o.PeakRatio, result.Baseline+1)
	replicaBaseline := median(coveredValues(desiredProfile, desiredCovered))

	start, length, ok := findPeakRun(signalProfile, signalCovered, result.Threshold)
	if !ok {
		result.Notes = append(result.Notes,
			fmt.Sprintf(confidence.BadgeObserved+" the %s signal stayed near the daily baseline of %.1f across %d days; no recurring ramp to pre-scale for.",
				result.Signal, result.Baseline, len(days)))
		return result
	}

	peak := buildPeakWindow(days, start, length, buckets, o, result.Threshold, useMetric)
	result.Peak = peak.PeakWindow

	scheduleDays, narrowed := scheduleWeekdays(days, peak.matched)
	if narrowed {
		result.Cycle = CycleWeekly
	}
	peak.DaysMatched, peak.DaysCovered = countScheduled(days, peak.matched, peak.coveredDay, scheduleDays)
	if peak.DaysCovered > 0 {
		result.Confidence = float64(peak.DaysMatched) / float64(peak.DaysCovered)
	}

	if peak.DaysMatched < minMatchedDays || result.Confidence < minConsistency {
		result.Peak = nil
		result.Notes = append(result.Notes,
			fmt.Sprintf(confidence.BadgeObserved+" a peak near %s-%s appeared on %d of %d day(s), below the %.0f%% consistency needed to call it recurring.",
				peak.Start, peak.End, peak.DaysMatched, peak.DaysCovered, minConsistency*100),
			confidence.BadgeEstimated+" this looks like a one-off event rather than a schedule; record for longer if you expect it to repeat.")
		return result
	}

	result.Detected = true
	if useMetric && float64(peak.PeakDesired) <= replicaBaseline {
		result.Notes = append(result.Notes,
			fmt.Sprintf(confidence.BadgeObserved+" the %s signal ramps from a baseline of %.1f%s%s to %.1f%s%s between %s and %s, but desiredReplicas stayed at the %.0f-replica baseline.",
				result.Signal, result.Baseline, metricUnit, unitSep(metricUnit), peak.PeakSignal, metricUnit, unitSep(metricUnit), peak.Start, peak.End, replicaBaseline),
			confidence.BadgeEstimated+" the ramp stayed within the HPA's headroom (controller tolerance or minReplicas), so there is no replica floor to pre-scale; the scale-up delay it would cause is not visible in the replica history either.")
		return result
	}
	result.Recommendation = buildRecommendation(peak, scheduleDays, narrowed, o, result.Confidence)
	result.Notes = append(result.Notes, detectionNotes(result, peak, len(days), replicaBaseline)...)
	return result
}

// selectSignal resolves the profiled signal. With SignalAuto it picks the
// metric name covering the most observations — ties break lexicographically
// so repeated runs choose the same name — provided that name covers more than
// half the observations; otherwise it falls back to desiredReplicas, which
// legacy records always carry. With SignalMetric, usable=false reports that
// the recording carries no metric samples at all.
func selectSignal(obs []Observation, want Signal) (name, unit string, useMetric, usable bool) {
	if want == SignalReplicas {
		return "", "", false, true
	}

	coverage := map[string]int{}
	units := map[string]string{}
	for _, ob := range obs {
		seen := map[string]bool{}
		for _, s := range ob.Metrics {
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			coverage[s.Name]++
			if _, ok := units[s.Name]; !ok {
				units[s.Name] = s.Unit
			}
		}
	}

	best := ""
	for candidate, n := range coverage {
		// Auto requires more than half the observations to carry the metric;
		// a forced metric signal accepts whatever coverage exists.
		if n <= len(obs)/2 && want != SignalMetric {
			continue
		}
		if best == "" || n > coverage[best] || (n == coverage[best] && candidate < best) {
			best = candidate
		}
	}

	if best == "" {
		if want == SignalMetric {
			return "", "", true, false
		}
		return "", "", false, true
	}
	return best, units[best], true, true
}

// signalLabel renders the Analysis.Signal value for the resolved signal.
func signalLabel(useMetric bool, name string) string {
	if !useMetric {
		return string(SignalReplicas)
	}
	return "metric:" + name
}

// unitSep separates a value from its unit in notes: "%" glues to the number,
// unitless quantities get a space.
func unitSep(unit string) string {
	if unit == "%" {
		return ""
	}
	return " "
}

// buildDayProfiles groups observations into local calendar days and reduces
// each day to per-bucket maxima. Using the maximum (rather than the mean)
// within a bucket keeps short ramps visible at coarse resolutions. In metric
// mode, observations without a reading for the selected metric still record
// their desiredReplicas but leave the signal bucket empty.
func buildDayProfiles(obs []Observation, o Options, buckets int, metricName string, useMetric bool) []dayProfile {
	byDate := map[string]*dayProfile{}
	for _, ob := range obs {
		local := ob.Timestamp.In(o.Location)
		date := local.Format("2006-01-02")
		day, exists := byDate[date]
		if !exists {
			day = &dayProfile{
				date:    date,
				weekday: local.Weekday(),
				signal:  newEmptySignalBuckets(buckets),
				desired: newEmptyDesiredBuckets(buckets),
			}
			byDate[date] = day
		}
		idx := (local.Hour()*60 + local.Minute()) / o.BucketMinutes
		if idx >= buckets {
			idx = buckets - 1
		}
		if ob.Desired > day.desired[idx] {
			day.desired[idx] = ob.Desired
		}
		signal := float64(ob.Desired)
		if useMetric {
			v, found := ob.metricValue(metricName)
			if !found {
				continue
			}
			signal = v
		}
		if signal > day.signal[idx] {
			day.signal[idx] = signal
		}
	}

	out := make([]dayProfile, 0, len(byDate))
	for _, day := range byDate {
		out = append(out, *day)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].date < out[j].date })
	return out
}

// newEmptySignalBuckets returns a signal slice marked entirely as "no
// observation".
func newEmptySignalBuckets(buckets int) []float64 {
	out := make([]float64, buckets)
	for i := range out {
		out[i] = -1
	}
	return out
}

// newEmptyDesiredBuckets returns a desiredReplicas slice marked entirely as
// "no observation".
func newEmptyDesiredBuckets(buckets int) []int32 {
	out := make([]int32, buckets)
	for i := range out {
		out[i] = -1
	}
	return out
}

// averageBuckets collapses the per-day profiles into a single time-of-day
// shape by averaging each bucket across the days that observed it. Values
// below 0 mark unobserved buckets and are skipped.
func averageBuckets[B float64 | int32](days []dayProfile, buckets int, at func(dayProfile) []B) ([]float64, []bool) {
	profile := make([]float64, buckets)
	covered := make([]bool, buckets)
	for b := range buckets {
		var sum float64
		var n int
		for _, day := range days {
			v := at(day)[b]
			if v < 0 {
				continue
			}
			sum += float64(v)
			n++
		}
		if n == 0 {
			continue
		}
		profile[b] = sum / float64(n)
		covered[b] = true
	}
	return profile, covered
}

// coveredValues extracts the profile values for buckets that hold data.
func coveredValues(profile []float64, covered []bool) []float64 {
	out := make([]float64, 0, len(profile))
	for i, ok := range covered {
		if ok {
			out = append(out, profile[i])
		}
	}
	return out
}

// findPeakRun locates the contiguous run of above-threshold buckets carrying
// the most excess demand. The search wraps around midnight so overnight batch
// windows are found as a single window rather than two fragments.
//
// It returns the starting bucket, the run length, and whether any run exists.
func findPeakRun(profile []float64, covered []bool, threshold float64) (int, int, bool) {
	buckets := len(profile)
	above := func(b int) bool {
		i := b % buckets
		return covered[i] && profile[i] >= threshold
	}

	// A fully-above-threshold profile has no baseline to contrast against and
	// therefore no actionable window.
	allAbove := true
	for b := range buckets {
		if !above(b) {
			allAbove = false
			break
		}
	}
	if allAbove {
		return 0, 0, false
	}

	bestStart, bestLen := 0, 0
	var bestExcess float64
	// Scanning twice around the clock lets a run that spans midnight be seen
	// as contiguous; runs are capped at one full day.
	for b := 0; b < buckets*2; b++ {
		if !above(b) || above(b-1+buckets) {
			continue // not the first bucket of a run
		}
		var excess float64
		length := 0
		for length < buckets && above(b+length) {
			excess += profile[(b+length)%buckets] - threshold
			length++
		}
		if excess > bestExcess || (excess == bestExcess && length > bestLen) {
			bestStart, bestLen, bestExcess = b%buckets, length, excess
		}
	}
	if bestLen == 0 {
		return 0, 0, false
	}
	return bestStart, bestLen, true
}

// peakInternals carries per-day match bookkeeping alongside the exported
// PeakWindow while the analysis is still being assembled.
type peakInternals struct {
	*PeakWindow
	// matched[i] reports whether days[i] exhibited the peak.
	matched []bool
	// coveredDay[i] reports whether days[i] has any observation inside the
	// window, which makes it eligible to count for or against consistency.
	coveredDay []bool
}

// buildPeakWindow turns a bucket run into a PeakWindow and records which days
// exhibited it. Match bookkeeping runs on the profiled signal; the Peak/
// Onset replica levels come from desiredReplicas so the recommendation stays
// a replica floor even in metric mode.
func buildPeakWindow(days []dayProfile, start, length, buckets int, o Options, threshold float64, useMetric bool) *peakInternals {
	window := &PeakWindow{
		StartMinute:     start * o.BucketMinutes,
		EndMinute:       ((start + length) % buckets) * o.BucketMinutes,
		CrossesMidnight: start+length > buckets,
	}
	window.Start = formatMinute(window.StartMinute)
	window.End = formatMinute(window.EndMinute)

	matched := make([]bool, len(days))
	coveredDay := make([]bool, len(days))
	// Peak and onset levels are taken across the days that actually exhibited
	// the pattern. Averaging over every recorded day would dilute them with
	// the quiet days (a weekday-only ramp would report a peak no day ever
	// reached), which would understate the replicas the workload needs.
	var onsets, peaks, signalPeaks []float64
	for di, day := range days {
		var dayMax int32 = -1
		var signalMax float64 = -1
		for i := range length {
			bucket := (start + i) % buckets
			if v := day.desired[bucket]; v >= 0 {
				coveredDay[di] = true
				if v > dayMax {
					dayMax = v
				}
			}
			if s := day.signal[bucket]; s >= 0 {
				coveredDay[di] = true
				if s > signalMax {
					signalMax = s
				}
			}
		}
		if signalMax >= 0 && signalMax >= threshold {
			matched[di] = true
			if dayMax >= 0 {
				peaks = append(peaks, float64(dayMax))
			}
			if onset := day.desired[start]; onset >= 0 {
				onsets = append(onsets, float64(onset))
			}
			if useMetric {
				signalPeaks = append(signalPeaks, signalMax)
			}
		}
	}
	window.PeakDesired = int32(math.Ceil(median(peaks)))
	window.OnsetDesired = int32(math.Ceil(median(onsets)))
	if window.OnsetDesired <= 0 {
		window.OnsetDesired = window.PeakDesired
	}
	if useMetric {
		window.PeakSignal = median(signalPeaks)
	}
	window.Weekdays = weekdayNames(matchedWeekdays(days, matched))

	return &peakInternals{PeakWindow: window, matched: matched, coveredDay: coveredDay}
}

// scheduleWeekdays decides which weekdays the recommended schedule should
// cover. It narrows away from "every day" only when the recording contains
// enough samples of each weekday to justify the exclusion: narrowing on a
// single observation of a weekday would overfit the recording.
//
// The second return value reports whether narrowing actually happened.
func scheduleWeekdays(days []dayProfile, matched []bool) ([]time.Weekday, bool) {
	type tally struct{ covered, matched int }
	counts := map[time.Weekday]*tally{}
	for i, day := range days {
		t, ok := counts[day.weekday]
		if !ok {
			t = &tally{}
			counts[day.weekday] = t
		}
		t.covered++
		if matched[i] {
			t.matched++
		}
	}

	var qualifying, excluded []time.Weekday
	for wd, t := range counts {
		if t.covered < minWeekdaySamples {
			// Not enough evidence about this weekday either way.
			return nil, false
		}
		switch t.matched {
		case t.covered:
			qualifying = append(qualifying, wd)
		case 0:
			excluded = append(excluded, wd)
		default:
			// This weekday is itself inconsistent, so the pattern is not
			// cleanly weekday-scoped.
			return nil, false
		}
	}
	if len(qualifying) == 0 || len(excluded) == 0 {
		return nil, false
	}
	sortWeekdays(qualifying)
	return qualifying, true
}

// countScheduled tallies matched and covered days restricted to the weekdays
// the schedule actually applies to. A nil schedule means every day.
func countScheduled(days []dayProfile, matched, coveredDay []bool, schedule []time.Weekday) (int, int) {
	inSchedule := func(wd time.Weekday) bool {
		return len(schedule) == 0 || slices.Contains(schedule, wd)
	}

	var matchedCount, coveredCount int
	for i, day := range days {
		if !inSchedule(day.weekday) || !coveredDay[i] {
			continue
		}
		coveredCount++
		if matched[i] {
			matchedCount++
		}
	}
	return matchedCount, coveredCount
}

// matchedWeekdays returns the distinct weekdays that exhibited the peak.
func matchedWeekdays(days []dayProfile, matched []bool) []time.Weekday {
	seen := map[time.Weekday]bool{}
	var out []time.Weekday
	for i, day := range days {
		if !matched[i] || seen[day.weekday] {
			continue
		}
		seen[day.weekday] = true
		out = append(out, day.weekday)
	}
	sortWeekdays(out)
	return out
}

// sortWeekdays orders weekdays Monday-first so schedules read naturally,
// while keeping Sunday's cron value of 0 intact.
func sortWeekdays(days []time.Weekday) {
	rank := func(wd time.Weekday) int {
		if wd == time.Sunday {
			return 7
		}
		return int(wd)
	}
	sort.Slice(days, func(i, j int) bool { return rank(days[i]) < rank(days[j]) })
}

// weekdayNames renders weekdays as their three-letter abbreviations.
func weekdayNames(days []time.Weekday) []string {
	if len(days) == 0 {
		return nil
	}
	out := make([]string, 0, len(days))
	for _, wd := range days {
		out = append(out, wd.String()[:3])
	}
	return out
}

// detectionNotes explains a positive detection in the [observed]/[estimated]
// convention shared with the other analysis domains.
func detectionNotes(result *Analysis, peak *peakInternals, totalDays int, replicaBaseline float64) []string {
	notes := []string{
		fmt.Sprintf(confidence.BadgeObserved+" the recording spans %.1fh across %d day(s) in %s.",
			result.SpanHours, totalDays, result.Timezone),
	}
	if result.SignalUnit != "" {
		notes = append(notes,
			fmt.Sprintf(confidence.BadgeObserved+" the %s signal rises from a baseline of %.1f%s%s to %.1f%s%s between %s and %s on %d of %d recorded day(s); desiredReplicas reaches %d in the window.",
				result.Signal, result.Baseline, result.SignalUnit, unitSep(result.SignalUnit), peak.PeakSignal, result.SignalUnit, unitSep(result.SignalUnit),
				peak.Start, peak.End, peak.DaysMatched, peak.DaysCovered, peak.PeakDesired),
			fmt.Sprintf(confidence.BadgeEstimated+" the HPA can only react after the ramp begins, so each cycle pays a scale-up delay from %.0f to %d replicas; pre-scaling removes it.",
				replicaBaseline, peak.OnsetDesired))
		return notes
	}
	notes = append(notes,
		fmt.Sprintf(confidence.BadgeObserved+" desiredReplicas rises from a baseline of %.1f to %d between %s and %s on %d of %d recorded day(s).",
			result.Baseline, peak.PeakDesired, peak.Start, peak.End, peak.DaysMatched, peak.DaysCovered),
		fmt.Sprintf(confidence.BadgeEstimated+" the HPA can only react after the ramp begins, so each cycle pays a scale-up delay from %.0f to %d replicas; pre-scaling removes it.",
			result.Baseline, peak.OnsetDesired))
	return notes
}

// spanHours returns the wall-clock span covered by the observations.
func spanHours(obs []Observation) float64 {
	first, last := obs[0].Timestamp, obs[0].Timestamp
	for _, ob := range obs[1:] {
		if ob.Timestamp.Before(first) {
			first = ob.Timestamp
		}
		if ob.Timestamp.After(last) {
			last = ob.Timestamp
		}
	}
	return last.Sub(first).Hours()
}

// median returns the middle value of the sample, or 0 when it is empty. The
// input slice is copied rather than sorted in place.
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// formatMinute renders minutes-from-midnight as HH:MM, wrapping values outside
// a single day so that a lead time crossing midnight formats correctly.
func formatMinute(minute int) string {
	m := ((minute % minutesPerDay) + minutesPerDay) % minutesPerDay
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

// confidenceLabel maps a consistency fraction to the qualitative label used
// across the other recommendation types.
func confidenceLabel(score float64) string {
	switch {
	case score >= 0.8:
		return "high"
	case score >= 0.5:
		return "medium"
	default:
		return "low"
	}
}
