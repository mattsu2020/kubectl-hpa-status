package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func toleranceTestHPA(behavior *autoscalingv2.HorizontalPodAutoscalerBehavior) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MaxReplicas:    10,
			Behavior:       behavior,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 4,
			DesiredReplicas: 4,
		},
	}
}

func directionalToleranceAssumption(t *testing.T, assumptions []Assumption, name string) Assumption {
	t.Helper()
	for _, assumption := range assumptions {
		if assumption.Name == name {
			return assumption
		}
	}
	t.Fatalf("assumption %q not found in %#v", name, assumptions)
	return Assumption{}
}

// TestAnalysisAssumptionsReportDirectionalTolerances locks in the fix for the
// mismatch between the analysis calculations (which honour per-direction
// spec.behavior tolerances) and the assumptions list (which used to always
// print the 0.1 controller default).
func TestAnalysisAssumptionsReportDirectionalTolerances(t *testing.T) {
	t.Parallel()
	scaleUpTolerance := resource.MustParse("0.05")
	scaleDownTolerance := resource.MustParse("0.2")
	src := toleranceTestHPA(&autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleUp: &autoscalingv2.HPAScalingRules{Tolerance: &scaleUpTolerance},
		ScaleDown: &autoscalingv2.HPAScalingRules{
			Tolerance: &scaleDownTolerance,
		},
	})

	a := FinalizeAnalysis(Analyze(src, false))

	up := directionalToleranceAssumption(t, a.Actions.Assumptions, assumptionToleranceScaleUp)
	if up.Value != "0.05" {
		t.Errorf("toleranceScaleUp value = %q, want 0.05", up.Value)
	}
	if up.Source != "hpa.spec" || up.Confidence != "high" {
		t.Errorf("toleranceScaleUp source/confidence = %s/%s, want hpa.spec/high", up.Source, up.Confidence)
	}

	down := directionalToleranceAssumption(t, a.Actions.Assumptions, assumptionToleranceScaleDown)
	if down.Value != "0.2" {
		t.Errorf("toleranceScaleDown value = %q, want 0.2", down.Value)
	}
	if down.Source != "hpa.spec" || down.Confidence != "high" {
		t.Errorf("toleranceScaleDown source/confidence = %s/%s, want hpa.spec/high", down.Source, down.Confidence)
	}
}

// TestAnalysisAssumptionsDefaultTolerancePerDirection verifies that a
// direction without an explicit tolerance reports the controller default with
// the assumed source, and only for that direction.
func TestAnalysisAssumptionsDefaultTolerancePerDirection(t *testing.T) {
	t.Parallel()
	scaleDownTolerance := resource.MustParse("0.3")
	src := toleranceTestHPA(&autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{Tolerance: &scaleDownTolerance},
	})

	a := FinalizeAnalysis(Analyze(src, false))

	up := directionalToleranceAssumption(t, a.Actions.Assumptions, assumptionToleranceScaleUp)
	if up.Value != "0.1" || up.Source != "assumed-controller-default" {
		t.Errorf("toleranceScaleUp = %s/%s, want 0.1/assumed-controller-default", up.Value, up.Source)
	}
	down := directionalToleranceAssumption(t, a.Actions.Assumptions, assumptionToleranceScaleDown)
	if down.Value != "0.3" || down.Source != "hpa.spec" {
		t.Errorf("toleranceScaleDown = %s/%s, want 0.3/hpa.spec", down.Value, down.Source)
	}
}

// TestAnalysisAssumptionsWithoutBehavior verifies the no-behavior HPA reports
// the controller default for both directions, and that re-finalization does
// not duplicate the entries.
func TestAnalysisAssumptionsWithoutBehavior(t *testing.T) {
	t.Parallel()
	src := toleranceTestHPA(nil)

	a := FinalizeAnalysis(FinalizeAnalysis(Analyze(src, false)))

	count := 0
	for _, assumption := range a.Actions.Assumptions {
		switch assumption.Name {
		case assumptionToleranceScaleUp, assumptionToleranceScaleDown:
			count++
			if assumption.Value != "0.1" || assumption.Source != "assumed-controller-default" || assumption.Confidence != "medium" {
				t.Errorf("%s = %s/%s/%s, want 0.1/assumed-controller-default/medium",
					assumption.Name, assumption.Value, assumption.Source, assumption.Confidence)
			}
		case "tolerance":
			t.Errorf("legacy combined tolerance assumption is still emitted: %#v", assumption)
		}
	}
	if count != 2 {
		t.Fatalf("directional tolerance assumptions = %d, want 2 (no duplicates)", count)
	}
}
