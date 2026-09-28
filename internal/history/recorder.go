package history

import (
	"context"
	"fmt"
	"time"

	sharedclock "github.com/mattsu2020/kubectl-hpa-status/pkg/clock"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

// Clock supplies one consistent observation time per record operation.
type Clock interface {
	Now() time.Time
}

type sharedClock struct{}

func (sharedClock) Now() time.Time { return sharedclock.Now() }

// SnapshotStore is the persistence surface required by Recorder. Every
// operation carries a context so lock waits and file I/O are cancellable when
// the owning command exits.
type SnapshotStore interface {
	Append(ctx context.Context, key SnapshotKey, snapshot healthtrend.HealthSnapshot) error
	LoadAt(ctx context.Context, key SnapshotKey, since time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error)
	PruneAt(ctx context.Context, key SnapshotKey, retention time.Duration, now time.Time) error
}

type transactionalSnapshotStore interface {
	RecordAndLoad(ctx context.Context, key SnapshotKey, snapshot healthtrend.HealthSnapshot, retention, since time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error)
}

// RecordInput is independent of the large public Analysis DTO.
type RecordInput struct {
	// Cluster is the local cluster identity (kubeconfig context or cluster
	// name). It keeps same-named HPAs of different clusters from merging.
	Cluster string
	// UID is the HPA object UID; a recreated HPA starts a new generation.
	UID       string
	Namespace string
	Name      string

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
		clock = sharedClock{}
	}
	return &Recorder{store: store, clock: clock}
}

// RecordAndAnalyze persists one snapshot and analyzes retained history. The
// context bounds the underlying lock wait and file I/O; cancellation surfaces
// as the usual persistence warnings instead of blocking past command exit.
func (r *Recorder) RecordAndAnalyze(ctx context.Context, input RecordInput) RecordResult {
	if ctx == nil {
		ctx = context.Background()
	}
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
	key := SnapshotKey{Cluster: input.Cluster, Namespace: input.Namespace, Name: input.Name, UID: input.UID}
	if store, ok := r.store.(transactionalSnapshotStore); ok {
		snapshots, err := store.RecordAndLoad(ctx, key, snapshot, input.Retention, input.Since, now)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("health trend transaction warning: %v", err))
		}
		if len(snapshots) > 0 {
			trend := healthtrend.AnalyzeHealthTrend(snapshots)
			result.Trend = &trend
		}
		return result
	}
	if err := r.store.Append(ctx, key, snapshot); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("health trend append failed: %v", err))
	}
	if err := r.store.PruneAt(ctx, key, input.Retention, now); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("health trend prune failed: %v", err))
	}
	snapshots, err := r.store.LoadAt(ctx, key, input.Since, now)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("health trend load warning: %v", err))
	}
	if len(snapshots) > 0 {
		trend := healthtrend.AnalyzeHealthTrend(snapshots)
		result.Trend = &trend
	}
	return result
}
