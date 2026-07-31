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

	return &Recommendation{
		MinReplicas:           peak.OnsetDesired,
		LeadTime:              o.LeadTime.String(),
		PrescaleAt:            formatMinute(prescaleMinute),
		CronExpression:        cronAt(prescaleMinute, dow),
		ReleaseCronExpression: cronAt(peak.EndMinute, dow),
		KEDATrigger:           kedaCronTrigger(peak, prescaleMinute, dow, o),
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

// kedaCronTrigger renders a KEDA ScaledObject cron trigger that holds the
// replica floor across the detected window. KEDA is preferred over a static
// minReplicas bump because it releases the floor automatically after the
// window, so the pre-scaling costs nothing off-peak.
func kedaCronTrigger(peak *peakInternals, prescaleMinute int, dayOfWeek string, o Options) string {
	var b strings.Builder
	b.WriteString("triggers:\n")
	b.WriteString("- type: cron\n")
	b.WriteString("  metadata:\n")
	fmt.Fprintf(&b, "    timezone: %s\n", o.Location.String())
	fmt.Fprintf(&b, "    start: %s\n", cronAt(prescaleMinute, dayOfWeek))
	fmt.Fprintf(&b, "    end: %s\n", cronAt(peak.EndMinute, dayOfWeek))
	fmt.Fprintf(&b, "    desiredReplicas: %q\n", strconv.Itoa(int(peak.OnsetDesired)))
	return b.String()
}
