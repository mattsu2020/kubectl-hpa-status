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

func TestBehaviorDisabledAndRollingPeriods(t *testing.T) {
	for _, replicas := range [][2]int32{{2, 10}, {10, 2}} {
		disabled := behaviorDirection{SelectPolicy: "Disabled", Policies: []behaviorPolicyOutput{{Type: "Pods", Value: 4, PeriodSeconds: 15}}}
		path := estimateBehaviorPath(replicas[0], replicas[1], disabled, disabled)
		if len(path) != 1 || path[0].Replicas != replicas[0] {
			t.Fatalf("disabled scaling changed replicas: %+v", path)
		}
	}
	for _, selection := range []string{"Max", "Min"} {
		rules := behaviorDirection{SelectPolicy: selection, Policies: []behaviorPolicyOutput{{Type: "Pods", Value: 1, PeriodSeconds: 15}, {Type: "Pods", Value: 10, PeriodSeconds: 60}}}
		path := estimateBehaviorPath(2, 50, rules, behaviorDirection{})
		for _, point := range path {
			if selection == "Max" && point.AfterSeconds < 75 && point.Replicas > 11+point.AfterSeconds/15 {
				t.Fatalf("reused the 60s allowance too early: %+v", path)
			}
			if selection == "Min" && point.Replicas > 2+point.AfterSeconds/15 {
				t.Fatalf("exceeded the 15s allowance: %+v", path)
			}
		}
		if len(path) == 0 {
			t.Fatal("missing estimated path")
		}
	}
	rules := behaviorDirection{SelectPolicy: "Max", Policies: []behaviorPolicyOutput{{Type: "Pods", Value: 1, PeriodSeconds: 15}, {Type: "Pods", Value: 10, PeriodSeconds: 20}}}
	path := estimateBehaviorPath(2, 30, rules, behaviorDirection{})
	if len(path) < 3 || path[2].AfterSeconds != 35 {
		t.Fatalf("period expiry must occur at 35s: %+v", path)
	}
}

func TestBehaviorPercentDecreaseAndOverflow(t *testing.T) {
	down := behaviorDirection{Policies: []behaviorPolicyOutput{{Type: "Percent", Value: 33, PeriodSeconds: 15}}}
	if path := estimateBehaviorPath(10, 7, behaviorDirection{}, down); len(path) != 1 || path[0].Replicas != 7 {
		t.Fatalf("33%% decrease must leave 7 replicas: %+v", path)
	}
	up := behaviorDirection{Policies: []behaviorPolicyOutput{{Type: "Percent", Value: 100, PeriodSeconds: 15}}}
	path := estimateBehaviorPath(1<<30, 1<<31-1, up, behaviorDirection{})
	if len(path) != 1 || path[0].Replicas != 1<<31-1 {
		t.Fatalf("overflowed replica estimate: %+v", path)
	}
}
