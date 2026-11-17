package backtest

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/tolerance"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/scaling"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

const maxStabilizationWindowSeconds = 3600

// Run replays input.Trace against each candidate specification, scores the
// outcomes, and returns a ranked comparison report.
func Run(input Input) (*Report, error) {
	if len(input.Trace.Snapshots) == 0 {
		return nil, fmt.Errorf("backtest requires at least one snapshot in trace")
	}
	if len(input.Candidates) == 0 {
		return nil, fmt.Errorf("backtest requires at least one candidate configuration")
	}

	sortedSnaps := sortedSnapshots(input.Trace.Snapshots)
	trace := input.Trace
	trace.Snapshots = sortedSnaps

	metricBacked := countMetricBackedSnapshots(sortedSnaps)
	confidence := traceConfidence(metricBacked, len(sortedSnaps))
	baseline := summarizeTrace(trace, 0)

	results := make([]CandidateReport, 0, len(input.Candidates))
	for i, spec := range input.Candidates {
		if err := validateCandidate(spec, i); err != nil {
			return nil, err
		}
		summary := simulateCandidate(trace, spec)
		summary.ExtraPodHours = summary.PodHours - baseline.PodHours
		score, risk := scoreCandidate(summary)
		results = append(results, CandidateReport{
			Name:           candidateName(spec, i),
			Config:         candidateConfigMap(spec),
			Summary:        summary,
			HealthScore:    score,
			RiskLevel:      risk,
			Confidence:     confidence,
			Recommendation: buildRecommendation(baseline, summary, risk),
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if a.HealthScore != b.HealthScore {
			return a.HealthScore > b.HealthScore
		}
		if a.Summary.UnderProvisionedSnapshots != b.Summary.UnderProvisionedSnapshots {
			return a.Summary.UnderProvisionedSnapshots < b.Summary.UnderProvisionedSnapshots
		}
		if a.Summary.DirectionFlips != b.Summary.DirectionFlips {
			return a.Summary.DirectionFlips < b.Summary.DirectionFlips
		}
		return a.Summary.PodHours < b.Summary.PodHours
	})
	for i := range results {
		results[i].Rank = i + 1
	}

	best := ""
	if len(results) > 0 {
		best = results[0].Name
	}

	return &Report{
		Namespace:             trace.Namespace,
		Name:                  trace.HPAName,
		Source:                input.Source,
		Snapshots:             len(sortedSnaps),
		MetricBackedSnapshots: metricBacked,
		Confidence:            confidence,
		Baseline:              baseline,
		BestCandidate:         best,
		Candidates:            results,
		Limitations:           buildLimitations(metricBacked, len(sortedSnaps)),
	}, nil
}

func validateCandidate(spec CandidateSpec, idx int) error {
	label := candidateName(spec, idx)
	if spec.MaxReplicas <= 0 {
		return fmt.Errorf("candidate %q: maxReplicas must be > 0 (got %d)", label, spec.MaxReplicas)
	}
	if spec.MinReplicas != nil {
		if *spec.MinReplicas < 0 {
			return fmt.Errorf("candidate %q: minReplicas must be >= 0 (got %d)", label, *spec.MinReplicas)
		}
		if *spec.MinReplicas > spec.MaxReplicas {
			return fmt.Errorf("candidate %q: minReplicas (%d) exceeds maxReplicas (%d)", label, *spec.MinReplicas, spec.MaxReplicas)
		}
	}
	if spec.ScaleUpStabilizationSeconds < 0 || spec.ScaleUpStabilizationSeconds > maxStabilizationWindowSeconds {
		return fmt.Errorf("candidate %q: scaleUpStabilizationSeconds must be between 0 and %d", label, maxStabilizationWindowSeconds)
	}
	if spec.ScaleDownStabilizationSeconds < 0 || spec.ScaleDownStabilizationSeconds > maxStabilizationWindowSeconds {
		return fmt.Errorf("candidate %q: scaleDownStabilizationSeconds must be between 0 and %d", label, maxStabilizationWindowSeconds)
	}
	if spec.TargetUtilization != nil && *spec.TargetUtilization <= 0 {
		return fmt.Errorf("candidate %q: targetUtilization must be > 0", label)
	}
	if spec.TargetValue != nil && *spec.TargetValue <= 0 {
		return fmt.Errorf("candidate %q: targetValue must be > 0", label)
	}
	return nil
}

type recommendationSample struct {
	ts      time.Time
	desired int32
}

func simulateCandidate(trace hpaanalysis.TimelineTrace, spec CandidateSpec) Summary {
	minReplicas := int32(1)
	if spec.MinReplicas != nil {
		minReplicas = *spec.MinReplicas
	}
	tol := spec.Tolerance
	if tol <= 0 {
		tol = DefaultTolerance
	}

	current := max(minReplicas, trace.Snapshots[0].Desired)
	if current > spec.MaxReplicas {
		current = spec.MaxReplicas
	}

	var (
		recHistory     []recommendationSample
		scalingHistory []scaling.Event
		baseUnix       = trace.Snapshots[0].Timestamp.Unix()
		metricBacked   int
		underProv      int
		capped         int
		cappedSecs     int64
		scaleEvents    int
		flips          int
		lastDir        int
		peak           int32
		podHours       float64
	)

	for i, snap := range trace.Snapshots {
		effectiveMin := effectiveMinReplicas(minReplicas, spec.PrescaleWindows, snap.Timestamp)
		rawDesired, usedMetrics := computeRawDesired(snap, current, spec, tol)
		if usedMetrics {
			metricBacked++
		}
		demandDesired := computeDemandDesired(snap, spec, tol)

		recHistory = append(recHistory, recommendationSample{ts: snap.Timestamp, desired: rawDesired})
		stabilized := applyStabilization(recHistory, snap.Timestamp, current, rawDesired, spec)

		nowSec := snap.Timestamp.Unix() - baseUnix
		rateLimited := applyRateRules(current, stabilized, nowSec, scalingHistory, spec)
		next := clampInt32(rateLimited, effectiveMin, spec.MaxReplicas)

		if next != current && i > 0 {
			scaleEvents++
			dir := 1
			if next < current {
				dir = -1
			}
			if lastDir != 0 && dir != lastDir {
				flips++
			}
			lastDir = dir
			scalingHistory = append(scalingHistory, scaling.Event{
				TimeSeconds: nowSec,
				Change:      next - current,
			})
		}
		current = next

		if current > peak {
			peak = current
		}
		secs := snapshotDurationSeconds(trace, i)
		if current >= spec.MaxReplicas && demandDesired > spec.MaxReplicas {
			capped++
			cappedSecs += secs
		}
		if current < demandDesired {
			underProv++
		}
		podHours += float64(current) * float64(secs) / 3600.0
	}

	total := len(trace.Snapshots)
	return Summary{
		Snapshots:                 total,
		MetricBackedSnapshots:     metricBacked,
		ScaleEvents:               scaleEvents,
		DirectionFlips:            flips,
		PeakReplicas:              peak,
		CappedSnapshots:           capped,
		CappedDurationSeconds:     cappedSecs,
		UnderProvisionedSnapshots: underProv,
		UnderProvisionedRatio:     float64(underProv) / float64(total),
		PodHours:                  podHours,
	}
}

func computeRawDesired(snap hpaanalysis.TimelineSnapshot, simCurrent int32, spec CandidateSpec, tol float64) (int32, bool) {
	if len(snap.MetricValues) == 0 {
		return snap.Desired, false
	}

	recordedBase := max(int32(1), snap.Current)
	simBase := max(int32(1), simCurrent)
	var maxDesired int32
	computed := false

	for _, reading := range snap.MetricValues {
		target := resolveMetricTarget(reading, spec)
		if target <= 0 || reading.Value < 0 || math.IsNaN(reading.Value) || math.IsInf(reading.Value, 0) {
			continue
		}
		projectedValue := reading.Value
		if isPerPodMetric(reading) {
			projectedValue = reading.Value * float64(recordedBase) / float64(simBase)
		}
		ratio := projectedValue / target
		if math.Abs(ratio-1) <= tol {
			if !computed || simBase > maxDesired {
				maxDesired = simBase
			}
			computed = true
			continue
		}
		desired, ok := tolerance.ProjectedReplicasForRatio(simBase, ratio)
		if !ok {
			continue
		}
		if !computed || desired > maxDesired {
			maxDesired = desired
		}
		computed = true
	}

	if !computed {
		return snap.Desired, false
	}
	return maxDesired, true
}

func computeDemandDesired(snap hpaanalysis.TimelineSnapshot, spec CandidateSpec, tol float64) int32 {
	if len(snap.MetricValues) == 0 {
		return snap.Desired
	}
	base := max(int32(1), snap.Current)
	var maxDesired int32
	computed := false
	for _, reading := range snap.MetricValues {
		target := resolveMetricTarget(reading, spec)
		if target <= 0 || reading.Value < 0 || math.IsNaN(reading.Value) || math.IsInf(reading.Value, 0) {
			continue
		}
		ratio := reading.Value / target
		if math.Abs(ratio-1) <= tol {
			if !computed || base > maxDesired {
				maxDesired = base
			}
			computed = true
			continue
		}
		desired, ok := tolerance.ProjectedReplicasForRatio(base, ratio)
		if ok && (!computed || desired > maxDesired) {
			maxDesired = desired
			computed = true
		}
	}
	if !computed {
		return snap.Desired
	}
	return maxDesired
}

func resolveMetricTarget(reading hpaanalysis.MetricReading, spec CandidateSpec) float64 {
	if reading.Unit == "%" && spec.TargetUtilization != nil {
		return float64(*spec.TargetUtilization)
	}
	if spec.TargetValue != nil {
		return *spec.TargetValue
	}
	return reading.Target
}

func isPerPodMetric(reading hpaanalysis.MetricReading) bool {
	return reading.Unit == "%" || reading.Type == "Resource" || reading.Type == "ContainerResource" || reading.Type == "Pods"
}

func applyStabilization(history []recommendationSample, now time.Time, current, rawDesired int32, spec CandidateSpec) int32 {
	if rawDesired > current && spec.ScaleUpStabilizationSeconds > 0 {
		cutoff := now.Add(-time.Duration(spec.ScaleUpStabilizationSeconds) * time.Second)
		minRec := rawDesired
		for _, sample := range history {
			if !sample.ts.Before(cutoff) && sample.desired < minRec {
				minRec = sample.desired
			}
		}
		if minRec > current {
			return minRec
		}
		return current
	}
	if rawDesired < current && spec.ScaleDownStabilizationSeconds > 0 {
		cutoff := now.Add(-time.Duration(spec.ScaleDownStabilizationSeconds) * time.Second)
		maxRec := rawDesired
		for _, sample := range history {
			if !sample.ts.Before(cutoff) && sample.desired > maxRec {
				maxRec = sample.desired
			}
		}
		if maxRec < current {
			return maxRec
		}
		return current
	}
	return rawDesired
}

func applyRateRules(current, desired int32, nowSec int64, history []scaling.Event, spec CandidateSpec) int32 {
	if desired > current && spec.ScaleUpRules != nil {
		sel := autoscalingv2.MaxChangePolicySelect
		if spec.ScaleUpRules.SelectPolicy != nil {
			sel = *spec.ScaleUpRules.SelectPolicy
		}
		policies := spec.ScaleUpRules.Policies
		if policies == nil {
			policies = scaling.DefaultPolicies(true)
		}
		if limit, ok := scaling.Limit(current, true, sel, policies, nowSec, history); ok && desired > limit {
			return limit
		}
	}
	if desired < current && spec.ScaleDownRules != nil {
		sel := autoscalingv2.MaxChangePolicySelect
		if spec.ScaleDownRules.SelectPolicy != nil {
			sel = *spec.ScaleDownRules.SelectPolicy
		}
		policies := spec.ScaleDownRules.Policies
		if policies == nil {
			policies = scaling.DefaultPolicies(false)
		}
		if limit, ok := scaling.Limit(current, false, sel, policies, nowSec, history); ok && desired < limit {
			return limit
		}
	}
	return desired
}

func effectiveMinReplicas(baseMin int32, windows []PrescaleWindow, ts time.Time) int32 {
	eff := baseMin
	if ts.IsZero() {
		return eff
	}
	minute := ts.Hour()*60 + ts.Minute()
	day := ts.Weekday()
	for _, w := range windows {
		if len(w.Weekdays) > 0 && !containsWeekday(w.Weekdays, day) {
			continue
		}
		if minuteInWindow(minute, w.StartMinute, w.EndMinute) && w.MinReplicas > eff {
			eff = w.MinReplicas
		}
	}
	return eff
}

func containsWeekday(days []time.Weekday, target time.Weekday) bool {
	for _, d := range days {
		if d == target {
			return true
		}
	}
	return false
}

func minuteInWindow(minute, start, end int) bool {
	if start == end {
		return false
	}
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

func summarizeTrace(trace hpaanalysis.TimelineTrace, maxReplicas int32) Summary {
	total := len(trace.Snapshots)
	var (
		metricBacked int
		scaleEvents  int
		flips        int
		lastDesired  int32
		lastDir      int
		peak         int32
		capped       int
		cappedSecs   int64
		underProv    int
		podHours     float64
	)
	for i, snap := range trace.Snapshots {
		if len(snap.MetricValues) > 0 {
			metricBacked++
		}
		if snap.Desired > peak {
			peak = snap.Desired
		}
		secs := snapshotDurationSeconds(trace, i)
		podHours += float64(snap.Desired) * float64(secs) / 3600.0
		if snap.Health == string(hpaanalysis.HealthLimited) {
			underProv++
			capped++
			cappedSecs += secs
		} else if maxReplicas > 0 && snap.Desired >= maxReplicas {
			capped++
			cappedSecs += secs
		}
		if i == 0 {
			lastDesired = snap.Desired
			continue
		}
		if snap.Desired == lastDesired {
			continue
		}
		scaleEvents++
		dir := 1
		if snap.Desired < lastDesired {
			dir = -1
		}
		if lastDir != 0 && dir != lastDir {
			flips++
		}
		lastDir = dir
		lastDesired = snap.Desired
	}
	return Summary{
		Snapshots:                 total,
		MetricBackedSnapshots:     metricBacked,
		ScaleEvents:               scaleEvents,
		DirectionFlips:            flips,
		PeakReplicas:              peak,
		CappedSnapshots:           capped,
		CappedDurationSeconds:     cappedSecs,
		UnderProvisionedSnapshots: underProv,
		UnderProvisionedRatio:     float64(underProv) / float64(total),
		PodHours:                  podHours,
	}
}

func scoreCandidate(s Summary) (int, string) {
	underPenalty := int(math.Round(s.UnderProvisionedRatio * 50))
	flapPenalty := min(30, s.DirectionFlips*5)
	churnPenalty := min(10, s.ScaleEvents/3)
	capPenalty := 0
	if s.Snapshots > 0 {
		capPenalty = min(15, int(math.Round(float64(s.CappedSnapshots)/float64(s.Snapshots)*20)))
	}
	score := clampInt(100-underPenalty-flapPenalty-churnPenalty-capPenalty, 0, 100)

	risk := "low"
	switch {
	case s.UnderProvisionedRatio >= 0.15 || s.CappedSnapshots > 3 || score < 60:
		risk = "high"
	case s.UnderProvisionedSnapshots > 0 || s.DirectionFlips > 2 || s.CappedSnapshots > 0 || score < 85:
		risk = "medium"
	}
	return score, risk
}

func buildRecommendation(baseline, candidate Summary, risk string) string {
	var parts []string
	if candidate.DirectionFlips < baseline.DirectionFlips {
		parts = append(parts, fmt.Sprintf("reduces direction flips from %d to %d", baseline.DirectionFlips, candidate.DirectionFlips))
	}
	if candidate.UnderProvisionedSnapshots > 0 {
		parts = append(parts, fmt.Sprintf("%d snapshot(s) under-provisioned vs observed demand", candidate.UnderProvisionedSnapshots))
	} else {
		parts = append(parts, "meets observed demand across all snapshots")
	}
	if candidate.ExtraPodHours > 0.05 {
		parts = append(parts, fmt.Sprintf("cost delta %+.2f pod-hours", candidate.ExtraPodHours))
	} else if candidate.ExtraPodHours < -0.05 {
		parts = append(parts, fmt.Sprintf("saves %.2f pod-hours", -candidate.ExtraPodHours))
	}
	if risk == "high" {
		parts = append(parts, "high SLO risk")
	}
	return strings.Join(parts, "; ")
}

func sortedSnapshots(in []hpaanalysis.TimelineSnapshot) []hpaanalysis.TimelineSnapshot {
	out := make([]hpaanalysis.TimelineSnapshot, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

func countMetricBackedSnapshots(snaps []hpaanalysis.TimelineSnapshot) int {
	n := 0
	for _, s := range snaps {
		if len(s.MetricValues) > 0 {
			n++
		}
	}
	return n
}

func traceConfidence(metricBacked, total int) string {
	if total == 0 || metricBacked == 0 {
		return ConfidenceLow
	}
	if metricBacked*2 >= total {
		return ConfidenceHigh
	}
	return ConfidenceMedium
}

func buildLimitations(metricBacked, total int) []string {
	var out []string
	if metricBacked == 0 {
		out = append(out, "[estimated] trace contains no metricValues; backtest replayed recorded desiredReplicas as demand baseline")
	} else if metricBacked < total {
		out = append(out, fmt.Sprintf("[estimated] %d/%d snapshots carry metricValues; remaining snapshots fall back to recorded desiredReplicas", metricBacked, total))
	}
	out = append(out, "[estimated] closed-loop metric projection assumes workload demand is inversely proportional to replica count")
	return out
}

func snapshotDurationSeconds(trace hpaanalysis.TimelineTrace, index int) int64 {
	if index >= 0 && index+1 < len(trace.Snapshots) {
		if d := trace.Snapshots[index+1].Timestamp.Sub(trace.Snapshots[index].Timestamp); d > 0 {
			return int64(d.Seconds())
		}
	}
	if trace.Interval > 0 {
		return int64(trace.Interval.Seconds())
	}
	return 60
}

func candidateName(spec CandidateSpec, idx int) string {
	if strings.TrimSpace(spec.Name) != "" {
		return spec.Name
	}
	return fmt.Sprintf("candidate-%d", idx+1)
}

func candidateConfigMap(spec CandidateSpec) map[string]string {
	cfg := make(map[string]string)
	if spec.MinReplicas != nil {
		cfg["minReplicas"] = strconv.Itoa(int(*spec.MinReplicas))
	}
	cfg["maxReplicas"] = strconv.Itoa(int(spec.MaxReplicas))
	if spec.TargetUtilization != nil {
		cfg["targetUtilization"] = fmt.Sprintf("%d%%", *spec.TargetUtilization)
	}
	if spec.TargetValue != nil {
		cfg["targetValue"] = strconv.FormatFloat(*spec.TargetValue, 'f', -1, 64)
	}
	if spec.ScaleUpStabilizationSeconds > 0 {
		cfg["scaleUpStabilizationSeconds"] = strconv.Itoa(int(spec.ScaleUpStabilizationSeconds))
	}
	if spec.ScaleDownStabilizationSeconds > 0 {
		cfg["scaleDownStabilizationSeconds"] = strconv.Itoa(int(spec.ScaleDownStabilizationSeconds))
	}
	if len(spec.PrescaleWindows) > 0 {
		cfg["prescaleWindows"] = strconv.Itoa(len(spec.PrescaleWindows))
	}
	return cfg
}

func clampInt32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
