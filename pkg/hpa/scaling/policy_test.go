package scaling

import (
	"math"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func TestRollingPolicyLimit(t *testing.T) {
	policies := []autoscalingv2.HPAScalingPolicy{{Type: autoscalingv2.PodsScalingPolicy, Value: 1, PeriodSeconds: 15}, {Type: autoscalingv2.PodsScalingPolicy, Value: 10, PeriodSeconds: 60}}
	for _, test := range []struct {
		scaleUp   bool
		current   int32
		change    int32
		selection autoscalingv2.ScalingPolicySelect
		now       int64
		want      int32
	}{
		{true, 12, 10, autoscalingv2.MaxChangePolicySelect, 30, 13},
		{true, 12, 10, autoscalingv2.MaxChangePolicySelect, 75, 22},
		{true, 3, 1, autoscalingv2.MinChangePolicySelect, 30, 4},
		{false, 20, -10, autoscalingv2.MaxChangePolicySelect, 30, 19},
		{false, 20, -10, autoscalingv2.MaxChangePolicySelect, 75, 10},
		{false, 29, -1, autoscalingv2.MinChangePolicySelect, 30, 28},
	} {
		got, ok := Limit(test.current, test.scaleUp, test.selection, policies, test.now, []Event{{TimeSeconds: 15, Change: test.change}})
		if !ok || got != test.want {
			t.Fatalf("%+v: got %d, %v", test, got, ok)
		}
	}
	if got, ok := Limit(12, true, autoscalingv2.DisabledPolicySelect, policies, 75, nil); !ok || got != 12 {
		t.Fatalf("disabled limit=%d, %v", got, ok)
	}
}

func TestPolicyReplicaBoundaries(t *testing.T) {
	for _, test := range []struct {
		current int32
		up      bool
		policy  autoscalingv2.HPAScalingPolicy
		want    int32
	}{
		{10, false, autoscalingv2.HPAScalingPolicy{Type: autoscalingv2.PercentScalingPolicy, Value: 33}, 7},
		{math.MaxInt32, true, autoscalingv2.HPAScalingPolicy{Type: autoscalingv2.PercentScalingPolicy, Value: math.MaxInt32}, math.MaxInt32},
		{2, false, autoscalingv2.HPAScalingPolicy{Type: autoscalingv2.PodsScalingPolicy, Value: 10}, 0},
	} {
		if got, ok := ReplicaLimit(test.current, test.up, test.policy); !ok || got != test.want {
			t.Fatalf("%+v: got %d, %v", test, got, ok)
		}
	}
}

func TestSelectPolicyAndUnusablePolicies(t *testing.T) {
	policies := []autoscalingv2.HPAScalingPolicy{{Type: autoscalingv2.PodsScalingPolicy, Value: 4}, {Type: autoscalingv2.PodsScalingPolicy, Value: 2}}
	for _, test := range []struct {
		up        bool
		selection autoscalingv2.ScalingPolicySelect
		want      int32
	}{
		{true, autoscalingv2.MaxChangePolicySelect, 14}, {true, autoscalingv2.MinChangePolicySelect, 12},
		{false, autoscalingv2.MaxChangePolicySelect, 6}, {false, autoscalingv2.MinChangePolicySelect, 8},
	} {
		if got, ok := Limit(10, test.up, test.selection, policies, 0, nil); !ok || got != test.want {
			t.Fatalf("%+v: %d, %v", test, got, ok)
		}
	}
	for _, policy := range []autoscalingv2.HPAScalingPolicy{{Type: autoscalingv2.PodsScalingPolicy, Value: 0}, {Type: "Unsupported", Value: 1}} {
		if _, ok := ReplicaLimit(10, true, policy); ok {
			t.Fatalf("accepted unusable policy: %+v", policy)
		}
	}
	if _, ok := Limit(10, true, autoscalingv2.MaxChangePolicySelect, nil, 0, nil); ok {
		t.Fatal("accepted empty policies")
	}
}
