package seasonality

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

// buildRecommendation turns a detected peak window into an applyable
// pre-scaling schedule.
//
// narrowed reports whether the recording justified restricting the schedule to
// specific weekdays. When it did not, the schedule covers every day: observing
// only weekdays is not evidence that weekends behave differently.
func buildRecommendation(peak *peakInternals, schedule []time.Weekday, narrowed bool, o Options, confidence float64) *Recommendation {
	dow := "*"
	if narrowed {
		dow = cronDayOfWeek(schedule)
	}

	leadMinutes := int(o.LeadTime.Minutes())
	prescaleMinute := peak.StartMinute - leadMinutes
	// A lead time earlier than local midnight wraps the pre-scale fire time
	// into the previous calendar day, so the day-of-week field must shift with
	// it: pre-scaling "Mon-Fri 00:00" ramps means firing "Sun-Thu 23:45", not
	// "Mon-Fri 23:45" which would miss the Monday ramp by a full day.
	prescaleDOW := shiftCronDayOfWeek(dow, floorDiv(prescaleMinute, minutesPerDay))

	return &Recommendation{
		MinReplicas:           peak.OnsetDesired,
		LeadTime:              o.LeadTime.String(),
		PrescaleAt:            formatMinute(prescaleMinute),
		CronExpression:        cronAt(prescaleMinute, prescaleDOW),
		ReleaseCronExpression: cronAt(peak.EndMinute, dow),
		KEDATrigger:           kedaCronTrigger(peak, prescaleMinute, prescaleDOW, dow, o),
		Patch: util.MustMarshalJSON(map[string]any{
			"spec": map[string]any{"minReplicas": peak.OnsetDesired},
		}),
		Rationale: fmt.Sprintf(
			"desiredReplicas recurrently reaches %d by %s. Raising the floor to %d at %s (%s ahead of the ramp) lets pods be scheduled, pulled, and ready before demand arrives, instead of after the HPA observes the metric breach.",
			peak.OnsetDesired, peak.Start, peak.OnsetDesired, formatMinute(prescaleMinute), o.LeadTime),
		Confidence: confidenceLabel(confidence),
	}
}

// cronAt renders a 5-field cron expression firing at the given
// minutes-from-midnight on the given day-of-week field.
func cronAt(minute int, dayOfWeek string) string {
	m := ((minute % minutesPerDay) + minutesPerDay) % minutesPerDay
	return fmt.Sprintf("%d %d * * %s", m%60, m/60, dayOfWeek)
}

// cronDayOfWeek renders a weekday set as a cron day-of-week field, collapsing
// a contiguous span into a range. An empty or complete set becomes "*".
func cronDayOfWeek(days []time.Weekday) string {
	if len(days) == 0 {
		return "*"
	}

	seen := map[int]bool{}
	nums := make([]int, 0, len(days))
	for _, d := range days {
		n := int(d)
		if seen[n] {
			continue
		}
		seen[n] = true
		nums = append(nums, n)
	}
	sort.Ints(nums)

	if len(nums) == 7 {
		return "*"
	}
	if len(nums) == 1 {
		return strconv.Itoa(nums[0])
	}
	// Collapse to a range only for three or more consecutive days; a pair
	// like "0,6" reads more clearly enumerated.
	if len(nums) >= 3 && nums[len(nums)-1]-nums[0] == len(nums)-1 {
		return fmt.Sprintf("%d-%d", nums[0], nums[len(nums)-1])
	}

	parts := make([]string, 0, len(nums))
	for _, n := range nums {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ",")
}

// shiftCronDayOfWeek moves every weekday in a cron day-of-week field by the
// given number of days (mod 7), so a schedule whose fire time wraps past
// midnight keeps pre-scaling the right ramps. "*", ranges ("1-5"), and lists
// ("0,6") are all handled; a shifted range that stays contiguous is re-collapsed.
func shiftCronDayOfWeek(field string, shift int) string {
	shift = ((shift % 7) + 7) % 7
	if shift == 0 || field == "" {
		return field
	}
	if field == "*" {
		return "*"
	}

	var days []time.Weekday
	for _, part := range strings.Split(field, ",") {
		if start, end, ok := strings.Cut(part, "-"); ok {
			lo, err := strconv.Atoi(start)
			if err != nil {
				return field
			}
			hi, err := strconv.Atoi(end)
			if err != nil {
				return field
			}
			for d := lo; d <= hi && d-lo < 7; d++ {
				days = append(days, time.Weekday(((d+shift)%7+7)%7))
			}
			continue
		}
		d, err := strconv.Atoi(part)
		if err != nil {
			return field
		}
		days = append(days, time.Weekday(((d+shift)%7+7)%7))
	}
	if len(days) == 7 {
		return "*"
	}
	return cronDayOfWeek(days)
}

// floorDiv divides rounding toward negative infinity, so a prescale minute of
// -15 maps to day offset -1 (the previous calendar day), not 0.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// kedaCronTrigger renders a KEDA ScaledObject cron trigger that holds the
// replica floor across the detected window. KEDA is preferred over a static
// minReplicas bump because it releases the floor automatically after the
// window, so the pre-scaling costs nothing off-peak. The start schedule uses
// prescaleDayOfWeek because a lead time before midnight fires on the previous
// weekday; the end schedule stays on the window's own weekdays.
func kedaCronTrigger(peak *peakInternals, prescaleMinute int, prescaleDayOfWeek, endDayOfWeek string, o Options) string {
	var b strings.Builder
	b.WriteString("triggers:\n")
	b.WriteString("- type: cron\n")
	b.WriteString("  metadata:\n")
	fmt.Fprintf(&b, "    timezone: %s\n", o.Location.String())
	fmt.Fprintf(&b, "    start: %s\n", cronAt(prescaleMinute, prescaleDayOfWeek))
	fmt.Fprintf(&b, "    end: %s\n", cronAt(peak.EndMinute, endDayOfWeek))
	fmt.Fprintf(&b, "    desiredReplicas: %q\n", strconv.Itoa(int(peak.OnsetDesired)))
	return b.String()
}
