package cmd

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/simulate"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func TestSimulationPreservesAllHealthWeights(t *testing.T) {
	for _, condition := range []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: autoscalingv2.ScalingLimited, Status: corev1.ConditionTrue},
		{Type: autoscalingv2.ScalingActive, Status: corev1.ConditionFalse},
		{Type: autoscalingv2.AbleToScale, Status: corev1.ConditionFalse},
		{Type: autoscalingv2.AbleToScale, Status: corev1.ConditionTrue, Reason: hpaanalysis.ReasonScaleDownStabilized},
	} {
		for _, value := range []int{0, 3, 100} {
			t.Run(fmt.Sprintf("%s/%s/%d", condition.Type, condition.Reason, value), func(t *testing.T) {
				weights := hpaanalysis.HealthWeights{ScalingInactive: hpaanalysis.IntWeight(value), ScalingLimited: hpaanalysis.IntWeight(value), UnableToScale: hpaanalysis.IntWeight(value), ScaleDownStabilized: hpaanalysis.IntWeight(value), AtMinimumReplicas: hpaanalysis.IntWeight(value), ImplicitMaxReplicas: hpaanalysis.IntWeight(value), KEDAInactiveTrigger: hpaanalysis.IntWeight(value), VPAConflict: hpaanalysis.IntWeight(value), Churn: hpaanalysis.IntWeight(value)}
				hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(1, 1))
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{condition}
				expected := hpaanalysis.AnalyzeWithOptions(hpa, true, hpaanalysis.AnalysisOptions{HealthWeights: weights})
				converted := weightsForSimulate(weights)
				if converted.Overrides == nil || !reflect.DeepEqual(weights, *converted.Overrides) {
					t.Fatal("lost configured weight")
				}
				result, err := simulate.HPA(hpa, nil, converted)
				if err != nil {
					t.Fatal(err)
				}
				if result.Before.HealthScore != expected.Decision.HealthScore || result.After.HealthScore != expected.Decision.HealthScore {
					t.Fatalf("score mismatch: status=%d before=%d after=%d", expected.Decision.HealthScore, result.Before.HealthScore, result.After.HealthScore)
				}
				*weights.ScalingInactive = 99
				if *converted.Overrides.ScalingInactive != value {
					t.Fatal("simulation weights alias caller")
				}
			})
		}
	}
}
