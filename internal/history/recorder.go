package history

import (
	"fmt"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

// Clock supplies one consistent observation time per record operation.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// SnapshotStore is the persistence surface required by Recorder.
type SnapshotStore interface {
	Append(namespace, name string, snapshot healthtrend.HealthSnapshot) error
	LoadAt(namespace, name string, since time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error)
	PruneAt(namespace, name string, retention time.Duration, now time.Time) error
}

// RecordInput is independent of the large public Analysis DTO.
type RecordInput struct {
	Namespace       string
	Name            string
	HealthScore     int
	HealthState     string
	DesiredReplicas int32
	CurrentReplicas int32
	Stabilizing     bool
	Since           time.Duration
	Retention       time.Duration
}

// RecordResult contains the optional trend and non-fatal persistence warnings.
type RecordResult struct {
	Trend    *healthtrend.Result
	Warnings []string
}

// Recorder owns the Append -> Prune -> Load -> Analyze workflow shared by
// status and list. Persistence errors are diagnostic warnings, not fatal
// analysis failures.
type Recorder struct {
	store SnapshotStore
	clock Clock
}

// NewRecorder creates a recorder. A nil clock selects the real clock.
func NewRecorder(store SnapshotStore, clock Clock) *Recorder {
	if clock == nil {
		clock = realClock{}
	}
	return &Recorder{store: store, clock: clock}
}

// RecordAndAnalyze persists one snapshot and analyzes retained history.
func (r *Recorder) RecordAndAnalyze(input RecordInput) RecordResult {
	if r == nil || r.store == nil {
		return RecordResult{Warnings: []string{"health trend store unavailable"}}
	}
	now := r.clock.Now()
	snapshot := healthtrend.HealthSnapshot{
		Timestamp:       now,
		HealthScore:     input.HealthScore,
		HealthState:     input.HealthState,
		DesiredReplicas: input.DesiredReplicas,
		CurrentReplicas: input.CurrentReplicas,
		Stabilizing:     input.Stabilizing,
	}

	var result RecordResult
	if err := r.store.Append(input.Namespace, input.Name, snapshot); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("health trend append failed: %v", err))
	}
	if err := r.store.PruneAt(input.Namespace, input.Name, input.Retention, now); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("health trend prune failed: %v", err))
	}
	snapshots, err := r.store.LoadAt(input.Namespace, input.Name, input.Since, now)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("health trend load warning: %v", err))
	}
	if len(snapshots) > 0 {
		trend := healthtrend.AnalyzeHealthTrend(snapshots)
		result.Trend = &trend
	}
	return result
}
