package simulate

import (
	"math"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/scaling"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// normalizeSimulatedDesired mirrors the HPA controller's ordering when it
// combines min/max bounds with scaling-rate policies. In particular, a target
// that is already outside its configured range is moved toward the range at the
// permitted rate rather than being clamped past that rate in one step.
//
// Scaling-event history is not present in an HPA object. Policy limits therefore
// use the current replica count as the beginning-of-period baseline.
func normalizeSimulatedDesired(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	desired, minReplicas, maxReplicas int32,
) (int32, bool, string) {
	current := hpa.Status.CurrentReplicas

	// A nil behavior uses the controller's legacy normalization path. Its
	// scale-up ceiling is max(2*currentReplicas, 4); scale-down has no rate
	// ceiling beyond minReplicas.
	if hpa.Spec.Behavior == nil {
		maximumAllowed := maxReplicas
		reason := "TooManyReplicas"
		scaleUpLimit := legacySimulatedScaleUpLimit(current)
		if maximumAllowed > scaleUpLimit {
			maximumAllowed = scaleUpLimit
			reason = "ScaleUpLimit"
		}
		switch {
		case desired < minReplicas:
			return minReplicas, true, "TooFewReplicas"
		case desired > maximumAllowed:
			return maximumAllowed, true, reason
		default:
			return desired, false, "DesiredWithinRange"
		}
	}

	if desired > current {
		scaleUpLimit := simulatedPolicyReplicaLimit(current, true, hpa.Spec.Behavior.ScaleUp)
		if scaleUpLimit < current {
			scaleUpLimit = current
		}
		maximumAllowed := maxReplicas
		reason := "TooManyReplicas"
		if maximumAllowed > scaleUpLimit {
			maximumAllowed = scaleUpLimit
			reason = "ScaleUpLimit"
		}
		if desired > maximumAllowed {
			return maximumAllowed, true, reason
		}
		return desired, false, "DesiredWithinRange"
	}

	if desired < current {
		scaleDownLimit := simulatedPolicyReplicaLimit(current, false, hpa.Spec.Behavior.ScaleDown)
		if scaleDownLimit > current {
			scaleDownLimit = current
		}
		minimumAllowed := minReplicas
		reason := "TooFewReplicas"
		if minimumAllowed < scaleDownLimit {
			minimumAllowed = scaleDownLimit
			reason = "ScaleDownLimit"
		}
		if desired < minimumAllowed {
			return minimumAllowed, true, reason
		}
		return desired, false, "DesiredWithinRange"
	}

	return desired, false, "DesiredWithinRange"
}

func legacySimulatedScaleUpLimit(current int32) int32 {
	limit := max(int64(current)*2, int64(4))
	if limit > int64(math.MaxInt32) {
		return math.MaxInt32
	}
	return int32(limit)
}

// simulatedPolicyReplicaLimit applies the immediate per-period policy ceiling
// using the current replica count as the period baseline. Nil policies are
// expanded to the autoscaling/v2 API defaults, matching API defaulting after a
// local behavior override introduces a previously absent behavior block.
func simulatedPolicyReplicaLimit(current int32, scaleUp bool, rules *autoscalingv2.HPAScalingRules) int32 {
	policy := autoscalingv2.MaxChangePolicySelect
	if rules != nil && rules.SelectPolicy != nil {
		policy = *rules.SelectPolicy
	}
	if policy == autoscalingv2.DisabledPolicySelect {
		return current
	}

	policies := defaultSimulatedScalingPolicies(scaleUp)
	if rules != nil && rules.Policies != nil {
		policies = rules.Policies
	}

	limit, ok := selectedPolicyReplicaLimit(current, scaleUp, policy, policies)
	if !ok {
		// An explicitly empty policy list is invalid in the Kubernetes API. Be
		// conservative if such an object is nevertheless supplied directly.
		return current
	}
	return limit
}

func defaultSimulatedScalingPolicies(scaleUp bool) []autoscalingv2.HPAScalingPolicy {
	return scaling.DefaultPolicies(scaleUp)
}

func selectedPolicyReplicaLimit(current int32, scaleUp bool, selectPolicy autoscalingv2.ScalingPolicySelect, policies []autoscalingv2.HPAScalingPolicy) (int32, bool) {
	return scaling.Limit(current, scaleUp, selectPolicy, policies, 0, nil)
}

func policyReplicaLimit(current int32, scaleUp bool, policy autoscalingv2.HPAScalingPolicy) (int32, bool) {
	return scaling.ReplicaLimit(current, scaleUp, policy)
}
