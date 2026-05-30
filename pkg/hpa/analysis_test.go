package hpa

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
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
	if !containsLine(got.Interpretation, "consistent with tolerance-based no-scale") {
		t.Fatalf("expected tolerance interpretation, got %#v", got.Interpretation)
	}
}

func TestMostInfluentialMetricChoosesLargestDistance(t *testing.T) {
	hpa := baseHPA()
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
	if !containsLine(got.Interpretation, "only an impact estimate") {
		t.Fatalf("expected multi-metric estimate caveat, got %#v", got.Interpretation)
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

func TestNewListItemHighlightsImplicitMaxReplicasLimit(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 10
	hpa.Status.DesiredReplicas = 10
	hpa.Spec.MaxReplicas = 10

	got := NewListItem(Analyze(hpa, false))
	if got.Health != "LIMITED" {
		t.Fatalf("expected LIMITED health, got %s", got.Health)
	}
	if got.Issue != "LIMITED: maxReplicas" {
		t.Fatalf("unexpected issue: %s", got.Issue)
	}
}

func TestWriteListTextVisuallyHighlightsProblems(t *testing.T) {
	report := ListReport{Items: []ListItem{
		{Namespace: "default", Name: "web", Current: 2, Desired: 2, Health: "OK", Summary: "steady"},
		{Namespace: "default", Name: "api", Current: 2, Desired: 2, Health: "ERROR", Issue: "ERROR: FailedGetResourceMetric", Summary: "broken"},
		{Namespace: "default", Name: "worker", Current: 5, Desired: 5, Health: "LIMITED", Issue: "LIMITED: TooManyReplicas", Summary: "capped"},
	}}

	var out bytes.Buffer
	if err := WriteListText(&out, report, ListTextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "ERROR") {
		t.Fatalf("expected ERROR marker in %q", text)
	}
	if !strings.Contains(text, "ScalingLimited") {
		t.Fatalf("expected LIMITED marker in %q", text)
	}
}

func TestWriteListTextColorizesHealthWhenEnabled(t *testing.T) {
	report := ListReport{Items: []ListItem{
		{Namespace: "default", Name: "api", Current: 2, Desired: 2, Health: "ERROR", Issue: "ERROR: FailedGetResourceMetric", Summary: "broken"},
	}}

	var out bytes.Buffer
	if err := WriteListText(&out, report, ListTextOptions{Theme: style.NewTheme(true)}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ERROR") {
		t.Fatalf("expected ERROR marker, got %q", out.String())
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("expected ANSI escape codes in colorized output, got %q", out.String())
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
	if !containsLine(got.Actions, "wait up to about 300s") {
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
	if !strings.Contains(got.Suggestions[0].Patch, `"maxReplicas":20`) {
		t.Fatalf("expected maxReplicas patch, got %#v", got.Suggestions[0])
	}
}

func baseHPA() *autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := int32(2)
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web", Generation: 1},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    &minReplicas,
			MaxReplicas:    10,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 2,
			DesiredReplicas: 2,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
			},
		},
	}
}

func resourceMetricSpec(name corev1.ResourceName, target int32) autoscalingv2.MetricSpec {
	return autoscalingv2.MetricSpec{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: name,
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: &target,
			},
		},
	}
}

func resourceMetricStatus(name corev1.ResourceName, current int32) autoscalingv2.MetricStatus {
	return autoscalingv2.MetricStatus{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricStatus{
			Name: name,
			Current: autoscalingv2.MetricValueStatus{
				AverageUtilization: &current,
			},
		},
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

func TestWriteStatusDiff_NoChanges(t *testing.T) {
	analysis := Analyze(baseHPA(), false)
	prev := analysis // copy
	state := WatchState{Previous: &prev, Current: &analysis}

	var buf bytes.Buffer
	if err := WriteStatusDiff(&buf, state, style.NewTheme(false)); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "HPA default/web") {
		t.Errorf("expected HPA header, got:\n%s", output)
	}
	// When unchanged, replicas should show without emphasis
	if !strings.Contains(output, "current=2 desired=2") {
		t.Errorf("expected plain replicas, got:\n%s", output)
	}
}

func TestWriteStatusDiff_ReplicasChanged(t *testing.T) {
	prev := Analyze(baseHPA(), false)
	prev.Current = 3
	prev.Desired = 3

	curr := Analyze(baseHPA(), false)
	curr.Current = 5
	curr.Desired = 7

	state := WatchState{Previous: &prev, Current: &curr}
	var buf bytes.Buffer
	if err := WriteStatusDiff(&buf, state, style.NewTheme(false)); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "current=5") {
		t.Errorf("expected current=5, got:\n%s", output)
	}
	if !strings.Contains(output, "desired=7") {
		t.Errorf("expected desired=7, got:\n%s", output)
	}
}

func TestWriteStatusDiff_ConditionsChanged(t *testing.T) {
	hpa := baseHPA()
	prev := Analyze(hpa, false)

	// Modify HPA to have ScalingLimited
	hpa2 := baseHPA()
	hpa2.Status.Conditions = append(hpa2.Status.Conditions,
		autoscalingv2.HorizontalPodAutoscalerCondition{
			Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas",
		},
	)
	curr := Analyze(hpa2, false)

	state := WatchState{Previous: &prev, Current: &curr}
	var buf bytes.Buffer
	if err := WriteStatusDiff(&buf, state, style.NewTheme(false)); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "ScalingLimited") {
		t.Errorf("expected ScalingLimited in diff, got:\n%s", output)
	}
}

func TestWriteStatusDiff_NilPrevious(t *testing.T) {
	// Diff with nil previous should not panic; the caller should use
	// WriteStatusText for the first iteration, but WriteStatusDiff
	// should handle nil gracefully.
	curr := Analyze(baseHPA(), false)
	state := WatchState{Previous: nil, Current: &curr}

	var buf bytes.Buffer
	// This should still work even without previous
	err := WriteStatusDiff(&buf, state, style.NewTheme(false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "HPA default/web") {
		t.Errorf("expected HPA header in diff output, got:\n%s", buf.String())
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
	if !containsLine(got.Interpretation, "consistent with tolerance-based no-scale") {
		t.Fatalf("expected tolerance mention within 10%% margin, got %#v", got.Interpretation)
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
	if !containsLine(got.Interpretation, "memory has the largest distance from target") && !containsLine(got.Interpretation, "cpu has the largest distance from target") {
		// Either cpu (ratio 1.8) or memory (ratio 0.8) could be evaluated.
		// Ratio distance: CPU: 1.8-1 = 0.8. Memory: 0.8-1 = -0.2 (abs 0.2).
		// So CPU (0.8 distance) should be the winner.
		if !containsLine(got.Interpretation, "cpu has the largest distance from target (ratio 1.800)") {
			t.Fatalf("expected CPU to be chosen as most influential, got %#v", got.Interpretation)
		}
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
	if !containsLine(got.Actions, "wait up to about 600s") {
		t.Fatalf("expected wait action referring to 600s window, got %#v", got.Actions)
	}
}
