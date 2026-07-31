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
type Snapshot struct {
	client kubernetes.Interface
	hpa    autoscalingv2.HorizontalPodAutoscaler

	targetOnce sync.Once
	target     Value[*kube.ScaleTargetInfo]

	podsOnce sync.Once
	pods     Value[[]corev1.Pod]
}

// New creates a request-scoped workload observation snapshot.
func New(client kubernetes.Interface, hpa *autoscalingv2.HorizontalPodAutoscaler) *Snapshot {
	if hpa == nil {
		return &Snapshot{client: client}
	}
	return &Snapshot{client: client, hpa: *hpa.DeepCopy()}
}

// ScaleTarget returns the memoized target observation.
func (s *Snapshot) ScaleTarget(ctx context.Context) Value[*kube.ScaleTargetInfo] {
	if s == nil || s.client == nil {
		return Value[*kube.ScaleTargetInfo]{State: StateUnavailable, Err: fmt.Errorf("kubernetes client is unavailable")}
	}
	s.targetOnce.Do(func() {
		info, err := kube.FetchScaleTargetInfo(ctx, s.client, s.hpa.Namespace, s.hpa.Spec.ScaleTargetRef)
		switch {
		case err != nil:
			s.target = Value[*kube.ScaleTargetInfo]{State: StateUnavailable, Err: err}
		case info == nil:
			s.target = Value[*kube.ScaleTargetInfo]{State: StateNotApplicable}
		default:
			s.target = Value[*kube.ScaleTargetInfo]{Data: info, State: StateKnown}
		}
	})
	return s.target
}

// Pods returns the target's memoized raw Pod objects.
func (s *Snapshot) Pods(ctx context.Context) Value[[]corev1.Pod] {
	if s == nil {
		return Value[[]corev1.Pod]{State: StateUnavailable, Err: fmt.Errorf("observation snapshot is unavailable")}
	}
	s.podsOnce.Do(func() {
		target := s.ScaleTarget(ctx)
		switch target.State {
		case StateUnavailable:
			s.pods = Value[[]corev1.Pod]{State: StateUnavailable, Err: target.Err}
			return
		case StateNotApplicable:
			s.pods = Value[[]corev1.Pod]{State: StateNotApplicable}
			return
		}
		if target.Data.SelectorStr == "" {
			s.pods = Value[[]corev1.Pod]{State: StateNotApplicable}
			return
		}
		pods, err := kube.FetchPodObjectsForSelector(ctx, s.client, s.hpa.Namespace, target.Data.SelectorStr)
		if err != nil {
			s.pods = Value[[]corev1.Pod]{State: StateUnavailable, Err: err}
			return
		}
		s.pods = Value[[]corev1.Pod]{Data: pods, State: StateKnown}
	})
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
