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
