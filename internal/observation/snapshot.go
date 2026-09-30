// Package observation collects request-scoped Kubernetes observations and
// preserves whether each value is known, unavailable, or not applicable.
package observation

import (
	"context"
	"fmt"
	"sync"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// State distinguishes observed absence from observation failure.
type State string

const ( //nolint:revive // The package-level State documentation describes this enum.
	// StateKnown means the value was observed successfully.
	StateKnown         State = "known"
	StateUnavailable   State = "unavailable"    //nolint:revive // State enum value.
	StateNotApplicable State = "not-applicable" //nolint:revive // State enum value.
)

// Value is one typed observation and its availability state.
type Value[T any] struct {
	Data  T
	State State
	Err   error
}

// Known reports whether the API read completed successfully.
func (v Value[T]) Known() bool { return v.State == StateKnown }

// Snapshot memoizes workload reads for one HPA report. Returned data is
// immutable by contract; consumers that need to mutate a slice must copy it.
//
// Every observation — success, not-applicable, or failure — is memoized for
// the snapshot's lifetime. A snapshot is request-scoped (one report or one
// watch/TUI fetch), so a later fetch already retries through a fresh
// snapshot, while the Kubernetes reads underneath carry their own
// transient-error retries (see kube.retryTransient). Memoizing failures keeps
// the derived views (pod info, pending details, container state) from
// re-running the same failing API chain several times per report.
type Snapshot struct {
	client  kubernetes.Interface
	cluster *ClusterSnapshot
	hpa     autoscalingv2.HorizontalPodAutoscaler

	targetMu sync.Mutex
	targetOK bool
	target   Value[*kube.ScaleTargetInfo]

	podsMu sync.Mutex
	podsOK bool
	pods   Value[[]corev1.Pod]

	replicaSetsMu sync.Mutex
	replicaSetsOK bool
	replicaSets   Value[[]kube.ReplicaSetInfo]
}

// New creates a request-scoped workload observation snapshot.
func New(client kubernetes.Interface, hpa *autoscalingv2.HorizontalPodAutoscaler, clusters ...*ClusterSnapshot) *Snapshot {
	cluster := &ClusterSnapshot{}
	if len(clusters) > 0 && clusters[0] != nil {
		cluster = clusters[0]
	}

	if hpa == nil {
		return &Snapshot{client: client, cluster: cluster}
	}
	return &Snapshot{client: client, cluster: cluster, hpa: *hpa.DeepCopy()}
}

// ScaleTarget returns the memoized target observation.
func (s *Snapshot) ScaleTarget(ctx context.Context) Value[*kube.ScaleTargetInfo] {
	if s == nil || s.client == nil {
		return Value[*kube.ScaleTargetInfo]{State: StateUnavailable, Err: fmt.Errorf("kubernetes client is unavailable")}
	}
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	if s.targetOK {
		return s.target
	}
	info, err := kube.FetchScaleTargetInfo(ctx, s.client, s.hpa.Namespace, s.hpa.Spec.ScaleTargetRef)
	if err != nil {
		s.target = Value[*kube.ScaleTargetInfo]{State: StateUnavailable, Err: err}
		s.targetOK = true
		return s.target
	}
	if info == nil {
		s.target = Value[*kube.ScaleTargetInfo]{State: StateNotApplicable}
	} else {
		s.target = Value[*kube.ScaleTargetInfo]{Data: info, State: StateKnown}
	}
	s.targetOK = true
	return s.target
}

// Pods returns the target's memoized raw Pod objects.
func (s *Snapshot) Pods(ctx context.Context) Value[[]corev1.Pod] {
	if s == nil {
		return Value[[]corev1.Pod]{State: StateUnavailable, Err: fmt.Errorf("observation snapshot is unavailable")}
	}
	s.podsMu.Lock()
	defer s.podsMu.Unlock()
	if s.podsOK {
		return s.pods
	}
	target := s.ScaleTarget(ctx)
	switch target.State {
	case StateUnavailable:
		return Value[[]corev1.Pod]{State: StateUnavailable, Err: target.Err}
	case StateNotApplicable:
		s.pods = Value[[]corev1.Pod]{State: StateNotApplicable}
		s.podsOK = true
		return s.pods
	}
	if target.Data.SelectorStr == "" {
		s.pods = Value[[]corev1.Pod]{State: StateNotApplicable}
		s.podsOK = true
		return s.pods
	}
	pods, err := kube.FetchPodObjectsForSelector(ctx, s.client, s.hpa.Namespace, target.Data.SelectorStr)
	if err != nil {
		s.pods = Value[[]corev1.Pod]{State: StateUnavailable, Err: err}
		s.podsOK = true
		return s.pods
	}
	s.pods = Value[[]corev1.Pod]{Data: pods, State: StateKnown}
	s.podsOK = true
	return s.pods
}

// PodInfos derives the compact analysis view without another API read.
func (s *Snapshot) PodInfos(ctx context.Context) Value[[]kube.PodInfo] {
	pods := s.Pods(ctx)
	return mapValue(pods, kube.PodInfosFromPods)
}

// PendingPods derives pending scheduling details without another API read.
func (s *Snapshot) PendingPods(ctx context.Context) Value[[]kube.PendingPodDetail] {
	pods := s.Pods(ctx)
	return mapValue(pods, kube.PendingPodDetailsFromPods)
}

// ContainerStatuses derives container state without another API read.
func (s *Snapshot) ContainerStatuses(ctx context.Context) Value[[]kube.ContainerStatusDetail] {
	pods := s.Pods(ctx)
	return mapValue(pods, kube.ContainerStatusesFromPods)
}

func mapValue[A, B any](source Value[A], convert func(A) B) Value[B] {
	if source.State != StateKnown {
		return Value[B]{State: source.State, Err: source.Err}
	}
	return Value[B]{Data: convert(source.Data), State: StateKnown}
}

// ReplicaSets returns the memoized scale-path replica status.
func (s *Snapshot) ReplicaSets(ctx context.Context) Value[[]kube.ReplicaSetInfo] {
	if s == nil {
		return Value[[]kube.ReplicaSetInfo]{State: StateUnavailable, Err: fmt.Errorf("observation snapshot is unavailable")}
	}
	s.replicaSetsMu.Lock()
	defer s.replicaSetsMu.Unlock()
	if s.replicaSetsOK {
		return s.replicaSets
	}
	target := s.ScaleTarget(ctx)
	if !target.Known() {
		return Value[[]kube.ReplicaSetInfo]{State: target.State, Err: target.Err}
	}
	var items []kube.ReplicaSetInfo
	if target.Data.Kind == "ReplicaSet" {
		info := target.Data
		items = []kube.ReplicaSetInfo{{Name: info.Name, DesiredReplicas: info.DesiredReplicas, CurrentReplicas: info.Replicas, ReadyReplicas: info.ReadyReplicas}}
	} else {
		var err error
		items, err = kube.FetchReplicaSetsForScaleTarget(ctx, s.client, s.hpa.Namespace, s.hpa.Spec.ScaleTargetRef, target.Data.SelectorStr)
		if err != nil {
			s.replicaSets = Value[[]kube.ReplicaSetInfo]{State: StateUnavailable, Err: err}
			s.replicaSetsOK = true
			return s.replicaSets
		}
	}
	s.replicaSets = Value[[]kube.ReplicaSetInfo]{Data: items, State: StateKnown}
	s.replicaSetsOK = true
	return s.replicaSets
}

// ClusterSnapshot shares immutable cluster observations across HPAs in one run.
// All consumers must use the same cluster client. A new polling tick needs a new
// snapshot so neither successful observations nor failures become stale.
type ClusterSnapshot struct {
	once           sync.Once
	data           *kube.ClusterResourceSnapshot
	err            error
	autoscalerOnce sync.Once
	autoscaler     bool
	autoscalerErr  error
}

// ClusterHeadroom projects target placement without repeating the cluster lists.
func (s *Snapshot) ClusterHeadroom(ctx context.Context, podSpec *corev1.PodSpec) (*kube.ClusterResourceHeadroom, error) {
	if s == nil || s.client == nil || s.cluster == nil {
		return nil, fmt.Errorf("cluster observation is unavailable")
	}
	s.cluster.once.Do(func() { s.cluster.data, s.cluster.err = kube.FetchClusterResourceSnapshot(ctx, s.client) })
	if s.cluster.err != nil {
		return nil, s.cluster.err
	}
	return s.cluster.data.HeadroomForPod(podSpec), nil
}

// ClusterAutoscaler shares detection across the same request and reuses Nodes.
func (s *Snapshot) ClusterAutoscaler(ctx context.Context) (bool, error) {
	if s == nil || s.client == nil || s.cluster == nil {
		return false, fmt.Errorf("cluster observation is unavailable")
	}
	s.cluster.once.Do(func() { s.cluster.data, s.cluster.err = kube.FetchClusterResourceSnapshot(ctx, s.client) })
	s.cluster.autoscalerOnce.Do(func() {
		s.cluster.autoscaler, s.cluster.autoscalerErr = s.cluster.data.DetectClusterAutoscaler(ctx, s.client)
	})
	return s.cluster.autoscaler, s.cluster.autoscalerErr
}
