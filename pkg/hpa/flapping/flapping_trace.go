package flapping

// TraceInput is the minimal recorded-history projection that the trace
// flapping detector needs. It is deliberately decoupled from the richer
// TimelineTrace types so this package stays a self-contained leaf domain
// (the cmd/ layer builds it from a TimelineTrace).
type TraceInput struct {
	// Namespace is the HPA namespace, copied through to the report.
	Namespace string
	// Name is the HPA name, copied through to the report.
	Name string
	// Desired is the ordered desiredReplicas series from the recording.
	// The detector walks it in index order; timestamps are not needed
	// because the classification is count-based, not time-based.
	Desired []int32
}

// TraceReport is the outcome of AnalyzeTrace. It summarises how much the
// desired-replica series flapped and classifies the severity.
//
// The JSON/YAML field names are part of the analyze-record --detect flapping
// public contract; do not rename them.
type TraceReport struct {
	// Namespace is the HPA namespace.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA name.
	Name string `json:"name" yaml:"name"`
	// Snapshots is the number of snapshots in the input series.
	Snapshots int `json:"snapshots" yaml:"snapshots"`
	// DesiredChanges is the number of times desiredReplicas changed
	// between consecutive snapshots.
	DesiredChanges int `json:"desiredChanges" yaml:"desiredChanges"`
	// DirectionFlips is the number of times the scaling direction reversed
	// (scale-up followed by scale-down, or vice versa).
	DirectionFlips int `json:"directionFlips" yaml:"directionFlips"`
	// ReplicaMin is the smallest desiredReplicas value in the trace.
	ReplicaMin int32 `json:"replicaMin,omitempty" yaml:"replicaMin,omitempty"`
	// ReplicaMax is the largest desiredReplicas value in the trace.
	ReplicaMax int32 `json:"replicaMax,omitempty" yaml:"replicaMax,omitempty"`
	// Level is the flapping severity: LOW, MEDIUM, HIGH, or CRITICAL.
	Level string `json:"level" yaml:"level"`
	// Suggestions are remediation hints when flapping is observed.
	Suggestions []string `json:"suggestions,omitempty" yaml:"suggestions,omitempty"`
}

// Flapping severity thresholds for recorded-trace analysis. An HPA is
// classified by the number of scale-direction reversals (direction flips) and
// the total desiredReplicas changes across the recording.
const (
	flappingCriticalDirectionFlips = 6
	flappingCriticalDesiredChanges = 15
	flappingHighDirectionFlips     = 3
	flappingHighDesiredChanges     = 8
	flappingMediumDesiredChanges   = 4
)

// AnalyzeTrace walks a recorded desired-replica series and classifies its
// flapping severity. The function is pure: it does not modify the input slice
// and always returns a non-nil report.
//
// The thresholds and classification are identical to the legacy
// analyzeTraceFlapping helper that previously lived in cmd/analyze_record.go,
// so JSON field names and level boundaries are preserved.
func AnalyzeTrace(in TraceInput) TraceReport {
	report := TraceReport{
		Namespace: in.Namespace,
		Name:      in.Name,
		Snapshots: len(in.Desired),
		Level:     "LOW",
	}
	report.ReplicaMin, report.ReplicaMax = replicaRange(in.Desired)

	var lastDesired int32
	var lastDirection int32
	for i, desired := range in.Desired {
		if i == 0 {
			lastDesired = desired
			continue
		}
		if desired == lastDesired {
			continue
		}
		report.DesiredChanges++
		direction := int32(1)
		if desired < lastDesired {
			direction = -1
		}
		if lastDirection != 0 && direction != lastDirection {
			report.DirectionFlips++
		}
		lastDirection = direction
		lastDesired = desired
	}

	switch {
	case report.DirectionFlips >= flappingCriticalDirectionFlips || report.DesiredChanges >= flappingCriticalDesiredChanges:
		report.Level = "CRITICAL"
	case report.DirectionFlips >= flappingHighDirectionFlips || report.DesiredChanges >= flappingHighDesiredChanges:
		report.Level = "HIGH"
	case report.DirectionFlips > 0 || report.DesiredChanges >= flappingMediumDesiredChanges:
		report.Level = "MEDIUM"
	}

	if report.Level != "LOW" {
		report.Suggestions = append(report.Suggestions,
			"review scaleDown stabilization window and configured tolerance",
			"check whether target utilization is too close to normal traffic baseline",
		)
	}
	return report
}

// replicaRange returns the smallest and largest values in desired. An empty
// input yields (0, 0). A single linear pass is used so the input is never
// copied, reordered, or mutated.
func replicaRange(desired []int32) (int32, int32) {
	if len(desired) == 0 {
		return 0, 0
	}
	minimum, maximum := desired[0], desired[0]
	for _, v := range desired[1:] {
		if v < minimum {
			minimum = v
		}
		if v > maximum {
			maximum = v
		}
	}
	return minimum, maximum
}
