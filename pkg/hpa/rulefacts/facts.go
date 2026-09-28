// Package rulefacts extracts neutral HPA observations shared by audit, lint,
// and policy profiles. Profiles retain their own thresholds and severities.
package rulefacts

import autoscalingv2 "k8s.io/api/autoscaling/v2"

// ResourceUtilizationTarget is a normalized resource utilization target.
type ResourceUtilizationTarget struct {
	Resource string
	Percent  int32
}

// ResourceUtilizationTargets extracts all percentage-based resource targets.
// MaxRecommendedReplicaRatio is the maxReplicas/minReplicas ratio beyond which
// both the audit and lint replica-range rules flag a wide range: beyond ~10x a
// single scale event can grow the workload by an order of magnitude.
const MaxRecommendedReplicaRatio = 10

// ReplicaRangeRatio returns maxReplicas divided by minReplicas. It reports
// false when minReplicas is zero so callers do not need to repeat the guard.
func ReplicaRangeRatio(minReplicas, maxReplicas int32) (ratio int32, ok bool) {
	if minReplicas <= 0 {
		return 0, false
	}
	return maxReplicas / minReplicas, true
}

func ResourceUtilizationTargets(hpa *autoscalingv2.HorizontalPodAutoscaler) []ResourceUtilizationTarget {
	if hpa == nil {
		return nil
	}
	var targets []ResourceUtilizationTarget
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type != autoscalingv2.ResourceMetricSourceType || metric.Resource == nil ||
			metric.Resource.Target.Type != autoscalingv2.UtilizationMetricType || metric.Resource.Target.AverageUtilization == nil {
			continue
		}
		targets = append(targets, ResourceUtilizationTarget{
			Resource: string(metric.Resource.Name),
			Percent:  *metric.Resource.Target.AverageUtilization,
		})
	}
	return targets
}
