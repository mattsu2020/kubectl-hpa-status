package hpa

import (
	"errors"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestSimulateHPA_NilHPA(t *testing.T) {
	_, err := SimulateHPA(nil, map[string]string{"maxReplicas": "20"}, HealthWeights{})
	if err == nil {
		t.Error("expected error for nil HPA")
	}
}

func TestSimulateHPA_RaiseMaxReplicas(t *testing.T) {
	hpa := buildSimHPA(5, 5, 10) // current=5, desired=5, max=10 -> at max -> LIMITED

	result, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "20"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Parameter != "maxReplicas" {
		t.Errorf("expected parameter=maxReplicas, got %q", result.Parameter)
	}
	if result.OriginalValue != "10" {
		t.Errorf("expected originalValue=10, got %q", result.OriginalValue)
	}
	if result.SimulatedValue != "20" {
		t.Errorf("expected simulatedValue=20, got %q", result.SimulatedValue)
	}

	// The deep copy should not mutate the original
	if hpa.Spec.MaxReplicas != 10 {
		t.Errorf("original HPA was mutated: maxReplicas=%d", hpa.Spec.MaxReplicas)
	}
}

func TestSimulateHPA_LowerMinReplicas(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)
	hpa.Spec.MinReplicas = ptr.To(int32(3))

	result, err := SimulateHPA(hpa, map[string]string{"minReplicas": "1"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.OriginalValue != "3" {
		t.Errorf("expected originalValue=3, got %q", result.OriginalValue)
	}
	if result.SimulatedValue != "1" {
		t.Errorf("expected simulatedValue=1, got %q", result.SimulatedValue)
	}
}

func TestSimulateHPA_StabilizationWindow(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)
	window := int32(300)
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &window,
		},
	}

	_, err := SimulateHPA(hpa, map[string]string{"scaleDown.stabilizationWindowSeconds": "30"}, HealthWeights{})
	if !errors.Is(err, ErrUnsupportedSimulationSemantics) {
		t.Fatalf("error = %v, want ErrUnsupportedSimulationSemantics", err)
	}
}

func TestSimulateHPA_InvalidPath(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	_, err := SimulateHPA(hpa, map[string]string{"invalidField": "10"}, HealthWeights{})
	if err == nil {
		t.Error("expected error for invalid path")
	}
	if !strings.Contains(err.Error(), "unsupported path") {
		t.Errorf("expected unsupported path error, got: %v", err)
	}
}

func TestSimulateHPA_InvalidValue(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	_, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "abc"}, HealthWeights{})
	if err == nil {
		t.Error("expected error for non-numeric value")
	}
}

func TestSimulateHPA_MaxReplicasZero(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	_, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "0"}, HealthWeights{})
	if err == nil {
		t.Error("expected error for maxReplicas=0")
	}
}

func TestSimulateHPA_DeepCopyIsolation(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	_, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "20"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hpa.Spec.MaxReplicas != 10 {
		t.Errorf("original HPA maxReplicas mutated: got %d, want 10", hpa.Spec.MaxReplicas)
	}
}

func TestSimulateHPA_InterpretationGenerated(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)

	result, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "20"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Interpretation) == 0 {
		t.Error("expected interpretation lines to be generated")
	}

	found := false
	for _, line := range result.Interpretation {
		if strings.Contains(line, "desiredReplicas") {
			found = true
		}
	}
	if !found {
		t.Error("expected interpretation to mention desiredReplicas")
	}
}

func TestSimulateHPA_SelectPolicy(t *testing.T) {
	hpa := buildMetricSimHPA(10, 10, 30, 100)
	current := int32(50)
	hpa.Status.CurrentMetrics[0].Resource.Current.AverageUtilization = &current

	result, err := SimulateHPA(hpa, map[string]string{"scaleDown.selectPolicy": "Disabled"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.After.DesiredReplicas != hpa.Status.CurrentReplicas {
		t.Fatalf("Disabled scaleDown projected %d replicas, want current %d",
			result.After.DesiredReplicas, hpa.Status.CurrentReplicas)
	}
}

func TestSimulateHPA_InvalidSelectPolicy(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)
	_, err := SimulateHPA(hpa, map[string]string{"scaleDown.selectPolicy": "Fastest"}, HealthWeights{})
	if !errors.Is(err, ErrInvalidSimulationValue) {
		t.Fatalf("error = %v, want ErrInvalidSimulationValue", err)
	}
}

func TestSimulateHPA_ValidatesReplicaBounds(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
	}{
		{name: "max below existing min", overrides: map[string]string{"maxReplicas": "2"}},
		{name: "min above existing max", overrides: map[string]string{"minReplicas": "11"}},
		{name: "negative min", overrides: map[string]string{"minReplicas": "-1"}},
		{name: "negative stabilization", overrides: map[string]string{"scaleDown.stabilizationWindowSeconds": "-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpa := buildSimHPA(3, 3, 10)
			hpa.Spec.MinReplicas = ptr.To(int32(3))
			_, err := SimulateHPA(hpa, tt.overrides, HealthWeights{})
			if !errors.Is(err, ErrInvalidSimulationValue) {
				t.Fatalf("error = %v, want ErrInvalidSimulationValue", err)
			}
		})
	}
}

func TestSimulateHPA_RecomputesScalingLimitedCondition(t *testing.T) {
	hpa := buildMetricSimHPA(10, 10, 10, 100)
	current := int32(200)
	hpa.Status.CurrentMetrics[0].Resource.Current.AverageUtilization = &current
	hpa.Status.Conditions = append(hpa.Status.Conditions, autoscalingv2.HorizontalPodAutoscalerCondition{
		Type:   autoscalingv2.ScalingLimited,
		Status: corev1.ConditionTrue,
		Reason: "TooManyReplicas",
	})

	result, err := SimulateHPA(hpa, map[string]string{"maxReplicas": "20"}, HealthWeights{})
	if err != nil {
		t.Fatalf("SimulateHPA: %v", err)
	}
	if result.After.DesiredReplicas != 20 {
		t.Fatalf("After.DesiredReplicas = %d, want 20", result.After.DesiredReplicas)
	}
	if result.After.ScalingLimited {
		t.Fatal("After.ScalingLimited reused the stale live condition")
	}
	if result.After.Health == string(HealthLimited) {
		t.Fatalf("After.Health = %q; stale ScalingLimited penalty was retained", result.After.Health)
	}
}

func TestSimulateHPA_AppliesImmediateRatePolicies(t *testing.T) {
	hpa := buildMetricSimHPA(10, 10, 30, 100)
	current := int32(200)
	hpa.Status.CurrentMetrics[0].Resource.Current.AverageUtilization = &current
	selectMax := autoscalingv2.MaxChangePolicySelect
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleUp: &autoscalingv2.HPAScalingRules{
			SelectPolicy: &selectMax,
			Policies: []autoscalingv2.HPAScalingPolicy{
				{
					Type:          autoscalingv2.PodsScalingPolicy,
					Value:         3,
					PeriodSeconds: 60,
				},
				{
					Type:          autoscalingv2.PercentScalingPolicy,
					Value:         100,
					PeriodSeconds: 60,
				},
			},
		},
	}

	result, err := SimulateHPA(hpa, map[string]string{"scaleUp.selectPolicy": "Min"}, HealthWeights{})
	if err != nil {
		t.Fatalf("SimulateHPA: %v", err)
	}
	if result.After.DesiredReplicas != 13 {
		t.Fatalf("After.DesiredReplicas = %d, want policy-limited 13", result.After.DesiredReplicas)
	}
	if !result.After.ScalingLimited {
		t.Fatal("rate-limited projection must report ScalingLimited")
	}
}

func TestNormalizeSimulatedDesiredMatchesHPANormalization(t *testing.T) {
	t.Parallel()

	maxPolicy := autoscalingv2.MaxChangePolicySelect
	disabledPolicy := autoscalingv2.DisabledPolicySelect
	tests := []struct {
		name        string
		current     int32
		desired     int32
		min         int32
		max         int32
		behavior    *autoscalingv2.HorizontalPodAutoscalerBehavior
		want        int32
		wantLimited bool
		wantReason  string
	}{
		{
			name:        "nil behavior uses legacy scale-up ceiling",
			current:     3,
			desired:     20,
			min:         1,
			max:         30,
			want:        6,
			wantLimited: true,
			wantReason:  "ScaleUpLimit",
		},
		{
			name:    "non-nil behavior expands default scale-up policies",
			current: 1,
			desired: 20,
			min:     1,
			max:     30,
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{SelectPolicy: &maxPolicy},
			},
			want:        5,
			wantLimited: true,
			wantReason:  "ScaleUpLimit",
		},
		{
			name:    "target above max scales down at policy rate",
			current: 20,
			desired: 15,
			min:     1,
			max:     10,
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					SelectPolicy: &maxPolicy,
					Policies: []autoscalingv2.HPAScalingPolicy{{
						Type:          autoscalingv2.PodsScalingPolicy,
						Value:         2,
						PeriodSeconds: 60,
					}},
				},
			},
			want:        18,
			wantLimited: true,
			wantReason:  "ScaleDownLimit",
		},
		{
			name:    "max replicas is stricter than rate policy",
			current: 10,
			desired: 30,
			min:     1,
			max:     15,
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{
					SelectPolicy: &maxPolicy,
					Policies: []autoscalingv2.HPAScalingPolicy{{
						Type:          autoscalingv2.PodsScalingPolicy,
						Value:         20,
						PeriodSeconds: 60,
					}},
				},
			},
			want:        15,
			wantLimited: true,
			wantReason:  "TooManyReplicas",
		},
		{
			name:    "disabled policy holds current replicas",
			current: 10,
			desired: 30,
			min:     1,
			max:     30,
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{SelectPolicy: &disabledPolicy},
			},
			want:        10,
			wantLimited: true,
			wantReason:  "ScaleUpLimit",
		},
		{
			name:    "default scale-down policy permits zero",
			current: 10,
			desired: 0,
			min:     0,
			max:     30,
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{SelectPolicy: &maxPolicy},
			},
			want:        0,
			wantLimited: false,
			wantReason:  "DesiredWithinRange",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hpa := buildSimHPA(tt.current, tt.current, tt.max)
			hpa.Spec.Behavior = tt.behavior
			got, limited, reason := normalizeSimulatedDesired(hpa, tt.desired, tt.min, tt.max)
			if got != tt.want || limited != tt.wantLimited || reason != tt.wantReason {
				t.Fatalf(
					"normalizeSimulatedDesired() = (%d, %v, %q), want (%d, %v, %q)",
					got, limited, reason, tt.want, tt.wantLimited, tt.wantReason,
				)
			}
		})
	}
}

func TestPolicyReplicaLimitPercentScaleDownRoundsLikeController(t *testing.T) {
	t.Parallel()

	limit, ok := policyReplicaLimit(5, false, autoscalingv2.HPAScalingPolicy{
		Type:          autoscalingv2.PercentScalingPolicy,
		Value:         20,
		PeriodSeconds: 60,
	})
	if !ok || limit != 4 {
		t.Fatalf("policyReplicaLimit() = (%d, %v), want (4, true)", limit, ok)
	}
}

func TestMetricSimulationProjectedReplicasIncludesRatePolicy(t *testing.T) {
	t.Parallel()

	hpa := buildMetricSimHPA(10, 10, 30, 100)
	current := int32(100)
	hpa.Status.CurrentMetrics[0].Resource.Current.AverageUtilization = &current
	maxPolicy := autoscalingv2.MaxChangePolicySelect
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleUp: &autoscalingv2.HPAScalingRules{
			SelectPolicy: &maxPolicy,
			Policies: []autoscalingv2.HPAScalingPolicy{{
				Type:          autoscalingv2.PodsScalingPolicy,
				Value:         3,
				PeriodSeconds: 60,
			}},
		},
	}

	result, err := SimulateMetricChange(hpa, map[string]string{"cpu": "200%"}, HealthWeights{})
	if err != nil {
		t.Fatalf("SimulateMetricChange: %v", err)
	}
	if result.After.DesiredReplicas != 13 {
		t.Fatalf("After.DesiredReplicas = %d, want rate-limited 13", result.After.DesiredReplicas)
	}
	if len(result.MetricSimulations) != 1 || result.MetricSimulations[0].ProjectedReplicas != 13 {
		t.Fatalf("MetricSimulations = %+v, want projectedReplicas=13", result.MetricSimulations)
	}
}

func TestReplaceSimulatedControllerConditionsDropsProjectionStaleConditions(t *testing.T) {
	t.Parallel()

	hpa := buildSimHPA(3, 3, 10)
	hpa.Status.Conditions = append(hpa.Status.Conditions,
		autoscalingv2.HorizontalPodAutoscalerCondition{
			Type:   autoscalingv2.AbleToScale,
			Status: corev1.ConditionTrue,
			Reason: "ScaleUpStabilized",
		},
		autoscalingv2.HorizontalPodAutoscalerCondition{
			Type:   autoscalingv2.ScaledToZero,
			Status: corev1.ConditionTrue,
			Reason: "ScaledToZero",
		},
	)

	replaceSimulatedControllerConditions(hpa, false, "DesiredWithinRange")

	for _, condition := range hpa.Status.Conditions {
		if condition.Type == autoscalingv2.ScaledToZero {
			t.Fatal("projected conditions retained the live ScaledToZero observation")
		}
		if condition.Type == autoscalingv2.AbleToScale && condition.Reason == "ScaleUpStabilized" {
			t.Fatal("projected conditions retained a recommendation-history observation")
		}
	}
}

func TestSimulateHPA_CurrentZeroWithoutScaledToZeroRemainsDisabled(t *testing.T) {
	t.Parallel()

	hpa := buildMetricSimHPA(0, 0, 10, 100)
	modified, err := BuildSimulatedHPA(hpa, map[string]string{"maxReplicas": "20"}, nil)
	if err != nil {
		t.Fatalf("BuildSimulatedHPA: %v", err)
	}
	if modified.Status.DesiredReplicas != 0 {
		t.Fatalf("desiredReplicas = %d, want 0", modified.Status.DesiredReplicas)
	}

	var scalingActive *autoscalingv2.HorizontalPodAutoscalerCondition
	for i := range modified.Status.Conditions {
		if modified.Status.Conditions[i].Type == autoscalingv2.ScalingActive {
			scalingActive = &modified.Status.Conditions[i]
			break
		}
	}
	if scalingActive == nil ||
		scalingActive.Status != corev1.ConditionFalse ||
		scalingActive.Reason != "ScalingDisabled" {
		t.Fatalf("ScalingActive = %+v, want False/ScalingDisabled", scalingActive)
	}
}

func TestSimulateHPA_ExternalValueScalesFromZero(t *testing.T) {
	t.Parallel()

	hpa := buildExternalScaleFromZeroSimHPA(autoscalingv2.ValueMetricType)
	modified, err := BuildSimulatedHPA(hpa, map[string]string{"maxReplicas": "20"}, nil)
	if err != nil {
		t.Fatalf("BuildSimulatedHPA: %v", err)
	}
	if modified.Status.DesiredReplicas != 3 {
		t.Fatalf("desiredReplicas = %d, want ceil(250/100) = 3", modified.Status.DesiredReplicas)
	}

	var scalingActive *autoscalingv2.HorizontalPodAutoscalerCondition
	for i := range modified.Status.Conditions {
		condition := &modified.Status.Conditions[i]
		if condition.Type == autoscalingv2.ScaledToZero {
			t.Fatal("projected conditions retained the live ScaledToZero observation")
		}
		if condition.Type == autoscalingv2.ScalingActive {
			scalingActive = condition
		}
	}
	if scalingActive == nil ||
		scalingActive.Status != corev1.ConditionTrue ||
		scalingActive.Reason != "ValidMetricFound" {
		t.Fatalf("ScalingActive = %+v, want True/ValidMetricFound", scalingActive)
	}
}

func TestSimulateHPA_RejectsUnprojectableScaleFromZeroMetric(t *testing.T) {
	t.Parallel()

	hpa := buildExternalScaleFromZeroSimHPA(autoscalingv2.AverageValueMetricType)
	_, err := BuildSimulatedHPA(hpa, map[string]string{"maxReplicas": "20"}, nil)
	if !errors.Is(err, ErrUnsupportedSimulationSemantics) {
		t.Fatalf("error = %v, want ErrUnsupportedSimulationSemantics", err)
	}
}

func TestValidateSimulatedScalingRulesUpperBounds(t *testing.T) {
	t.Parallel()

	tooLongWindow := int32(3601)
	tests := []struct {
		name  string
		rules *autoscalingv2.HPAScalingRules
	}{
		{
			name: "stabilization window above API maximum",
			rules: &autoscalingv2.HPAScalingRules{
				StabilizationWindowSeconds: &tooLongWindow,
			},
		},
		{
			name: "policy period above API maximum",
			rules: &autoscalingv2.HPAScalingRules{
				Policies: []autoscalingv2.HPAScalingPolicy{{
					Type:          autoscalingv2.PodsScalingPolicy,
					Value:         1,
					PeriodSeconds: 1801,
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := validateSimulatedScalingRules("scaleUp", tt.rules); !errors.Is(err, ErrInvalidSimulationValue) {
				t.Fatalf("error = %v, want ErrInvalidSimulationValue", err)
			}
		})
	}
}

func TestSimulateHPA_RejectsRatePolicyOverrideWithoutEventHistory(t *testing.T) {
	hpa := buildSimHPA(3, 3, 10)
	_, err := SimulateHPA(hpa, map[string]string{"scaleUp.policies[0].value": "4"}, HealthWeights{})
	if !errors.Is(err, ErrUnsupportedSimulationSemantics) {
		t.Fatalf("error = %v, want ErrUnsupportedSimulationSemantics", err)
	}
}

func TestSimulateScenarioTargetToleranceAndDuration(t *testing.T) {
	hpa := buildMetricSimHPA(10, 10, 30, 100)
	// Current 108% is inside the default 10% band, but outside the simulated
	// 5% scale-up tolerance. Lowering the target to 90 further raises the ratio.
	current := int32(108)
	hpa.Status.CurrentMetrics[0].Resource.Current.AverageUtilization = &current

	result, err := SimulateScenario(hpa,
		map[string]string{"metric.cpu.target": "90", "tolerance": "0.05"},
		nil, HealthWeights{}, SimulationExtendedOptions{DurationSeconds: 120, StepSeconds: 30})
	if err != nil {
		t.Fatalf("SimulateScenario: %v", err)
	}
	if result.After.DesiredReplicas != 12 {
		t.Fatalf("after desired replicas = %d, want ceil(10 * 108/90) = 12", result.After.DesiredReplicas)
	}
	if len(result.TimeSeriesProjection) == 0 || result.TimeSeriesProjection[len(result.TimeSeriesProjection)-1].TimeOffset != 120 {
		t.Fatalf("duration projection not applied: %+v", result.TimeSeriesProjection)
	}
	modified, err := BuildSimulatedHPA(hpa,
		map[string]string{"metric.cpu.target": "90", "tolerance": "0.05"}, nil)
	if err != nil {
		t.Fatalf("BuildSimulatedHPA: %v", err)
	}
	if got := *modified.Spec.Metrics[0].Resource.Target.AverageUtilization; got != 90 {
		t.Fatalf("simulated target = %d, want 90", got)
	}
	if modified.Spec.Behavior == nil || modified.Spec.Behavior.ScaleUp.Tolerance == nil || modified.Spec.Behavior.ScaleDown.Tolerance == nil {
		t.Fatalf("simulated directional tolerances missing: %+v", modified.Spec.Behavior)
	}
}

func TestSimulateScenarioToleranceOnlyChangesProjection(t *testing.T) {
	hpa := buildMetricSimHPA(10, 10, 30, 100)
	current := int32(108)
	hpa.Status.CurrentMetrics[0].Resource.Current.AverageUtilization = &current

	defaultResult, err := SimulateScenario(hpa, nil, map[string]string{"cpu": "108%"}, HealthWeights{}, SimulationExtendedOptions{})
	if err != nil {
		t.Fatalf("default simulation: %v", err)
	}
	if defaultResult.After.DesiredReplicas != 10 {
		t.Fatalf("default tolerance should hold at 10, got %d", defaultResult.After.DesiredReplicas)
	}
	tightResult, err := SimulateScenario(hpa, map[string]string{"tolerance": "0.05"}, map[string]string{"cpu": "108%"}, HealthWeights{}, SimulationExtendedOptions{})
	if err != nil {
		t.Fatalf("tight tolerance simulation: %v", err)
	}
	if tightResult.After.DesiredReplicas != 11 {
		t.Fatalf("0.05 tolerance should project 11, got %d", tightResult.After.DesiredReplicas)
	}
}

func TestRecomputeSimulatedDesiredRequiresOneToOneCanonicalMetricsForDownscale(t *testing.T) {
	t.Parallel()

	target := resource.MustParse("100")
	current := resource.MustParse("50")
	selector := func(queue string) *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{"queue": queue}}
	}
	spec := func(queue string) autoscalingv2.MetricSpec {
		return autoscalingv2.MetricSpec{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "queue_depth",
					Selector: selector(queue),
				},
				Target: autoscalingv2.MetricTarget{
					Type:  autoscalingv2.ValueMetricType,
					Value: &target,
				},
			},
		}
	}
	status := func(queue string) autoscalingv2.MetricStatus {
		return autoscalingv2.MetricStatus{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "queue_depth",
					Selector: selector(queue),
				},
				Current: autoscalingv2.MetricValueStatus{Value: &current},
			},
		}
	}

	base := buildSimHPA(10, 10, 20)
	base.Spec.Metrics = []autoscalingv2.MetricSpec{spec("critical"), spec("bulk")}
	base.Status.CurrentMetrics = []autoscalingv2.MetricStatus{status("critical"), status("bulk")}
	if !hasOneToOneCanonicalMetricStatus(base) {
		t.Fatal("selector-distinct complete metric set must be recognized as one-to-one")
	}
	recomputeSimulatedDesired(base)
	if base.Status.DesiredReplicas != 5 {
		t.Fatalf("complete metric set projected desiredReplicas = %d, want 5", base.Status.DesiredReplicas)
	}

	duplicate := base.DeepCopy()
	duplicate.Status.DesiredReplicas = 10
	duplicate.Status.CurrentMetrics = []autoscalingv2.MetricStatus{status("critical"), status("critical")}
	if hasOneToOneCanonicalMetricStatus(duplicate) {
		t.Fatal("duplicate status identity must not satisfy a selector-distinct spec metric")
	}
	recomputeSimulatedDesired(duplicate)
	if duplicate.Status.DesiredReplicas != duplicate.Status.CurrentReplicas {
		t.Fatalf(
			"duplicate/missing identity projected downscale to %d, want conservative hold at %d",
			duplicate.Status.DesiredReplicas,
			duplicate.Status.CurrentReplicas,
		)
	}

	malformed := base.DeepCopy()
	malformed.Status.CurrentMetrics[1].External.Metric.Selector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key:      "queue",
			Operator: metav1.LabelSelectorOperator("Bogus"),
		}},
	}
	if hasOneToOneCanonicalMetricStatus(malformed) {
		t.Fatal("malformed current metric selector must fail the canonical completeness check")
	}
	recomputeSimulatedDesired(malformed)
	if malformed.Status.DesiredReplicas != malformed.Status.CurrentReplicas {
		t.Fatalf(
			"malformed identity projected downscale to %d, want conservative hold at %d",
			malformed.Status.DesiredReplicas,
			malformed.Status.CurrentReplicas,
		)
	}

	wrongShape := base.DeepCopy()
	wrongShape.Status.DesiredReplicas = 5
	wrongShape.Status.CurrentMetrics[0].External.Current.Value = nil
	wrongShape.Status.CurrentMetrics[0].External.Current.AverageValue = &current
	if hasOneToOneCanonicalMetricStatus(wrongShape) {
		t.Fatal("status value for a different target type must fail the completeness check")
	}
	recomputeSimulatedDesired(wrongShape)
	if wrongShape.Status.DesiredReplicas != wrongShape.Status.CurrentReplicas {
		t.Fatalf(
			"wrong target shape projected downscale to %d, want conservative hold at %d",
			wrongShape.Status.DesiredReplicas,
			wrongShape.Status.CurrentReplicas,
		)
	}
}

func TestRecomputeSimulatedDesiredBlocksDownscaleWithoutMetricEvidence(t *testing.T) {
	hpa := buildSimHPA(10, 5, 20)
	target := int32(50)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: corev1.ResourceCPU,
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: &target,
			},
		},
	}}
	hpa.Status.CurrentMetrics = nil

	recomputeSimulatedDesired(hpa)

	if hpa.Status.DesiredReplicas != hpa.Status.CurrentReplicas {
		t.Fatalf(
			"missing metric evidence projected downscale to %d, want conservative hold at %d",
			hpa.Status.DesiredReplicas,
			hpa.Status.CurrentReplicas,
		)
	}
}

func buildSimHPA(current, desired, maxReplicas int32) *autoscalingv2.HorizontalPodAutoscaler {
	return testutil.BuildHPA("default", "test-hpa",
		testutil.WithMinMax(1, maxReplicas),
		testutil.WithReplicas(current, desired),
		testutil.WithScaleTargetRef("Deployment", "test-deploy"),
		testutil.WithConditions(
			autoscalingv2.HorizontalPodAutoscalerCondition{
				Type: autoscalingv2.ScalingActive, Status: corev1.ConditionTrue, Reason: "ValidMetricFound",
			},
		),
	)
}

func buildExternalScaleFromZeroSimHPA(targetType autoscalingv2.MetricTargetType) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := buildSimHPA(0, 0, 10)
	minReplicas := int32(0)
	hpa.Spec.MinReplicas = &minReplicas
	target := resource.MustParse("100")
	current := resource.MustParse("250")
	metricTarget := autoscalingv2.MetricTarget{Type: targetType}
	currentValue := autoscalingv2.MetricValueStatus{}
	if targetType == autoscalingv2.ValueMetricType {
		metricTarget.Value = &target
		currentValue.Value = &current
	} else {
		metricTarget.AverageValue = &target
		currentValue.AverageValue = &current
	}
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricSource{
			Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
			Target: metricTarget,
		},
	}}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricStatus{
			Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth"},
			Current: currentValue,
		},
	}}
	hpa.Status.Conditions = append(hpa.Status.Conditions,
		autoscalingv2.HorizontalPodAutoscalerCondition{
			Type:   autoscalingv2.ScaledToZero,
			Status: corev1.ConditionTrue,
			Reason: "ScaledToZero",
		},
	)
	return hpa
}
