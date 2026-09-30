// Package scaling provides shared, bounded replica policy estimates.
package scaling

import (
	"math"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DefaultPolicies returns the autoscaling/v2 rate policies for one direction.
func DefaultPolicies(scaleUp bool) []autoscalingv2.HPAScalingPolicy {
	if !scaleUp {
		return []autoscalingv2.HPAScalingPolicy{{
			Type:          autoscalingv2.PercentScalingPolicy,
			Value:         100,
			PeriodSeconds: 15,
		}}
	}
	return []autoscalingv2.HPAScalingPolicy{
		{
			Type:          autoscalingv2.PodsScalingPolicy,
			Value:         4,
			PeriodSeconds: 15,
		},
		{
			Type:          autoscalingv2.PercentScalingPolicy,
			Value:         100,
			PeriodSeconds: 15,
		},
	}
}

// ReplicaLimit computes the replica bound from a period-start baseline.
func ReplicaLimit(current int32, scaleUp bool, policy autoscalingv2.HPAScalingPolicy) (int32, bool) {
	if policy.Value <= 0 {
		return 0, false
	}

	current64 := int64(current)
	var candidate int64
	switch policy.Type {
	case autoscalingv2.PodsScalingPolicy:
		if scaleUp {
			candidate = current64 + int64(policy.Value)
		} else {
			candidate = current64 - int64(policy.Value)
		}
	case autoscalingv2.PercentScalingPolicy:
		// Round fractionally-generated replica counts up in both directions so
		// the projection is symmetric and never underestimates how many pods
		// remain after a scale-down band. (This is a projection, deliberately
		// not a byte-for-byte reimplementation of the controller's separate
		// truncate-the-change approach.)
		multiplier := 1 + float64(policy.Value)/100
		if !scaleUp {
			multiplier = 1 - float64(policy.Value)/100
		}
		candidate = int64(math.Ceil(float64(current) * multiplier))
	default:
		return 0, false
	}

	const maxInt32Value = int64(1<<31 - 1)
	if candidate > maxInt32Value {
		candidate = maxInt32Value
	}
	if candidate < 0 {
		candidate = 0
	}
	return int32(candidate), true
}

// Event is a projected replica change. TimeSeconds is relative to the projection.
type Event struct {
	TimeSeconds int64
	Change      int32
}

// Limit combines policies with changes still inside each policy's rolling period.
// History may be nil for a single-snapshot estimate. Unknown controller history
// is never assumed to be observable; callers supply only projected events.
func Limit(current int32, scaleUp bool, selectPolicy autoscalingv2.ScalingPolicySelect, policies []autoscalingv2.HPAScalingPolicy, now int64, history []Event) (int32, bool) {
	if selectPolicy == autoscalingv2.DisabledPolicySelect {
		return current, true
	}
	var selected int32
	found := false
	for _, policy := range policies {
		baseline := int64(current)
		for _, event := range history {
			if event.TimeSeconds > now-int64(policy.PeriodSeconds) && event.TimeSeconds <= now {
				baseline -= int64(event.Change)
			}
		}
		baseline = max(int64(0), min(baseline, int64(math.MaxInt32)))
		candidate, ok := ReplicaLimit(int32(baseline), scaleUp, policy) // #nosec G115 -- clamped above.
		if !ok {
			continue
		}
		if scaleUp {
			candidate = max(current, candidate)
		} else {
			candidate = min(current, candidate)
		}
		if !found {
			selected = candidate
			found = true
			continue
		}
		if scaleUp == (selectPolicy != autoscalingv2.MinChangePolicySelect) {
			selected = max(selected, candidate)
		} else {
			selected = min(selected, candidate)
		}
	}
	return selected, found
}
