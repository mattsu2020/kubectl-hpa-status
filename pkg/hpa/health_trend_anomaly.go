package hpa

import (
	"fmt"
	"time"
)

const (
	// anomalyMinSnapshots is the minimum number of snapshots required for
	// anomaly detection to produce meaningful results.
	anomalyMinSnapshots = 5

	// suddenDegradationThreshold is the minimum health score drop between
	// consecutive snapshots to trigger a sudden degradation anomaly.
	suddenDegradationThreshold = 20

	// suddenDegradationCriticalThreshold is the score drop level that
	// elevates a sudden degradation to "critical" severity.
	suddenDegradationCriticalThreshold = 40

	// suddenDegradationMaxWindow is the maximum time window within which a
	// score drop is considered "sudden".
	suddenDegradationMaxWindow = 10 * time.Minute

	// stuckStateThreshold is the maximum score deviation to consider a
	// snapshot as "stuck" (unchanged).
	stuckStateThreshold = 2

	// stuckStateMinConsecutive is the minimum number of consecutive snapshots
	// within the threshold to trigger a stuck state anomaly.
	stuckStateMinConsecutive = 20

	// oscillationWindow is the number of recent snapshots used to compute
	// the current variance for oscillation escalation detection.
	oscillationWindow = 10

	// oscillationEscalationFactor is the multiplier applied to the previous
	// window variance; if the recent window exceeds this factor, it signals
	// oscillation escalation.
	oscillationEscalationFactor = 2.0
)

// DetectAnomalies analyzes a sorted series of health snapshots and returns
// any detected anomalies. Returns nil if fewer than anomalyMinSnapshots are
// provided. The input slice is not modified.
func DetectAnomalies(snapshots []HealthSnapshot) []AnomalyDetection {
	if len(snapshots) < anomalyMinSnapshots {
		return nil
	}

	var anomalies []AnomalyDetection

	anomalies = append(anomalies, detectSuddenDegradation(snapshots)...)
	anomalies = append(anomalies, detectStuckState(snapshots)...)
	anomalies = append(anomalies, detectOscillationEscalation(snapshots)...)

	return anomalies
}

// detectSuddenDegradation finds consecutive snapshots where the health score
// drops by more than suddenDegradationThreshold within suddenDegradationMaxWindow.
func detectSuddenDegradation(snapshots []HealthSnapshot) []AnomalyDetection {
	var anomalies []AnomalyDetection

	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1]
		curr := snapshots[i]

		drop := prev.HealthScore - curr.HealthScore
		if drop <= suddenDegradationThreshold {
			continue
		}

		timeDiff := curr.Timestamp.Sub(prev.Timestamp)
		if timeDiff > suddenDegradationMaxWindow {
			continue
		}

		severity := "warning"
		if drop > suddenDegradationCriticalThreshold {
			severity = "critical"
		}

		anomalies = append(anomalies, AnomalyDetection{
			Timestamp:     curr.Timestamp,
			Type:          AnomalySuddenDegradation,
			Severity:      severity,
			ScoreBefore:   prev.HealthScore,
			ScoreAfter:    curr.HealthScore,
			Duration:      formatDurationFromDuration(timeDiff),
			CauseEstimate: "Rapid health score drop suggests a sudden metric pipeline failure, workload spike, or configuration change",
			Remediation:   "Check for recent deployments, metric pipeline issues, or HPA spec changes",
		})
	}

	return anomalies
}

// detectStuckState finds runs of consecutive snapshots where the health score
// stays within stuckStateThreshold for more than stuckStateMinConsecutive.
func detectStuckState(snapshots []HealthSnapshot) []AnomalyDetection {
	if len(snapshots) < stuckStateMinConsecutive {
		return nil
	}

	var anomalies []AnomalyDetection

	runStart := 0
	for i := 1; i <= len(snapshots); i++ {
		// Check if the current snapshot is still within threshold of the run start.
		if i < len(snapshots) &&
			abs(snapshots[i].HealthScore-snapshots[runStart].HealthScore) <= stuckStateThreshold {
			continue
		}

		runLength := i - runStart
		if runLength >= stuckStateMinConsecutive {
			first := snapshots[runStart]
			last := snapshots[i-1]
			duration := last.Timestamp.Sub(first.Timestamp)

			anomalies = append(anomalies, AnomalyDetection{
				Timestamp:     last.Timestamp,
				Type:          AnomalyStuckState,
				Severity:      "info",
				ScoreBefore:   first.HealthScore,
				ScoreAfter:    last.HealthScore,
				Duration:      formatDurationFromDuration(duration),
				CauseEstimate: "Health score has plateaued, suggesting the HPA is in a steady state",
				Remediation:   "Verify that the steady state is expected; check if scaling should be occurring but isn't",
			})
		}

		runStart = i
	}

	return anomalies
}

// detectOscillationEscalation compares the variance of the last
// oscillationWindow snapshots against the previous oscillationWindow.
func detectOscillationEscalation(snapshots []HealthSnapshot) []AnomalyDetection {
	minRequired := oscillationWindow * 2
	if len(snapshots) < minRequired {
		return nil
	}

	n := len(snapshots)
	prevStart := n - oscillationWindow*2
	prevEnd := n - oscillationWindow

	prevScores := extractScores(snapshots[prevStart:prevEnd])
	recentScores := extractScores(snapshots[prevEnd:])

	prevMean := meanValue(prevScores)
	recentMean := meanValue(recentScores)

	prevVar := varianceValue(prevScores, prevMean)
	recentVar := varianceValue(recentScores, recentMean)

	if prevVar == 0 {
		// If the previous window had zero variance, any non-zero recent
		// variance counts as escalation.
		if recentVar > 0 {
			return []AnomalyDetection{buildOscillationAnomaly(snapshots)}
		}
		return nil
	}

	if recentVar > prevVar*oscillationEscalationFactor {
		return []AnomalyDetection{buildOscillationAnomaly(snapshots)}
	}

	return nil
}

// buildOscillationAnomaly creates an oscillation escalation anomaly from the
// full snapshot series.
func buildOscillationAnomaly(snapshots []HealthSnapshot) AnomalyDetection {
	n := len(snapshots)
	first := snapshots[n-oscillationWindow]
	last := snapshots[n-1]
	duration := last.Timestamp.Sub(first.Timestamp)

	return AnomalyDetection{
		Timestamp:     last.Timestamp,
		Type:          AnomalyOscillationEscalation,
		Severity:      "warning",
		ScoreBefore:   first.HealthScore,
		ScoreAfter:    last.HealthScore,
		Duration:      formatDurationFromDuration(duration),
		CauseEstimate: "Health score oscillation is increasing, suggesting growing instability",
		Remediation:   "Review HPA behavior configuration, consider increasing stabilization window",
	}
}

// extractScores returns a new slice of health scores from the given snapshots.
func extractScores(snapshots []HealthSnapshot) []int {
	scores := make([]int, len(snapshots))
	for i, s := range snapshots {
		scores[i] = s.HealthScore
	}
	return scores
}

// formatDurationFromDuration renders a time.Duration as a human-readable string.
func formatDurationFromDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}

	switch {
	case d >= 24*time.Hour:
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		if hours == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd%dh", days, hours)
	case d >= time.Hour:
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		if minutes == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case d >= time.Minute:
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		if seconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
