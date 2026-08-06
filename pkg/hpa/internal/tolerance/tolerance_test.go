package tolerance

import (
	"math"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func hpaWithTolerance(scaleUp, scaleDown *resource.Quantity) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: 4},
		Spec:   autoscalingv2.HorizontalPodAutoscalerSpec{MinReplicas: ptr.To(int32(1)), MaxReplicas: 10},
	}
	if scaleUp != nil || scaleDown != nil {
		hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{}
		if scaleUp != nil {
			hpa.Spec.Behavior.ScaleUp = &autoscalingv2.HPAScalingRules{Tolerance: scaleUp}
		}
		if scaleDown != nil {
			hpa.Spec.Behavior.ScaleDown = &autoscalingv2.HPAScalingRules{Tolerance: scaleDown}
		}
	}
	return hpa
}

func TestDirectionalTolerance_Defaults(t *testing.T) {
	hpa := hpaWithTolerance(nil, nil)

	value, configured := DirectionalTolerance(hpa, 2)
	if configured || value != DefaultTolerance {
		t.Errorf("scale-up default: value=%v configured=%v, want %v/false", value, configured, DefaultTolerance)
	}

	value, configured = DirectionalTolerance(hpa, 0)
	if configured || value != DefaultTolerance {
		t.Errorf("scale-down default: value=%v configured=%v, want %v/false", value, configured, DefaultTolerance)
	}

	// ratio == 1 selects neither direction's rules.
	value, configured = DirectionalTolerance(hpa, 1)
	if configured || value != DefaultTolerance {
		t.Errorf("ratio=1 default: value=%v configured=%v, want %v/false", value, configured, DefaultTolerance)
	}
}

func TestDirectionalTolerance_NilHPA(t *testing.T) {
	value, configured := DirectionalTolerance(nil, 2)
	if configured || value != DefaultTolerance {
		t.Errorf("nil HPA: value=%v configured=%v, want %v/false", value, configured, DefaultTolerance)
	}
}

func TestDirectionalTolerance_Configured(t *testing.T) {
	up := resource.MustParse("0.2")
	down := resource.MustParse("0.05")
	hpa := hpaWithTolerance(&up, &down)

	value, configured := DirectionalTolerance(hpa, 2)
	if !configured || value != 0.2 {
		t.Errorf("scale-up configured: value=%v configured=%v, want 0.2/true", value, configured)
	}

	value, configured = DirectionalTolerance(hpa, 0.5)
	if !configured || value != 0.05 {
		t.Errorf("scale-down configured: value=%v configured=%v, want 0.05/true", value, configured)
	}
}

func TestDirectionalTolerance_InvalidConfiguredFallsBackToDefault(t *testing.T) {
	negative := resource.MustParse("-1")
	hpa := hpaWithTolerance(&negative, nil)

	value, configured := DirectionalTolerance(hpa, 2)
	if configured || value != DefaultTolerance {
		t.Errorf("negative tolerance should fall back to default: value=%v configured=%v", value, configured)
	}
}

func TestConfiguredDirectionalTolerances(t *testing.T) {
	up := resource.MustParse("0.25")
	hpa := hpaWithTolerance(&up, nil)

	scaleUp, scaleDown := ConfiguredDirectionalTolerances(hpa)
	if scaleUp == nil || *scaleUp != 0.25 {
		t.Errorf("scaleUp = %v, want 0.25", scaleUp)
	}
	if scaleDown != nil {
		t.Errorf("scaleDown = %v, want nil", scaleDown)
	}

	if up2, down2 := ConfiguredDirectionalTolerances(nil); up2 != nil || down2 != nil {
		t.Errorf("nil HPA should return nil, nil; got %v, %v", up2, down2)
	}
}

func TestEffectiveDirectionalTolerances(t *testing.T) {
	up := resource.MustParse("0.15")
	hpa := hpaWithTolerance(&up, nil)

	scaleUp, scaleDown := EffectiveDirectionalTolerances(hpa)
	if scaleUp != 0.15 {
		t.Errorf("scaleUp = %v, want 0.15", scaleUp)
	}
	if scaleDown != DefaultTolerance {
		t.Errorf("scaleDown = %v, want default %v", scaleDown, DefaultTolerance)
	}
}

func TestRatioWithinTolerance(t *testing.T) {
	hpa := hpaWithTolerance(nil, nil)

	within, tolerance := RatioWithinTolerance(hpa, 1.05)
	if !within || tolerance != DefaultTolerance {
		t.Errorf("ratio 1.05 within default tolerance: within=%v tolerance=%v", within, tolerance)
	}

	within, _ = RatioWithinTolerance(hpa, 1.5)
	if within {
		t.Error("ratio 1.5 should be outside default tolerance")
	}
}

func TestEstimatedDesiredForRatio(t *testing.T) {
	hpa := hpaWithTolerance(nil, nil)

	// Within tolerance: stays at current.
	if got := EstimatedDesiredForRatio(hpa, 1.02); got != hpa.Status.CurrentReplicas {
		t.Errorf("within-tolerance ratio: got %d, want current replicas %d", got, hpa.Status.CurrentReplicas)
	}

	// Outside tolerance: scales proportionally.
	if got := EstimatedDesiredForRatio(hpa, 2.0); got != 8 {
		t.Errorf("ratio=2.0 with current=4: got %d, want 8", got)
	}
	if got := EstimatedDesiredForRatio(hpa, math.NaN()); got != hpa.Status.CurrentReplicas {
		t.Errorf("NaN ratio: got %d, want current replicas %d", got, hpa.Status.CurrentReplicas)
	}
	if got := EstimatedDesiredForRatio(hpa, -1); got != hpa.Status.CurrentReplicas {
		t.Errorf("negative ratio: got %d, want current replicas %d", got, hpa.Status.CurrentReplicas)
	}
	if got := EstimatedDesiredForRatio(hpa, math.Inf(1)); got != math.MaxInt32 {
		t.Errorf("infinite ratio: got %d, want MaxInt32", got)
	}
}

func TestProjectedReplicasForRatioNumericSafety(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		current    int32
		ratio      float64
		want       int32
		wantUsable bool
	}{
		{name: "ordinary", current: 4, ratio: 1.25, want: 5, wantUsable: true},
		{name: "NaN preserves current", current: 4, ratio: math.NaN(), want: 4, wantUsable: false},
		{name: "negative preserves current", current: 4, ratio: -1, want: 4, wantUsable: false},
		{name: "positive infinity saturates", current: 4, ratio: math.Inf(1), want: math.MaxInt32, wantUsable: true},
		{name: "finite overflow saturates", current: math.MaxInt32, ratio: 2, want: math.MaxInt32, wantUsable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, usable := ProjectedReplicasForRatio(tt.current, tt.ratio)
			if got != tt.want || usable != tt.wantUsable {
				t.Fatalf("ProjectedReplicasForRatio(%d, %v) = (%d, %v), want (%d, %v)",
					tt.current, tt.ratio, got, usable, tt.want, tt.wantUsable)
			}
		})
	}
}
