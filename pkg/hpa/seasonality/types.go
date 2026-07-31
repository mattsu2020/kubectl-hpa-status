// Package seasonality detects recurring (periodic) demand patterns in recorded
// HPA replica history and proposes scheduled pre-scaling to remove the
// cold-start lag an HPA necessarily incurs at the start of each ramp.
//
// The HPA controller is purely reactive: it can only raise replicas after the
// metric has already crossed the target, so every recurring traffic ramp pays a
// scale-up latency penalty. When the ramp is predictable, raising minReplicas
// ahead of it removes that penalty entirely. This package finds those ramps.
//
// It is a self-contained leaf domain: it depends only on the standard library
// and the shared JSON helper, and every exported function is pure.
package seasonality

import "time"

// Cycle identifies the periodicity a detection was made against.
type Cycle string

// CycleDaily indicates a pattern that repeats once per calendar day.
const CycleDaily Cycle = "daily"

const (
	// minutesPerDay is the size of the time-of-day profile in minutes.
	minutesPerDay = 24 * 60

	// defaultBucketMinutes is the time-of-day resolution of the profile. 30
	// minutes is coarse enough to absorb polling jitter and clock skew, and
	// fine enough to place a pre-scale schedule usefully.
	defaultBucketMinutes = 30

	// defaultMinDays is the minimum number of distinct local days required
	// before any claim of daily periodicity is made. Two days is the
	// theoretical floor for observing a repeat at all.
	defaultMinDays = 2

	// defaultLeadTime is how far ahead of the detected ramp the pre-scale is
	// scheduled, covering pod scheduling, image pull, and readiness.
	defaultLeadTime = 15 * time.Minute

	// defaultPeakRatio is the multiple of the daily baseline a time bucket
	// must reach to count as part of a peak window.
	defaultPeakRatio = 1.5

	// minConsistency is the fraction of scheduled days that must exhibit the
	// peak before it is reported as recurring rather than incidental.
	minConsistency = 0.5

	// minMatchedDays guards against calling a pattern recurring on the
	// strength of a single day.
	minMatchedDays = 2

	// minWeekdaySamples is how many observations of a given weekday are
	// required before the recommended schedule may be narrowed to exclude
	// (or include) that weekday. Narrowing on a single sample overfits.
	minWeekdaySamples = 2
)

// Observation is one recorded desired-replica reading at a point in time. It
// is deliberately decoupled from the richer snapshot types so this package
// stays a leaf domain.
type Observation struct {
	// Timestamp is when the reading was taken.
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`
	// Desired is the HPA's desiredReplicas at that moment.
	Desired int32 `json:"desiredReplicas" yaml:"desiredReplicas"`
}

// Options tunes detection. The zero value is valid and yields the documented
// defaults via withDefaults.
type Options struct {
	// BucketMinutes is the time-of-day resolution. It must divide 1440
	// evenly; invalid values fall back to the default.
	BucketMinutes int
	// MinDays is the minimum number of distinct local days required.
	MinDays int
	// LeadTime is how far ahead of the ramp to schedule the pre-scale.
	LeadTime time.Duration
	// PeakRatio is the multiple of baseline that marks a bucket as peak.
	PeakRatio float64
	// Location is the timezone whose calendar days and clock times define
	// the profile. Schedules are emitted in this timezone.
	Location *time.Location
}

// withDefaults returns a copy of o with unset or invalid fields replaced by
// their defaults. It never mutates the receiver.
func (o Options) withDefaults() Options {
	out := o
	if out.BucketMinutes <= 0 || out.BucketMinutes > minutesPerDay || minutesPerDay%out.BucketMinutes != 0 {
		out.BucketMinutes = defaultBucketMinutes
	}
	if out.MinDays < defaultMinDays {
		out.MinDays = defaultMinDays
	}
	if out.LeadTime <= 0 {
		out.LeadTime = defaultLeadTime
	}
	if out.PeakRatio <= 1 {
		out.PeakRatio = defaultPeakRatio
	}
	if out.Location == nil {
		out.Location = time.Local
	}
	return out
}

// Analysis is the result of a seasonality detection run.
//
// Analyze always returns a non-nil Analysis: when nothing is detected, the
// Notes explain why, which is itself the actionable output for an operator
// deciding whether to record for longer.
type Analysis struct {
	// Detected reports whether a recurring peak window was found with
	// enough consistency to act on.
	Detected bool `json:"detected" yaml:"detected"`
	// InsufficientData reports that the recording is too short to support
	// any periodicity claim, regardless of its shape.
	InsufficientData bool `json:"insufficientData" yaml:"insufficientData"`
	// Cycle is the periodicity tested against.
	Cycle Cycle `json:"cycle" yaml:"cycle"`
	// Timezone is the location used for calendar days and clock times.
	Timezone string `json:"timezone" yaml:"timezone"`
	// BucketMinutes is the resolution of the time-of-day profile.
	BucketMinutes int `json:"bucketMinutes" yaml:"bucketMinutes"`
	// DaysObserved is the number of distinct local days in the recording.
	DaysObserved int `json:"daysObserved" yaml:"daysObserved"`
	// SpanHours is the wall-clock span of the recording.
	SpanHours float64 `json:"spanHours" yaml:"spanHours"`
	// Baseline is the typical off-peak desiredReplicas level.
	Baseline float64 `json:"baseline" yaml:"baseline"`
	// Threshold is the level a bucket must reach to count as peak.
	Threshold float64 `json:"threshold" yaml:"threshold"`
	// Peak describes the detected window, or nil when none was found.
	Peak *PeakWindow `json:"peak,omitempty" yaml:"peak,omitempty"`
	// Confidence is the fraction of scheduled, covered days that exhibited
	// the peak, from 0 to 1.
	Confidence float64 `json:"confidence" yaml:"confidence"`
	// Recommendation is the proposed pre-scaling schedule, or nil when
	// nothing was detected.
	Recommendation *Recommendation `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	// Notes explain the finding, prefixed [observed] for measured facts and
	// [estimated] for inferences, matching the convention used across the
	// other analysis domains.
	Notes []string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// PeakWindow is a contiguous span of the day during which desiredReplicas
// recurrently sits above the daily baseline.
type PeakWindow struct {
	// StartMinute is the inclusive window start as minutes from local midnight.
	StartMinute int `json:"startMinute" yaml:"startMinute"`
	// EndMinute is the exclusive window end as minutes from local midnight.
	EndMinute int `json:"endMinute" yaml:"endMinute"`
	// Start is StartMinute formatted as local HH:MM.
	Start string `json:"start" yaml:"start"`
	// End is EndMinute formatted as local HH:MM.
	End string `json:"end" yaml:"end"`
	// CrossesMidnight reports whether the window wraps past 00:00.
	CrossesMidnight bool `json:"crossesMidnight" yaml:"crossesMidnight"`
	// PeakDesired is the highest averaged desiredReplicas in the window.
	PeakDesired int32 `json:"peakDesired" yaml:"peakDesired"`
	// OnsetDesired is the typical desiredReplicas at the window's first
	// bucket: the level the workload needs the moment the ramp begins.
	OnsetDesired int32 `json:"onsetDesired" yaml:"onsetDesired"`
	// Weekdays lists the abbreviated weekdays that exhibited the peak.
	Weekdays []string `json:"weekdays,omitempty" yaml:"weekdays,omitempty"`
	// DaysMatched is the number of days that exhibited the peak.
	DaysMatched int `json:"daysMatched" yaml:"daysMatched"`
	// DaysCovered is the number of scheduled days with observations inside
	// the window: the denominator of Confidence.
	DaysCovered int `json:"daysCovered" yaml:"daysCovered"`
}

// Recommendation is a concrete, applyable pre-scaling schedule.
type Recommendation struct {
	// MinReplicas is the replica floor to hold during the window.
	MinReplicas int32 `json:"minReplicas" yaml:"minReplicas"`
	// LeadTime is how far ahead of the ramp the floor is raised.
	LeadTime string `json:"leadTime" yaml:"leadTime"`
	// PrescaleAt is the local HH:MM at which to raise the floor.
	PrescaleAt string `json:"prescaleAt" yaml:"prescaleAt"`
	// CronExpression is the 5-field schedule for the pre-scale.
	CronExpression string `json:"cronExpression" yaml:"cronExpression"`
	// ReleaseCronExpression is the 5-field schedule for returning to the
	// normal floor after the window.
	ReleaseCronExpression string `json:"releaseCronExpression" yaml:"releaseCronExpression"`
	// KEDATrigger is a ready-to-paste KEDA ScaledObject cron trigger.
	KEDATrigger string `json:"kedaTrigger" yaml:"kedaTrigger"`
	// Patch is a JSON merge patch that raises minReplicas statically, for
	// clusters without KEDA. It trades cost for simplicity.
	Patch string `json:"patch" yaml:"patch"`
	// Rationale explains why this schedule was proposed.
	Rationale string `json:"rationale" yaml:"rationale"`
	// Confidence is the qualitative label for Analysis.Confidence.
	Confidence string `json:"confidence" yaml:"confidence"`
}
