package hpa

import (
	"fmt"
	"math"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// ProjectReplicaTrajectory generates a time-series projection showing how
// replica count would change over time under the modified HPA configuration.
//
// The model assumes a step function: metrics transition from current to simulated
// values, and the HPA controller responds according to the modified parameters.
// This is a simplified linear projection — the actual HPA controller behavior
// depends on many factors including metric freshness, evaluation intervals,
// and stabilization windows.
func ProjectReplicaTrajectory(original, modified *autoscalingv2.HorizontalPodAutoscaler, opts SimulationExtendedOptions) []ProjectedState {
	duration := opts.DurationSeconds
	if duration <= 0 {
		duration = 300 // default 5 minutes
	}

	step := opts.StepSeconds
	if step <= 0 {
		step = 30 // default 30-second steps
	}

	// Ensure step is reasonable.
	if step > duration {
		step = duration
	}

	minReplicas := int32(1)
	if modified.Spec.MinReplicas != nil {
		minReplicas = *modified.Spec.MinReplicas
	}
	maxReplicas := modified.Spec.MaxReplicas

	// Compute starting and ending replica estimates.
	startReplicas := original.Status.DesiredReplicas
	endReplicas := computeEndReplicas(original, modified)

	// Compute effective stabilization delay.
	stabilizationDelay := computeStabilizationDelay(modified)

	var states []ProjectedState
	for offset := int32(0); offset <= duration; offset += step {
		replicas := interpolateReplicas(startReplicas, endReplicas, offset, stabilizationDelay, duration)
		ratio := computeMetricRatio(startReplicas, replicas, minReplicas, maxReplicas)

		states = append(states, ProjectedState{
			TimeOffset:           offset,
			ProjectedReplicas:    replicas,
			ProjectedMetricRatio: ratio,
		})
	}

	// Ensure final state is included.
	if len(states) > 0 && states[len(states)-1].TimeOffset < duration {
		replicas := interpolateReplicas(startReplicas, endReplicas, duration, stabilizationDelay, duration)
		ratio := computeMetricRatio(startReplicas, replicas, minReplicas, maxReplicas)
		states = append(states, ProjectedState{
			TimeOffset:           duration,
			ProjectedReplicas:    replicas,
			ProjectedMetricRatio: ratio,
		})
	}

	return states
}

// computeEndReplicas estimates the final replica count after the modified HPA
// parameters take effect.
func computeEndReplicas(_, modified *autoscalingv2.HorizontalPodAutoscaler) int32 {
	modifiedAnalysis := AnalyzeWithOptions(modified, false, AnalysisOptions{})
	return modifiedAnalysis.Desired
}

// computeStabilizationDelay returns the stabilization delay in seconds from
// the modified HPA configuration.
func computeStabilizationDelay(modified *autoscalingv2.HorizontalPodAutoscaler) int32 {
	if modified.Spec.Behavior == nil {
		return 0
	}
	if modified.Spec.Behavior.ScaleDown != nil && modified.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
		return *modified.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	}
	return 0
}

// interpolateReplicas computes the estimated replica count at a given time
// offset using a simple model: the HPA waits for the stabilization window,
// then transitions replicas from start to end over the remaining time.
func interpolateReplicas(startReplicas, endReplicas, offset, stabilizationDelay, _ int32) int32 {
	if startReplicas == endReplicas {
		return startReplicas
	}

	// During stabilization window, replicas stay at current level.
	if offset < stabilizationDelay {
		return startReplicas
	}

	// After stabilization, transition to end replicas.
	// Scale-up is typically fast (no stabilization), scale-down is delayed.
	if endReplicas > startReplicas {
		// Scale-up: rapid transition (within one step).
		return endReplicas
	}

	// Scale-down: transition happens after stabilization window.
	return endReplicas
}

// computeMetricRatio estimates the metric ratio for a given replica count.
func computeMetricRatio(startReplicas, currentReplicas, _, _ int32) float64 {
	if startReplicas <= 0 {
		return 1.0
	}
	ratio := float64(startReplicas) / float64(currentReplicas)
	return math.Round(ratio*100) / 100
}

// FormatTrajectoryASCII renders a time-series projection as an ASCII graph.
// The Y-axis shows replica counts, the X-axis shows time.
func FormatTrajectoryASCII(states []ProjectedState, width int) string {
	if len(states) == 0 {
		return ""
	}
	if width <= 0 {
		width = 40
	}

	// Find min/max replicas for Y-axis scaling.
	minReplicas := states[0].ProjectedReplicas
	maxReplicas := states[0].ProjectedReplicas
	maxOffset := states[len(states)-1].TimeOffset
	for _, s := range states {
		if s.ProjectedReplicas < minReplicas {
			minReplicas = s.ProjectedReplicas
		}
		if s.ProjectedReplicas > maxReplicas {
			maxReplicas = s.ProjectedReplicas
		}
	}

	replicaRange := maxReplicas - minReplicas
	if replicaRange == 0 {
		replicaRange = 1
	}

	// Build the graph rows.
	graphHeight := 5
	rows := make([][]string, graphHeight)
	for i := range rows {
		rows[i] = make([]string, width)
		for j := range rows[i] {
			rows[i][j] = " "
		}
	}

	// Plot points.
	for _, s := range states {
		if maxOffset <= 0 {
			continue
		}
		x := int(float64(s.TimeOffset) / float64(maxOffset) * float64(width-1))
		if x >= width {
			x = width - 1
		}

		y := graphHeight - 1 - int(float64(s.ProjectedReplicas-minReplicas)/float64(replicaRange)*float64(graphHeight-1))
		if y < 0 {
			y = 0
		}
		if y >= graphHeight {
			y = graphHeight - 1
		}

		rows[y][x] = "█"
	}

	// Build output string.
	var result string
	for i, row := range rows {
		label := fmt.Sprintf("%3d", maxReplicas-int32(i)*replicaRange/int32(graphHeight))
		result += label + " │" + strings.Join(row, "") + "│\n"
	}

	// X-axis with time labels.
	result += "    └" + repeatChar("─", width) + "┘\n"
	result += fmt.Sprintf("     0s%"+fmt.Sprintf("%d", width-4)+"s\n", FormatDuration(int64(maxOffset)))

	return result
}

// FormatSimulationExtended renders the extended simulation result including
// before/after table and risk warnings.
func FormatSimulationExtended(result *SimulationResult) string {
	if result == nil {
		return ""
	}

	var sections []string

	// Before/After comparison table.
	sections = append(sections, "Simulation Comparison:")
	sections = append(sections, fmt.Sprintf("  %-20s %12s %12s", "Field", "Before", "After"))
	sections = append(sections, fmt.Sprintf("  %-20s %12d %12d", "Desired Replicas", result.Before.DesiredReplicas, result.After.DesiredReplicas))
	sections = append(sections, fmt.Sprintf("  %-20s %12s %12s", "Health", result.Before.Health, result.After.Health))
	sections = append(sections, fmt.Sprintf("  %-20s %12d %12d", "Health Score", result.Before.HealthScore, result.After.HealthScore))
	sections = append(sections, fmt.Sprintf("  %-20s %12v %12v", "Scaling Limited", result.Before.ScalingLimited, result.After.ScalingLimited))

	// Risk warnings.
	if len(result.RiskWarnings) > 0 {
		sections = append(sections, "")
		sections = append(sections, "Risk Warnings:")
		for _, w := range result.RiskWarnings {
			sections = append(sections, fmt.Sprintf("  WARNING: %s", w))
		}
	}

	// Time-series projection.
	if len(result.TimeSeriesProjection) > 0 {
		sections = append(sections, "")
		sections = append(sections, "Projected Trajectory:")
		sections = append(sections, FormatTrajectoryASCII(result.TimeSeriesProjection, 40))
	}

	join := ""
	for i, s := range sections {
		if i > 0 {
			join += "\n"
		}
		join += s
	}
	return join
}
