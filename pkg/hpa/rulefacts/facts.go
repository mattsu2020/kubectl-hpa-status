// Package rulefacts extracts neutral HPA observations shared by audit, lint,
// and policy profiles. Profiles retain their own thresholds and severities.
package rulefacts

import autoscalingv2 "k8s.io/api/autoscaling/v2"

// ResourceUtilizationTarget is a normalized resource utilization target.
type ResourceUtilizationTarget struct {
	MetricIndex int
	Resource    string
	Percent     int32
}

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

// ResourceUtilizationTargets extracts all percentage-based resource targets.
func ResourceUtilizationTargets(hpa *autoscalingv2.HorizontalPodAutoscaler) []ResourceUtilizationTarget {
	if hpa == nil {
		return nil
	}
	var targets []ResourceUtilizationTarget
	for index, metric := range hpa.Spec.Metrics {
		switch metric.Type {
		case autoscalingv2.ResourceMetricSourceType:
			if metric.Resource == nil ||
				metric.Resource.Target.Type != autoscalingv2.UtilizationMetricType || metric.Resource.Target.AverageUtilization == nil {
				continue
			}
			targets = append(targets, ResourceUtilizationTarget{
				MetricIndex: index,
				Resource:    string(metric.Resource.Name),
				Percent:     *metric.Resource.Target.AverageUtilization,
			})
		case autoscalingv2.ContainerResourceMetricSourceType:
			if metric.ContainerResource == nil ||
				metric.ContainerResource.Target.Type != autoscalingv2.UtilizationMetricType || metric.ContainerResource.Target.AverageUtilization == nil {
				continue
			}
			targets = append(targets, ResourceUtilizationTarget{
				MetricIndex: index,
				Resource:    string(metric.ContainerResource.Name),
				Percent:     *metric.ContainerResource.Target.AverageUtilization,
			})
		}
	}
	return targets
}
