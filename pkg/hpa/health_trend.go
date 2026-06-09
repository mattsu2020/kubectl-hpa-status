package hpa

import (
	"math"
	"sort"
)

// AnalyzeHealthTrend computes trend statistics from a series of health snapshots.
// Returns a HealthTrendResult with variance, min/max/mean, degradation rate,
// and flapping detection.
func AnalyzeHealthTrend(snapshots []HealthSnapshot) HealthTrendResult {
	if len(snapshots) == 0 {
		return HealthTrendResult{}
	}

	// Sort by timestamp.
	sorted := make([]HealthSnapshot, len(snapshots))
	copy(sorted, snapshots)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	scores := make([]int, len(sorted))
	for i, s := range sorted {
		scores[i] = s.HealthScore
	}

	minScore, maxScore := minMax(scores)
	mean := meanValue(scores)
	variance := varianceValue(scores, mean)
	degradation := computeDegradationRate(sorted)
	flapping, severity := DetectFlapping(sorted)
	sparkline := FormatHealthSparkline(scores, 15)

	return HealthTrendResult{
		Snapshots:        sorted,
		Variance:         variance,
		MinScore:         minScore,
		MaxScore:         maxScore,
		MeanScore:        mean,
		DegradationRate:  degradation,
		FlappingDetected: flapping,
		FlappingSeverity: severity,
		Sparkline:        sparkline,
	}
}

// DetectFlapping identifies rapid oscillation in health states. It looks for
// repeated transitions between distinct health states (e.g., OK -> LIMITED -> OK)
// within a short time window.
func DetectFlapping(snapshots []HealthSnapshot) (bool, string) {
	if len(snapshots) < 3 {
		return false, ""
	}

	transitions := 0
	seenStates := make(map[string]int)

	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1].HealthState
		curr := snapshots[i].HealthState
		seenStates[curr]++

		if prev != curr {
			transitions++
		}
	}
	seenStates[snapshots[0].HealthState]++

	// Flapping detection: more than 3 state transitions in the series.
	if transitions < 4 {
		return false, ""
	}

	// Severity based on transition count.
	ratio := float64(transitions) / float64(len(snapshots)-1)
	switch {
	case ratio > 0.7:
		return true, "CRITICAL"
	case ratio > 0.5:
		return true, "HIGH"
	default:
		return true, "MEDIUM"
	}
}

// ComputeHealthVariance returns the population variance of health scores.
func ComputeHealthVariance(scores []int) float64 {
	if len(scores) == 0 {
		return 0
	}
	return varianceValue(scores, meanValue(scores))
}

// computeDegradationRate computes a linear regression slope of health scores
// over time. A negative value indicates degradation. Returns score change
// per hour.
func computeDegradationRate(snapshots []HealthSnapshot) float64 {
	if len(snapshots) < 2 {
		return 0
	}

	n := float64(len(snapshots))

	// Convert timestamps to hours from first snapshot.
	startTime := snapshots[0].Timestamp.Unix()
	var sumX, sumY, sumXY, sumX2 float64
	for _, s := range snapshots {
		x := float64(s.Timestamp.Unix()-startTime) / 3600.0
		y := float64(s.HealthScore)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denominator := n*sumX2 - sumX*sumX
	if denominator == 0 {
		return 0
	}

	slope := (n*sumXY - sumX*sumY) / denominator
	return math.Round(slope*100) / 100
}

// FormatHealthSparkline renders a compact sparkline from health scores.
// Uses block characters scaled to the score range.
func FormatHealthSparkline(scores []int, width int) string {
	if len(scores) == 0 || width <= 0 {
		return ""
	}

	// Downsample if more scores than width.
	display := scores
	if len(scores) > width {
		step := float64(len(scores)) / float64(width)
		display = make([]int, 0, width)
		for i := 0; i < width; i++ {
			idx := int(float64(i) * step)
			if idx >= len(scores) {
				idx = len(scores) - 1
			}
			display = append(display, scores[idx])
		}
	}

	_, maxVal := minMax(scores)
	if maxVal == 0 {
		maxVal = 100
	}

	chars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	var result string
	for _, score := range display {
		idx := int(float64(score) / float64(maxVal) * float64(len(chars)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(chars) {
			idx = len(chars) - 1
		}
		result += string(chars[idx])
	}

	return result
}

// Helper functions.

func minMax(values []int) (int, int) {
	if len(values) == 0 {
		return 0, 0
	}
	min := values[0]
	max := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}

func meanValue(values []int) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0
	for _, v := range values {
		sum += v
	}
	return float64(sum) / float64(len(values))
}

func varianceValue(values []int, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	var sum float64
	for _, v := range values {
		diff := float64(v) - mean
		sum += diff * diff
	}
	return sum / float64(len(values))
}
