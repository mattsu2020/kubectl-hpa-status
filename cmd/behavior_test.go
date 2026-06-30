package cmd

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func TestMinPositivePeriod(t *testing.T) {
	t.Run("nil policies", func(t *testing.T) {
		if got := minPositivePeriod(nil); got != 0 {
			t.Fatalf("minPositivePeriod(nil) = %d, want 0", got)
		}
	})

	t.Run("picks smallest positive period", func(t *testing.T) {
		policies := []behaviorPolicyOutput{
			{PeriodSeconds: 60},
			{PeriodSeconds: 30},
			{PeriodSeconds: 15},
		}
		if got := minPositivePeriod(policies); got != 15 {
			t.Fatalf("minPositivePeriod = %d, want 15", got)
		}
	})

	t.Run("ignores non-positive periods", func(t *testing.T) {
		policies := []behaviorPolicyOutput{
			{PeriodSeconds: 0},
			{PeriodSeconds: -5},
			{PeriodSeconds: 45},
		}
		if got := minPositivePeriod(policies); got != 45 {
			t.Fatalf("minPositivePeriod = %d, want 45", got)
		}
	})

	t.Run("all non-positive returns zero", func(t *testing.T) {
		policies := []behaviorPolicyOutput{
			{PeriodSeconds: 0},
			{PeriodSeconds: -1},
		}
		if got := minPositivePeriod(policies); got != 0 {
			t.Fatalf("minPositivePeriod = %d, want 0", got)
		}
	})
}

func TestBehaviorStepDelta(t *testing.T) {
	t.Run("no policies returns max int32", func(t *testing.T) {
		// When no scaling policies are configured, the controller allows the
		// maximum step in a single move; encode that as the sentinel so the
		// path short-circuits to the desired replica count.
		rules := behaviorDirection{SelectPolicy: "", Policies: nil}
		got := behaviorStepDelta(10, rules, 1)
		if got != int32(1<<31-1) {
			t.Fatalf("behaviorStepDelta with no policies = %d, want %d", got, int32(1<<31-1))
		}
	})

	t.Run("absolute policy default max select", func(t *testing.T) {
		rules := behaviorDirection{
			SelectPolicy: "",
			Policies: []behaviorPolicyOutput{
				{Type: string(autoscalingv2.PodsScalingPolicy), Value: 4},
				{Type: string(autoscalingv2.PodsScalingPolicy), Value: 2},
			},
		}
		// Default selectPolicy is Max, so the larger delta (4) wins.
		if got := behaviorStepDelta(10, rules, 1); got != 4 {
			t.Fatalf("behaviorStepDelta = %d, want 4", got)
		}
	})

	t.Run("absolute policy min select", func(t *testing.T) {
		rules := behaviorDirection{
			SelectPolicy: string(autoscalingv2.MinChangePolicySelect),
			Policies: []behaviorPolicyOutput{
				{Type: string(autoscalingv2.PodsScalingPolicy), Value: 4},
				{Type: string(autoscalingv2.PodsScalingPolicy), Value: 2},
			},
		}
		if got := behaviorStepDelta(10, rules, 1); got != 2 {
			t.Fatalf("behaviorStepDelta with Min = %d, want 2", got)
		}
	})

	t.Run("percent policy rounds up", func(t *testing.T) {
		rules := behaviorDirection{
			SelectPolicy: "",
			Policies: []behaviorPolicyOutput{
				{Type: string(autoscalingv2.PercentScalingPolicy), Value: 33},
			},
		}
		// ceil(10 * 33 / 100) = ceil(3.3) = 4
		if got := behaviorStepDelta(10, rules, 1); got != 4 {
			t.Fatalf("behaviorStepDelta percent = %d, want 4", got)
		}
	})

	t.Run("zero deltas return zero", func(t *testing.T) {
		rules := behaviorDirection{
			SelectPolicy: "",
			Policies: []behaviorPolicyOutput{
				{Type: string(autoscalingv2.PodsScalingPolicy), Value: 0},
			},
		}
		if got := behaviorStepDelta(10, rules, 1); got != 0 {
			t.Fatalf("behaviorStepDelta = %d, want 0", got)
		}
	})
}

func TestEstimateBehaviorPath(t *testing.T) {
	t.Run("returns nil for non-positive replicas", func(t *testing.T) {
		scaleUp := behaviorDirection{Policies: []behaviorPolicyOutput{{Value: 1, PeriodSeconds: 60}}}
		if got := estimateBehaviorPath(0, 10, scaleUp, behaviorDirection{}); got != nil {
			t.Fatalf("estimateBehaviorPath(0,...) = %v, want nil", got)
		}
		if got := estimateBehaviorPath(10, 0, scaleUp, behaviorDirection{}); got != nil {
			t.Fatalf("estimateBehaviorPath(...,0) = %v, want nil", got)
		}
	})

	t.Run("returns nil when current equals desired", func(t *testing.T) {
		scaleUp := behaviorDirection{Policies: []behaviorPolicyOutput{{Value: 1, PeriodSeconds: 60}}}
		if got := estimateBehaviorPath(5, 5, scaleUp, behaviorDirection{}); got != nil {
			t.Fatalf("estimateBehaviorPath(5,5,...) = %v, want nil", got)
		}
	})

	t.Run("scale up reaches desired in steps", func(t *testing.T) {
		scaleUp := behaviorDirection{
			SelectPolicy: "",
			Policies:     []behaviorPolicyOutput{{Type: string(autoscalingv2.PodsScalingPolicy), Value: 3, PeriodSeconds: 60}},
		}
		path := estimateBehaviorPath(2, 10, scaleUp, behaviorDirection{})
		if len(path) == 0 {
			t.Fatal("expected a non-empty path")
		}
		// Last point must be the desired replica count.
		last := path[len(path)-1]
		if last.Replicas != 10 {
			t.Fatalf("last path replica = %d, want 10", last.Replicas)
		}
		// First point must advance from current=2 by delta=3 → 5.
		if path[0].Replicas != 5 {
			t.Fatalf("first path replica = %d, want 5", path[0].Replicas)
		}
		// Points must be monotonically increasing toward desired.
		for i := 1; i < len(path); i++ {
			if path[i].Replicas < path[i-1].Replicas {
				t.Fatalf("path not monotonic at index %d: %d < %d", i, path[i].Replicas, path[i-1].Replicas)
			}
		}
	})

	t.Run("scale down uses scaleDown direction", func(t *testing.T) {
		scaleDown := behaviorDirection{
			SelectPolicy: "",
			Policies:     []behaviorPolicyOutput{{Type: string(autoscalingv2.PodsScalingPolicy), Value: 2, PeriodSeconds: 60}},
		}
		path := estimateBehaviorPath(10, 4, behaviorDirection{}, scaleDown)
		if len(path) == 0 {
			t.Fatal("expected a non-empty down path")
		}
		last := path[len(path)-1]
		if last.Replicas != 4 {
			t.Fatalf("last path replica = %d, want 4", last.Replicas)
		}
		// Points must be monotonically decreasing toward desired.
		for i := 1; i < len(path); i++ {
			if path[i].Replicas > path[i-1].Replicas {
				t.Fatalf("down path not monotonic at index %d: %d > %d", i, path[i].Replicas, path[i-1].Replicas)
			}
		}
	})

	t.Run("no policies jumps straight to desired", func(t *testing.T) {
		// When stepSeconds is 0 (no positive-period policies), the path is a
		// single point at the desired count.
		scaleUp := behaviorDirection{Policies: nil}
		path := estimateBehaviorPath(2, 10, scaleUp, behaviorDirection{})
		if len(path) != 1 {
			t.Fatalf("expected single-point path, got %d points", len(path))
		}
		if path[0].Replicas != 10 {
			t.Fatalf("single-point replica = %d, want 10", path[0].Replicas)
		}
		if path[0].AfterSeconds != 0 {
			t.Fatalf("single-point AfterSeconds = %d, want 0", path[0].AfterSeconds)
		}
	})
}
