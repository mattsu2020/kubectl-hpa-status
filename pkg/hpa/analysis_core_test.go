package hpa

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAnalyzeDoesNotTreatInactiveDesiredZeroAsScaleDown(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 3
	hpa.Status.DesiredReplicas = 0
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionFalse, Reason: "FailedGetResourceMetric", Message: "missing cpu metrics"},
	}

	got := Analyze(hpa, true)
	if got.Summary != "HPA cannot currently compute a scaling recommendation from metrics." {
		t.Fatalf("unexpected summary: %s", got.Summary)
	}
	if !containsLine(got.Interpretation, "avoids treating desiredReplicas=0 as a scale-down") {
		t.Fatalf("expected inactive metric interpretation, got %#v", got.Interpretation)
	}
}

func TestAnalyzeNilHPADoesNotPanic(t *testing.T) {
	got := Analyze(nil, true)
	if got.Health != "ERROR" {
		t.Fatalf("expected ERROR health, got %s", got.Health)
	}
	if got.HealthScore != 0 {
		t.Fatalf("expected zero health score, got %d", got.HealthScore)
	}
	if !containsLine(got.Interpretation, "HPA input was nil") {
		t.Fatalf("expected nil input interpretation, got %#v", got.Interpretation)
	}
}

func TestAnalyzeDetectsToleranceLikeNoScale(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 7
	hpa.Status.DesiredReplicas = 7
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceMemory, 70)}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceMemory, 73)}

	got := Analyze(hpa, true)
	if got.ImpactMetric == nil || got.ImpactMetric.Name != "memory" {
		t.Fatalf("expected memory impact estimate, got %#v", got.ImpactMetric)
	}
	if !containsLine(got.Interpretation, "within the Kubernetes default scaleUp tolerance") {
		t.Fatalf("expected directional tolerance interpretation, got %#v", got.Interpretation)
	}
}

func TestMostInfluentialMetricChoosesLargestEstimatedDesired(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 10
	hpa.Status.DesiredReplicas = 10
	hpa.Spec.MaxReplicas = 20
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		resourceMetricSpec(corev1.ResourceCPU, 80),
		resourceMetricSpec(corev1.ResourceMemory, 50),
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		resourceMetricStatus(corev1.ResourceCPU, 88),
		resourceMetricStatus(corev1.ResourceMemory, 68),
	}

	got, ok := MostInfluentialMetric(hpa)
	if !ok {
		t.Fatal("expected an impact estimate")
	}
	if got.Name != "memory" {
		t.Fatalf("expected memory to have largest distance, got %s", got.Name)
	}
}

func TestAnalyzeMultiMetricMaxReplicasExplainsLimitAndImpactEstimate(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 5
	hpa.Status.DesiredReplicas = 5
	hpa.Spec.MaxReplicas = 5
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		resourceMetricSpec(corev1.ResourceCPU, 80),
		resourceMetricSpec(corev1.ResourceMemory, 50),
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		resourceMetricStatus(corev1.ResourceCPU, 70),
		resourceMetricStatus(corev1.ResourceMemory, 68),
	}
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
	}

	got := Analyze(hpa, true)
	if got.Summary != "HPA is at maxReplicas." {
		t.Fatalf("unexpected summary: %s", got.Summary)
	}
	if got.ImpactMetric == nil || got.ImpactMetric.Name != "memory" {
		t.Fatalf("expected memory impact estimate, got %#v", got.ImpactMetric)
	}
	if !containsLine(got.Interpretation, "constrained by maxReplicas") {
		t.Fatalf("expected maxReplicas interpretation, got %#v", got.Interpretation)
	}
	if !containsLine(got.Interpretation, "winning metric cannot be reliably determined") {
		t.Fatalf("expected maxReplicas winner-detection warning, got %#v", got.Interpretation)
	}
}

func TestAnalyzeScaleDownStabilized(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 8
	hpa.Status.DesiredReplicas = 8
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
	}

	got := Analyze(hpa, true)
	if !containsLine(got.Interpretation, "Scale down appears stabilized") {
		t.Fatalf("expected stabilization interpretation, got %#v", got.Interpretation)
	}
}

func TestAnalyzeFormatsNonResourceMetrics(t *testing.T) {
	hpa := baseHPA()
	target := resource.MustParse("10")
	current := resource.MustParse("12")
	averageTarget := resource.MustParse("100m")
	averageCurrent := resource.MustParse("120m")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
		{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "requests_per_second"},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: &averageTarget},
			},
		},
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth"},
				Current: autoscalingv2.MetricValueStatus{Value: &current},
			},
		},
		{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "requests_per_second"},
				Current: autoscalingv2.MetricValueStatus{AverageValue: &averageCurrent},
			},
		},
	}

	got := Analyze(hpa, false)
	if len(got.Metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %#v", got.Metrics)
	}
	if got.Metrics[0].Text != "External queue_depth current=12 target=10 ratio=1.200 note=\"current value is above target\"" {
		t.Fatalf("unexpected external metric text: %s", got.Metrics[0].Text)
	}
	if got.Metrics[1].Text != "Pods requests_per_second current=120m target=100m ratio=1.200 note=\"current value is above target\"" {
		t.Fatalf("unexpected pods metric text: %s", got.Metrics[1].Text)
	}
}

func TestAnalyzeBehaviorAddsRecommendedScaleDownAction(t *testing.T) {
	window := int32(300)
	hpa := baseHPA()
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &window,
		},
	}
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
	}

	got := Analyze(hpa, true)
	if len(got.Behavior) != 1 {
		t.Fatalf("expected behavior entry, got %#v", got.Behavior)
	}
	if !strings.Contains(got.Behavior[0].Text, "stabilizationWindow=300s") {
		t.Fatalf("expected stabilization window text, got %s", got.Behavior[0].Text)
	}
	if !containsLine(got.Actions, "estimated wait up to ~300s") {
		t.Fatalf("expected scale-down action, got %#v", got.Actions)
	}
}

func TestAnalyzeAddsConcretePatchSuggestionForMaxReplicas(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 10
	hpa.Status.DesiredReplicas = 10
	hpa.Spec.MaxReplicas = 10
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
	}

	got := Analyze(hpa, true)
	if got.HealthScore >= 100 {
		t.Fatalf("expected reduced health score, got %d", got.HealthScore)
	}
	if len(got.Suggestions) == 0 {
		t.Fatalf("expected suggestions")
	}
	if !strings.Contains(got.Suggestions[0].Command, "kubectl patch hpa web") {
		t.Fatalf("expected kubectl patch command, got %#v", got.Suggestions[0])
	}
	if !strings.Contains(got.Suggestions[0].Command, "--dry-run=server") {
		t.Fatalf("expected dry-run command, got %#v", got.Suggestions[0])
	}
	if !strings.Contains(got.Suggestions[0].Patch, `"maxReplicas":20`) {
		t.Fatalf("expected maxReplicas patch, got %#v", got.Suggestions[0])
	}
	if len(got.Suggestions[0].Preconditions) == 0 || len(got.Suggestions[0].Warnings) == 0 {
		t.Fatalf("expected safety preconditions and warnings, got %#v", got.Suggestions[0])
	}
}

func TestAnalyzeWithOptionsDebugAndCustomHealthWeights(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.Conditions = append(hpa.Status.Conditions,
		autoscalingv2.HorizontalPodAutoscalerCondition{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
	)

	got := AnalyzeWithOptions(hpa, true, AnalysisOptions{
		HealthWeights: HealthWeights{ScalingLimited: IntWeight(40)},
		Debug:         true,
	})
	if got.HealthScore != 55 {
		t.Fatalf("expected custom score 55, got %d", got.HealthScore)
	}
	if !containsLine(got.Debug, "health: state=LIMITED score=55") {
		t.Fatalf("expected debug health line, got %#v", got.Debug)
	}
}

func TestAnalyzeExternalMetricDiagnosticsWhenStatusMissing(t *testing.T) {
	hpa := baseHPA()
	target := resource.MustParse("10")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}

	got := Analyze(hpa, true)
	if !containsLine(got.Interpretation, "External metric \"queue_depth\" is configured but no matching current metric status is reported") {
		t.Fatalf("expected external metric diagnostic, got %#v", got.Interpretation)
	}
	if !containsSuggestion(got.Suggestions, "Investigate stale external metric") {
		t.Fatalf("expected stale external metric suggestion, got %#v", got.Suggestions)
	}
}

func TestAnalyzeObjectMetricDiagnosticsShowsTargetComparison(t *testing.T) {
	hpa := baseHPA()
	target := resource.MustParse("100")
	current := resource.MustParse("150")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricSource{
				DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: "Service", Name: "web"},
				Metric:          autoscalingv2.MetricIdentifier{Name: "requests"},
				Target:          autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricStatus{
				DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: "Service", Name: "web"},
				Metric:          autoscalingv2.MetricIdentifier{Name: "requests"},
				Current:         autoscalingv2.MetricValueStatus{Value: &current},
			},
		},
	}

	got := Analyze(hpa, true)
	if !containsLine(got.Interpretation, "Object metric \"requests\" on Service/web is 1.500x its target") {
		t.Fatalf("expected object metric target comparison, got %#v", got.Interpretation)
	}
}

func TestAnalyzeAddsKEDADiagnosticsAndSuggestion(t *testing.T) {
	hpa := baseHPA()
	hpa.Name = "keda-hpa-worker"
	hpa.Labels = map[string]string{"scaledobject.keda.sh/name": "worker"}
	target := resource.MustParse("10")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "s0-queue"},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}

	got := Analyze(hpa, true)
	if !containsLine(got.Interpretation, "appears to be managed by KEDA") {
		t.Fatalf("expected KEDA diagnostic, got %#v", got.Interpretation)
	}
	if !containsSuggestion(got.Suggestions, "Inspect KEDA ScaledObject") {
		t.Fatalf("expected KEDA suggestion, got %#v", got.Suggestions)
	}
}

func TestAnalyzeToleranceBoundaries(t *testing.T) {
	// Case 1: Within tolerance (e.g. 73% vs 70% target -> ratio ~1.043, which is within 10% tolerance)
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 5
	hpa.Status.DesiredReplicas = 5
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceCPU, 70)}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceCPU, 73)}

	got := Analyze(hpa, true)
	if !containsLine(got.Interpretation, "within the Kubernetes default scaleUp tolerance") {
		t.Fatalf("expected directional tolerance mention within 10%% margin, got %#v", got.Interpretation)
	}

	// Case 2: Outside tolerance (e.g. 90% vs 70% target -> ratio ~1.286)
	hpa2 := baseHPA()
	hpa2.Status.CurrentReplicas = 5
	hpa2.Status.DesiredReplicas = 7
	hpa2.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceCPU, 70)}
	hpa2.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceCPU, 90)}

	got2 := Analyze(hpa2, true)
	if containsLine(got2.Interpretation, "consistent with tolerance-based no-scale") {
		t.Fatalf("did not expect tolerance mention for ratio outside margin, got %#v", got2.Interpretation)
	}
}

func TestAnalyzeMultipleMetricsCappedByMaxReplicas(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 10
	hpa.Status.DesiredReplicas = 10
	hpa.Spec.MaxReplicas = 10
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		resourceMetricSpec(corev1.ResourceCPU, 50),
		resourceMetricSpec(corev1.ResourceMemory, 100),
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		resourceMetricStatus(corev1.ResourceCPU, 90),    // ratio 1.800
		resourceMetricStatus(corev1.ResourceMemory, 80), // ratio 0.800
	}
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
	}

	got := Analyze(hpa, true)
	if got.Summary != "HPA is at maxReplicas." {
		t.Fatalf("expected HPA is at maxReplicas summary, got %s", got.Summary)
	}
	if got.ImpactMetric == nil || got.ImpactMetric.Name != "cpu" {
		t.Fatalf("expected cpu as the most influential metric, got %#v", got.ImpactMetric)
	}
	// When desiredReplicas == maxReplicas, the winner metric cannot be reliably determined
	if !containsLine(got.Interpretation, "winning metric cannot be reliably determined") {
		t.Fatalf("expected maxReplicas winner-detection warning, got %#v", got.Interpretation)
	}
}

func TestAnalyzeStabilizationWindowSpecificRules(t *testing.T) {
	// Set custom window for scaleDown
	window := int32(600)
	hpa := baseHPA()
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &window,
		},
	}
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "scale down stabilized"},
	}

	got := Analyze(hpa, true)
	if !containsLine(got.Actions, "estimated wait up to ~600s") {
		t.Fatalf("expected wait action referring to 600s window, got %#v", got.Actions)
	}
}

func TestAnalyzeScaleToZeroMinReplicasZero(t *testing.T) {
	minReplicas := int32(0)
	hpa := baseHPA()
	hpa.Spec.MinReplicas = &minReplicas
	hpa.Status.CurrentReplicas = 0
	hpa.Status.DesiredReplicas = 0

	got := Analyze(hpa, true)
	if got.ScaleToZero == nil || !got.ScaleToZero.Enabled {
		t.Fatalf("expected ScaleToZero enabled, got %#v", got.ScaleToZero)
	}
	if got.Summary != "HPA is scaled to zero (minReplicas=0); awaiting trigger to scale up." {
		t.Fatalf("unexpected summary: %s", got.Summary)
	}
	if !containsLine(got.Interpretation, "Scale-to-zero is enabled") {
		t.Fatalf("expected scale-to-zero interpretation, got %#v", got.Interpretation)
	}
}

func TestAnalyzeScaleToZeroColdStart(t *testing.T) {
	minReplicas := int32(0)
	hpa := baseHPA()
	hpa.Spec.MinReplicas = &minReplicas
	hpa.Status.CurrentReplicas = 3
	hpa.Status.DesiredReplicas = 0
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
	}

	got := Analyze(hpa, true)
	if got.ScaleToZero == nil || !got.ScaleToZero.Enabled {
		t.Fatalf("expected ScaleToZero enabled, got %#v", got.ScaleToZero)
	}
	if !got.ScaleToZero.ColdStart {
		t.Fatalf("expected ColdStart=true, got %#v", got.ScaleToZero)
	}
	if !strings.Contains(got.Summary, "cold start") {
		t.Fatalf("expected cold start mention in summary, got %s", got.Summary)
	}
}

func TestAnalyzeStabilizationRemaining(t *testing.T) {
	window := int32(300)
	hpa := baseHPA()
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &window,
		},
	}
	lastScaleTime := metav1.Now()
	hpa.Status.LastScaleTime = &lastScaleTime
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
	}

	got := Analyze(hpa, true)
	if got.StabilizationRemaining == nil {
		t.Fatalf("expected StabilizationRemaining to be set, got nil")
	}
	// The remaining time should be close to 300 (just scaled, so ~300s remaining)
	if *got.StabilizationRemaining > 300 || *got.StabilizationRemaining < 290 {
		t.Fatalf("expected StabilizationRemaining around 300, got %d", *got.StabilizationRemaining)
	}
}

func TestAnalyzeStaleStatusStructured(t *testing.T) {
	hpa := baseHPA()
	observed := int64(1)
	hpa.Generation = 3
	hpa.Status.ObservedGeneration = &observed

	got := Analyze(hpa, true)
	if got.StaleStatus == nil {
		t.Fatalf("expected StaleStatus to be set, got nil")
	}
	if got.StaleStatus.ObservedGeneration != 1 {
		t.Fatalf("expected ObservedGeneration=1, got %d", got.StaleStatus.ObservedGeneration)
	}
	if got.StaleStatus.CurrentGeneration != 3 {
		t.Fatalf("expected CurrentGeneration=3, got %d", got.StaleStatus.CurrentGeneration)
	}
	if got.StaleStatus.Diff != 2 {
		t.Fatalf("expected Diff=2, got %d", got.StaleStatus.Diff)
	}
	if !strings.HasPrefix(got.Summary, "[STALE STATUS]") {
		t.Fatalf("expected [STALE STATUS] prefix, got %s", got.Summary)
	}
}

func TestAnalyzeMetricImpactGuessConfidence(t *testing.T) {
	// Normal case - confidence should be medium
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceCPU, 80)}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceCPU, 88)}

	got := Analyze(hpa, false)
	if got.ImpactMetric == nil {
		t.Fatalf("expected ImpactMetric, got nil")
	}
	if got.ImpactMetric.Confidence != "medium" {
		t.Fatalf("expected confidence=medium, got %s", got.ImpactMetric.Confidence)
	}

	// MaxReplicas case - confidence should be low
	hpa2 := baseHPA()
	hpa2.Status.DesiredReplicas = 10
	hpa2.Spec.MaxReplicas = 10
	hpa2.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceCPU, 80)}
	hpa2.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceCPU, 88)}

	got2 := Analyze(hpa2, false)
	if got2.ImpactMetric == nil {
		t.Fatalf("expected ImpactMetric, got nil")
	}
	if got2.ImpactMetric.Confidence != "low" {
		t.Fatalf("expected confidence=low for maxReplicas case, got %s", got2.ImpactMetric.Confidence)
	}
}

func TestMostInfluentialMetricConsidersNonResourceMetrics(t *testing.T) {
	// Each row pairs a baseline cpu resource metric (ratio ~1.06) with a second
	// metric of a different type whose ratio is larger, so the second metric
	// must win MostInfluentialMetric.
	tests := []struct {
		name         string
		secondSpec   autoscalingv2.MetricSpec
		secondStatus autoscalingv2.MetricStatus
		wantName     string
		wantRatioMin float64
		wantRatioMax float64
		checkRatio   bool
	}{
		{
			name: "External",
			secondSpec: func() autoscalingv2.MetricSpec {
				v := resource.MustParse("10")
				return autoscalingv2.MetricSpec{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricSource{
						Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
						Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &v},
					},
				}
			}(),
			secondStatus: func() autoscalingv2.MetricStatus {
				v := resource.MustParse("20")
				return autoscalingv2.MetricStatus{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricStatus{
						Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth"},
						Current: autoscalingv2.MetricValueStatus{Value: &v},
					},
				}
			}(),
			wantName:     "queue_depth",
			wantRatioMin: 1.9,
			wantRatioMax: 2.1,
			checkRatio:   true,
		},
		{
			name: "Pods",
			secondSpec: func() autoscalingv2.MetricSpec {
				v := resource.MustParse("100m")
				return autoscalingv2.MetricSpec{
					Type: autoscalingv2.PodsMetricSourceType,
					Pods: &autoscalingv2.PodsMetricSource{
						Metric: autoscalingv2.MetricIdentifier{Name: "requests_per_second"},
						Target: autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: &v},
					},
				}
			}(),
			secondStatus: func() autoscalingv2.MetricStatus {
				v := resource.MustParse("180m")
				return autoscalingv2.MetricStatus{
					Type: autoscalingv2.PodsMetricSourceType,
					Pods: &autoscalingv2.PodsMetricStatus{
						Metric:  autoscalingv2.MetricIdentifier{Name: "requests_per_second"},
						Current: autoscalingv2.MetricValueStatus{AverageValue: &v},
					},
				}
			}(),
			wantName:     "requests_per_second",
			wantRatioMin: 1.7,
			wantRatioMax: 1.9,
			checkRatio:   true,
		},
		{
			name: "ContainerResource",
			secondSpec: func() autoscalingv2.MetricSpec {
				v := int32(50)
				return autoscalingv2.MetricSpec{
					Type: autoscalingv2.ContainerResourceMetricSourceType,
					ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
						Name:      corev1.ResourceCPU,
						Container: "sidecar",
						Target:    autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &v},
					},
				}
			}(),
			secondStatus: func() autoscalingv2.MetricStatus {
				v := int32(90)
				return autoscalingv2.MetricStatus{
					Type: autoscalingv2.ContainerResourceMetricSourceType,
					ContainerResource: &autoscalingv2.ContainerResourceMetricStatus{
						Name:      corev1.ResourceCPU,
						Container: "sidecar",
						Current:   autoscalingv2.MetricValueStatus{AverageUtilization: &v},
					},
				}
			}(),
			wantName:     "sidecar/cpu",
			wantRatioMin: 1.7,
			wantRatioMax: 1.9,
			checkRatio:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := baseHPA()
			hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
				resourceMetricSpec(corev1.ResourceCPU, 80),
				tc.secondSpec,
			}
			hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
				resourceMetricStatus(corev1.ResourceCPU, 85),
				tc.secondStatus,
			}

			got, ok := MostInfluentialMetric(hpa)
			if !ok {
				t.Fatal("expected an impact estimate")
			}
			if got.Name != tc.wantName {
				t.Fatalf("most influential metric = %q, want %q", got.Name, tc.wantName)
			}
			if tc.checkRatio && (got.Ratio < tc.wantRatioMin || got.Ratio > tc.wantRatioMax) {
				t.Fatalf("ratio = %.3f, want in [%.1f, %.1f]", got.Ratio, tc.wantRatioMin, tc.wantRatioMax)
			}
		})
	}
}

func TestRecommendedMaxReplicas_RespectsCap(t *testing.T) {
	hpa := baseHPA()
	hpa.Spec.MaxReplicas = 200
	hpa.Status.CurrentReplicas = 200
	hpa.Status.DesiredReplicas = 200
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
	}
	minReplicas := *hpa.Spec.MinReplicas
	suggestions := BuildSuggestions(hpa, minReplicas)
	if !containsSuggestion(suggestions, "Raise maxReplicas") {
		t.Fatalf("expected Raise maxReplicas suggestion, got %#v", suggestions)
	}
	// The suggested maxReplicas should not exceed the cap (200)
	for _, s := range suggestions {
		if s.Title == "Raise maxReplicas" && strings.Contains(s.Patch, `"maxReplicas":200`) {
			t.Fatalf("expected suggested maxReplicas to be capped, not 200 (same as current)")
		}
	}
}

func TestNilSafetyFindCondition(t *testing.T) {
	result := FindCondition(nil, "ScalingActive")
	if result != nil {
		t.Fatal("expected nil for nil HPA")
	}
}

func TestNilSafetySummarizeDirection(t *testing.T) {
	result := SummarizeDirection(nil, 1)
	if result != "HPA data is unavailable." {
		t.Fatalf("unexpected summary for nil HPA: %s", result)
	}
}

func TestNilSafetyMostInfluentialMetric(t *testing.T) {
	_, ok := MostInfluentialMetric(nil)
	if ok {
		t.Fatal("expected false for nil HPA")
	}
}

func TestNilSafetyMetricOutsideTarget(t *testing.T) {
	_, ok := MetricOutsideTarget(nil)
	if ok {
		t.Fatal("expected false for nil HPA")
	}
}

func TestDefaultMinReplicasConstant(t *testing.T) {
	if DefaultMinReplicas != 1 {
		t.Fatalf("expected DefaultMinReplicas=1, got %d", DefaultMinReplicas)
	}
}

func TestValidateHPA(t *testing.T) {
	tests := []struct {
		name      string
		hpa       func() *autoscalingv2.HorizontalPodAutoscaler
		wantNil   bool
		wantError bool // when wantNil is false, whether result.Health == "ERROR"
	}{
		{name: "NilHPA", hpa: func() *autoscalingv2.HorizontalPodAutoscaler { return nil }, wantNil: false, wantError: true},
		{name: "ValidHPA", hpa: baseHPA, wantNil: true},
		{
			name: "EmptyScaleTargetRef",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				hpa := baseHPA()
				hpa.Spec.ScaleTargetRef = autoscalingv2.CrossVersionObjectReference{}
				return hpa
			},
			wantNil: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := validateHPA(tc.hpa())
			if tc.wantNil {
				if result != nil {
					t.Fatalf("expected nil result, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil error result")
			}
			if tc.wantError && result.Health != "ERROR" {
				t.Errorf("Health = %q, want ERROR", result.Health)
			}
		})
	}
}

func TestResolveMinReplicas(t *testing.T) {
	tests := []struct {
		name string
		min  *int32
		want int32
	}{
		{name: "Default", min: nil, want: DefaultMinReplicas},
		{name: "Explicit", min: func() *int32 { v := int32(5); return &v }(), want: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := baseHPA()
			hpa.Spec.MinReplicas = tc.min
			if got := resolveMinReplicas(hpa); got != tc.want {
				t.Errorf("resolveMinReplicas = %d, want %d", got, tc.want)
			}
		})
	}
}
