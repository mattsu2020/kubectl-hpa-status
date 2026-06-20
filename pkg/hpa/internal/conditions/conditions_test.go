package conditions

import (
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/clock"
)

func TestFind_NilHPA(t *testing.T) {
	if got := Find(nil, ScalingActive); got != nil {
		t.Fatalf("Find(nil, ...) = %v, want nil", got)
	}
}

func TestFind_ReturnsMatchingCondition(t *testing.T) {
	wantReason := "DesiredWithinRange"
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: AbleToScale, Reason: "ReadyForNewScale"},
				{Type: ScalingActive, Reason: wantReason},
			},
		},
	}

	got := Find(hpa, ScalingActive)
	if got == nil {
		t.Fatalf("Find(ScalingActive) = nil, want match")
	}
	if got.Reason != wantReason {
		t.Fatalf("Reason = %q, want %q", got.Reason, wantReason)
	}
}

func TestFind_ReturnsNilWhenAbsent(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: AbleToScale},
			},
		},
	}
	if got := Find(hpa, ScalingActive); got != nil {
		t.Fatalf("Find(ScalingActive) = %v, want nil (absent)", got)
	}
}

func TestScaleDownStabilizationWindow(t *testing.T) {
	window := int32(300)

	t.Run("nil behavior returns nil", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		if got := ScaleDownStabilizationWindow(hpa); got != nil {
			t.Fatalf("got %v, want nil", *got)
		}
	})
	t.Run("nil scaleDown returns nil", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{},
			},
		}
		if got := ScaleDownStabilizationWindow(hpa); got != nil {
			t.Fatalf("got %v, want nil", *got)
		}
	})
	t.Run("returns configured window", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: &window,
					},
				},
			},
		}
		got := ScaleDownStabilizationWindow(hpa)
		if got == nil || *got != window {
			t.Fatalf("got = %v, want %d", got, window)
		}
	})
}

func TestEstimateStabilizationRemaining(t *testing.T) {
	window := int32(300)
	lastScale := metav1.Date(2024, time.January, 2, 3, 0, 0, 0, time.UTC)
	now := lastScale.Add(120 * time.Second)
	defer clock.SetForTest(now)()

	t.Run("nil when AbleToScale absent", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
				},
			},
		}
		if got := EstimateStabilizationRemaining(hpa); got != nil {
			t.Fatalf("got %v, want nil", *got)
		}
	})
	t.Run("nil when reason is not ScaleDownStabilized", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Status: autoscalingv2.HorizontalPodAutoscalerStatus{
				Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: AbleToScale, Reason: "ReadyForNewScale"},
				},
			},
		}
		if got := EstimateStabilizationRemaining(hpa); got != nil {
			t.Fatalf("got %v, want nil", *got)
		}
	})
	t.Run("nil when window absent", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Status: autoscalingv2.HorizontalPodAutoscalerStatus{
				Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: AbleToScale, Reason: "ScaleDownStabilized"},
				},
			},
		}
		if got := EstimateStabilizationRemaining(hpa); got != nil {
			t.Fatalf("got %v, want nil", *got)
		}
	})
	t.Run("nil when LastScaleTime absent", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
				},
			},
			Status: autoscalingv2.HorizontalPodAutoscalerStatus{
				Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: AbleToScale, Reason: "ScaleDownStabilized"},
				},
			},
		}
		if got := EstimateStabilizationRemaining(hpa); got != nil {
			t.Fatalf("got %v, want nil", *got)
		}
	})
	t.Run("returns remaining seconds", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
				},
			},
			Status: autoscalingv2.HorizontalPodAutoscalerStatus{
				LastScaleTime: &lastScale,
				Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: AbleToScale, Reason: "ScaleDownStabilized"},
				},
			},
		}
		// window=300, elapsed=120 -> remaining=180
		got := EstimateStabilizationRemaining(hpa)
		if got == nil {
			t.Fatalf("got nil, want 180")
		}
		if *got != 180 {
			t.Fatalf("remaining = %d, want 180", *got)
		}
	})
	t.Run("clamps negative remaining to zero", func(t *testing.T) {
		late := lastScale.Add(400 * time.Second) // window already expired
		defer clock.SetForTest(late)()
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
				},
			},
			Status: autoscalingv2.HorizontalPodAutoscalerStatus{
				LastScaleTime: &lastScale,
				Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: AbleToScale, Reason: "ScaleDownStabilized"},
				},
			},
		}
		got := EstimateStabilizationRemaining(hpa)
		if got == nil {
			t.Fatalf("got nil, want 0")
		}
		if *got != 0 {
			t.Fatalf("remaining = %d, want 0 (clamped)", *got)
		}
	})
}
