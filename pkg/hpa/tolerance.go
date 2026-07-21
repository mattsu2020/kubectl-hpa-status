package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// This file re-exports the tolerance helpers from pkg/hpa/internal/util so
// existing call sites in pkg/hpa keep working unqualified. Sub-packages that
// need these helpers (e.g. retrospective's replay analysis) import
// pkg/hpa/internal/util directly.

const defaultTolerance = util.DefaultTolerance

func directionalTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) (float64, bool) {
	return util.DirectionalTolerance(hpa, ratio)
}

func configuredDirectionalTolerances(hpa *autoscalingv2.HorizontalPodAutoscaler) (scaleUp, scaleDown *float64) {
	return util.ConfiguredDirectionalTolerances(hpa)
}

func effectiveDirectionalTolerances(hpa *autoscalingv2.HorizontalPodAutoscaler) (scaleUp, scaleDown float64) {
	return util.EffectiveDirectionalTolerances(hpa)
}

func ratioWithinTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) (bool, float64) {
	return util.RatioWithinTolerance(hpa, ratio)
}

func estimatedDesiredForRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) int32 {
	return util.EstimatedDesiredForRatio(hpa, ratio)
}
