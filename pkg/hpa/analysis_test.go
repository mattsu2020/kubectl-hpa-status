package hpa

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
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
	if !containsLine(got.Interpretation, "tolerance-confirmed") {
		t.Fatalf("expected tolerance-confirmed interpretation, got %#v", got.Interpretation)
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

func TestBuildSuggestionsAddsBehaviorAndToleranceRecommendations(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 5
	hpa.Status.DesiredReplicas = 5
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceCPU, 70)}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceCPU, 75)}

	got := Analyze(hpa, true)
	if !containsSuggestion(got.Suggestions, "Add explicit scale-up policy") {
		t.Fatalf("expected scale-up policy suggestion, got %#v", got.Suggestions)
	}
	if !containsSuggestion(got.Suggestions, "Review scale-up tolerance") {
		t.Fatalf("expected tolerance suggestion, got %#v", got.Suggestions)
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

func baseHPA() *autoscalingv2.HorizontalPodAutoscaler {
	return testutil.BuildHPA("default", "web",
		testutil.WithMinMax(2, 10),
		testutil.WithReplicas(2, 2),
		testutil.WithGeneration(1),
		testutil.WithScaleTargetRef("Deployment", "web"),
		testutil.WithConditions(
			autoscalingv2.HorizontalPodAutoscalerCondition{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		),
	)
}

func containsSuggestion(suggestions []Suggestion, title string) bool {
	for _, suggestion := range suggestions {
		if suggestion.Title == title {
			return true
		}
	}
	return false
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

// boolPtr returns a pointer to b, used for optional table-driven bool fields.
func boolPtr(b bool) *bool { return &b }

// TestWriteStatusTextWithOptions_RendersWarnings verifies that Analysis.Warnings
// are surfaced in plain-text output. Previously Warnings only appeared in
// JSON/YAML output, leaving text users unaware of enrichment failures.
func TestWriteStatusTextWithOptions_RendersWarnings(t *testing.T) {
	report := StatusReport{Analysis: Analyze(baseHPA(), true)}
	report.Analysis.Warnings = []string{
		"resource check unavailable: failed to read scale target resources: connection refused",
		"health trend append failed: permission denied",
	}

	var buf bytes.Buffer
	if err := WriteStatusTextWithOptions(&buf, report, StatusTextOptions{Theme: style.NewTheme(false)}); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	if !strings.Contains(output, "warning:") {
		t.Fatalf("expected 'warning:' section header, got:\n%s", output)
	}
	for _, w := range report.Analysis.Warnings {
		if !strings.Contains(output, w) {
			t.Fatalf("expected warning %q in output:\n%s", w, output)
		}
	}
}

// TestWriteStatusTextWithOptions_NoWarningsSectionWhenEmpty verifies the
// warnings section is omitted entirely when there are no warnings, so the
// baseline output for healthy HPAs stays unchanged.
func TestWriteStatusTextWithOptions_NoWarningsSectionWhenEmpty(t *testing.T) {
	report := StatusReport{Analysis: Analyze(baseHPA(), true)}
	var buf bytes.Buffer
	if err := WriteStatusTextWithOptions(&buf, report, StatusTextOptions{Theme: style.NewTheme(false)}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "warning:") {
		t.Fatalf("expected no warnings section for empty warnings, got:\n%s", buf.String())
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
	if !containsLine(got.Interpretation, "tolerance-confirmed") {
		t.Fatalf("expected tolerance-confirmed mention within 10%% margin, got %#v", got.Interpretation)
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

func TestStructuredInterpretation_ScalingInactive(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 3
	hpa.Status.DesiredReplicas = 0
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionFalse, Reason: "FailedGetResourceMetric", Message: "missing cpu metrics"},
	}

	got := Analyze(hpa, true)
	if len(got.StructuredInterpretation) == 0 {
		t.Fatalf("expected structured interpretation, got none")
	}
	found := false
	for _, msg := range got.StructuredInterpretation {
		if msg.Reason == "ScalingInactive" {
			found = true
			if msg.Severity != "error" {
				t.Fatalf("expected severity=error, got %s", msg.Severity)
			}
			if msg.NextStep == "" {
				t.Fatalf("expected non-empty NextStep for ScalingInactive")
			}
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=ScalingInactive, got %#v", got.StructuredInterpretation)
	}
}

func TestStructuredInterpretation_StaleStatus(t *testing.T) {
	hpa := baseHPA()
	observed := int64(1)
	hpa.Generation = 3
	hpa.Status.ObservedGeneration = &observed

	got := Analyze(hpa, true)
	found := false
	for _, msg := range got.StructuredInterpretation {
		if msg.Reason == "StaleStatus" {
			found = true
			if msg.Severity != "warning" {
				t.Fatalf("expected severity=warning, got %s", msg.Severity)
			}
			if msg.NextStep == "" {
				t.Fatalf("expected non-empty NextStep for StaleStatus")
			}
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=StaleStatus, got %#v", got.StructuredInterpretation)
	}
}

func TestStructuredInterpretation_ScaleDownStabilized(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 8
	hpa.Status.DesiredReplicas = 8
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
	}

	got := Analyze(hpa, true)
	found := false
	for _, msg := range got.StructuredInterpretation {
		if msg.Reason == "ScaleDownStabilized" {
			found = true
			if msg.Severity != "info" {
				t.Fatalf("expected severity=info, got %s", msg.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=ScaleDownStabilized, got %#v", got.StructuredInterpretation)
	}
}

func TestStructuredActions_RestoreMetrics(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 3
	hpa.Status.DesiredReplicas = 0
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionFalse, Reason: "FailedGetResourceMetric", Message: "missing cpu metrics"},
	}

	got := Analyze(hpa, true)
	if len(got.StructuredActions) == 0 {
		t.Fatalf("expected structured actions, got none")
	}
	found := false
	for _, msg := range got.StructuredActions {
		if msg.Reason == "RestoreMetrics" {
			found = true
			if msg.Severity != "error" {
				t.Fatalf("expected severity=error, got %s", msg.Severity)
			}
			if msg.NextStep == "" {
				t.Fatalf("expected non-empty NextStep for RestoreMetrics")
			}
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=RestoreMetrics, got %#v", got.StructuredActions)
	}
}

func TestFormatMetricStatusIncludesExternalSelector(t *testing.T) {
	target := resource.MustParse("10")
	current := resource.MustParse("20")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name: "queue_depth",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"queue": "payments"},
					},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}

	got := FormatMetricStatus(hpa, autoscalingv2.MetricStatus{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricStatus{
			Metric: autoscalingv2.MetricIdentifier{
				Name: "queue_depth",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"queue": "payments"},
				},
			},
			Current: autoscalingv2.MetricValueStatus{Value: &current},
		},
	})

	if got.Selector != "queue=payments" {
		t.Fatalf("expected selector in metric, got %#v", got)
	}
	if !strings.Contains(got.Text, `selector="queue=payments"`) {
		t.Fatalf("expected selector in text, got %q", got.Text)
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

func TestInterpretDetectsMetricDisagreement(t *testing.T) {
	target := resource.MustParse("10")
	lowCurrent := resource.MustParse("5")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		resourceMetricSpec(corev1.ResourceCPU, 80),
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		resourceMetricStatus(corev1.ResourceCPU, 50), // ratio ~0.625, below target
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth"},
				Current: autoscalingv2.MetricValueStatus{Value: &lowCurrent}, // ratio 0.5, below target
			},
		},
	}

	// No disagreement when both are below target
	got := Analyze(hpa, true)
	if containsLine(got.Interpretation, "Metric disagreement detected") {
		t.Fatal("did not expect disagreement when both metrics are below target")
	}

	// Now make the external metric above target to create disagreement
	highCurrent := resource.MustParse("20")
	hpa.Status.CurrentMetrics[1] = autoscalingv2.MetricStatus{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricStatus{
			Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth"},
			Current: autoscalingv2.MetricValueStatus{Value: &highCurrent}, // ratio 2.0, above target
		},
	}

	got2 := Analyze(hpa, true)
	if !containsLine(got2.Interpretation, "Metric disagreement detected") {
		t.Fatalf("expected metric disagreement warning, got %#v", got2.Interpretation)
	}
	if !containsLine(got2.Interpretation, "cpu") {
		t.Fatalf("expected cpu mentioned in disagreement, got %#v", got2.Interpretation)
	}
	if !containsLine(got2.Interpretation, "queue_depth") {
		t.Fatalf("expected queue_depth mentioned in disagreement, got %#v", got2.Interpretation)
	}
}

func TestDiagnoseMetricsPipeline_NilHPA(t *testing.T) {
	got := DiagnoseMetricsPipeline(nil)
	if got != nil {
		t.Fatalf("expected nil for nil HPA, got %#v", got)
	}
}

func TestDiagnoseMetricsPipeline(t *testing.T) {
	// externalValueSpec/Status build a value-type External metric pair. Reused
	// across the partial-match and external-healthy cases.
	externalValueSpec := func(name string) autoscalingv2.MetricSpec {
		target := resource.MustParse("10")
		return autoscalingv2.MetricSpec{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: name},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		}
	}
	externalValueStatus := func(name string) autoscalingv2.MetricStatus {
		current := resource.MustParse("12")
		return autoscalingv2.MetricStatus{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: name},
				Current: autoscalingv2.MetricValueStatus{Value: &current},
			},
		}
	}

	tests := []struct {
		name             string
		specMetrics      []autoscalingv2.MetricSpec
		currentMetrics   []autoscalingv2.MetricStatus
		wantOverall      string
		wantNumChecks    int
		wantStatusByName map[string]string // metric name -> expected status (empty means don't check)
		wantRemediation  *bool             // nil = don't check; otherwise expect non-empty matching the dereferenced value
		wantMetricType   string            // optional: assert MetricType of the first check
	}{
		{
			name:          "NoSpecMetrics",
			wantOverall:   "healthy",
			wantNumChecks: 0,
		},
		{
			name: "AllMetricsMissing",
			specMetrics: []autoscalingv2.MetricSpec{
				resourceMetricSpec(corev1.ResourceCPU, 80),
				resourceMetricSpec(corev1.ResourceMemory, 70),
			},
			// No current metrics set — simulates metrics server being down.
			wantOverall:     "error",
			wantNumChecks:   2,
			wantRemediation: boolPtr(true),
		},
		{
			name: "AllMetricsHealthy",
			specMetrics: []autoscalingv2.MetricSpec{
				resourceMetricSpec(corev1.ResourceCPU, 80),
				resourceMetricSpec(corev1.ResourceMemory, 70),
			},
			currentMetrics: []autoscalingv2.MetricStatus{
				resourceMetricStatus(corev1.ResourceCPU, 75),
				resourceMetricStatus(corev1.ResourceMemory, 65),
			},
			wantOverall:   "healthy",
			wantNumChecks: 2,
		},
		{
			name: "PartialMatches",
			specMetrics: []autoscalingv2.MetricSpec{
				resourceMetricSpec(corev1.ResourceCPU, 80),
				externalValueSpec("queue_depth"),
			},
			currentMetrics: []autoscalingv2.MetricStatus{
				resourceMetricStatus(corev1.ResourceCPU, 75),
				// External metric intentionally omitted — simulates partial missing.
			},
			wantOverall:      "degraded",
			wantNumChecks:    2,
			wantStatusByName: map[string]string{"cpu": "healthy", "queue_depth": "missing"},
			wantRemediation:  boolPtr(true),
		},
		{
			name:           "ExternalMetricHealthy",
			specMetrics:    []autoscalingv2.MetricSpec{externalValueSpec("queue_depth")},
			currentMetrics: []autoscalingv2.MetricStatus{externalValueStatus("queue_depth")},
			wantOverall:    "healthy",
			wantNumChecks:  1,
			wantMetricType: "External",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := baseHPA()
			hpa.Spec.Metrics = tc.specMetrics
			hpa.Status.CurrentMetrics = tc.currentMetrics

			got := DiagnoseMetricsPipeline(hpa)
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.OverallStatus != tc.wantOverall {
				t.Errorf("OverallStatus = %q, want %q", got.OverallStatus, tc.wantOverall)
			}
			if len(got.PerMetricChecks) != tc.wantNumChecks {
				t.Fatalf("got %d PerMetricChecks, want %d", len(got.PerMetricChecks), tc.wantNumChecks)
			}
			if tc.wantStatusByName != nil {
				for name, wantStatus := range tc.wantStatusByName {
					found := false
					for _, check := range got.PerMetricChecks {
						if check.MetricName == name {
							found = true
							if check.Status != wantStatus {
								t.Errorf("metric %s status = %q, want %q", name, check.Status, wantStatus)
							}
						}
					}
					if !found {
						t.Errorf("metric %s not found in checks", name)
					}
				}
			}
			if tc.wantMetricType != "" && got.PerMetricChecks[0].MetricType != tc.wantMetricType {
				t.Errorf("first check MetricType = %q, want %q", got.PerMetricChecks[0].MetricType, tc.wantMetricType)
			}
			if tc.wantRemediation != nil {
				gotRemediation := len(got.RemediationSteps) > 0
				if gotRemediation != *tc.wantRemediation {
					t.Errorf("RemediationSteps present = %v, want %v", gotRemediation, *tc.wantRemediation)
				}
			}
		})
	}
}

func TestApplyEnrichmentPenalties(t *testing.T) {
	// inactiveKEDA returns a KEDAAnalysis whose only trigger is Inactive, which
	// triggers the KEDA penalty.
	inactiveKEDA := func() *KEDAAnalysis {
		return &KEDAAnalysis{
			Triggers: []KEDATriggerSummary{{Type: "prometheus", Status: "Inactive"}},
		}
	}
	// activeKEDA returns a KEDAAnalysis whose trigger is Active (no penalty).
	activeKEDA := func() *KEDAAnalysis {
		return &KEDAAnalysis{
			Triggers: []KEDATriggerSummary{{Type: "prometheus", Status: "Active"}},
		}
	}
	vpaConflict := func() *VPAConflictInfo {
		return &VPAConflictInfo{VPAName: "my-vpa", UpdateMode: "Auto"}
	}

	tests := []struct {
		name       string
		health     string
		score      int
		keda       func() *KEDAAnalysis
		vpa        func() *VPAConflictInfo
		weights    HealthWeights
		wantScore  int
		wantHealth string
	}{
		{name: "KEDAInactiveTrigger", health: "OK", score: 95, keda: inactiveKEDA, wantScore: 80, wantHealth: "LIMITED"},
		{name: "VPAConflict", health: "OK", score: 95, vpa: vpaConflict, wantScore: 75, wantHealth: "LIMITED"},
		{name: "BothPenalties", health: "OK", score: 95, keda: inactiveKEDA, vpa: vpaConflict, wantScore: 60, wantHealth: "LIMITED"},
		{name: "NilEnrichment", health: "OK", score: 95, wantScore: 95, wantHealth: "OK"},
		{name: "CustomWeights", health: "OK", score: 95, keda: inactiveKEDA, vpa: vpaConflict,
			weights:   HealthWeights{KEDAInactiveTrigger: IntWeight(30), VPAConflict: IntWeight(40)},
			wantScore: 25, wantHealth: "LIMITED"},
		{name: "ScoreNotBelowZero", health: "OK", score: 10, keda: inactiveKEDA, vpa: vpaConflict, wantScore: 0, wantHealth: "LIMITED"},
		{name: "DoesNotDowngradeERROR", health: "ERROR", score: 55, keda: inactiveKEDA, wantScore: 40, wantHealth: "ERROR"},
		{name: "KEDAHealthyTriggersNoPenalty", health: "OK", score: 95, keda: activeKEDA, wantScore: 95, wantHealth: "OK"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &Analysis{Health: tc.health, HealthScore: tc.score}
			if tc.keda != nil {
				a.KEDAInfo = tc.keda()
			}
			if tc.vpa != nil {
				a.VPAConflict = tc.vpa()
			}
			ApplyEnrichmentPenalties(a, tc.weights)
			if a.HealthScore != tc.wantScore {
				t.Errorf("HealthScore = %d, want %d", a.HealthScore, tc.wantScore)
			}
			if a.Health != tc.wantHealth {
				t.Errorf("Health = %q, want %q", a.Health, tc.wantHealth)
			}
		})
	}
}

// TestApplyEnrichmentPenalties_NilAnalysis verifies the function is nil-safe.
func TestApplyEnrichmentPenalties_NilAnalysis(_ *testing.T) {
	ApplyEnrichmentPenalties(nil, HealthWeights{})
	// Should not panic.
}

func TestBuildSuggestions(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(hpa *autoscalingv2.HorizontalPodAutoscaler)
		wantSuggestion string
		wantPresent    bool
	}{
		{
			name: "NoRaiseMaxReplicasWhenCurrentReplicasZero",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 0
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
				}
			},
			wantSuggestion: "Raise maxReplicas",
			wantPresent:    false,
		},
		{
			name: "RaiseMaxReplicasWhenCurrentReplicasPositive",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 10
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
				}
			},
			wantSuggestion: "Raise maxReplicas",
			wantPresent:    true,
		},
		{
			name: "NoLowerMinReplicasWhenMinIsOne",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				minReplicas := int32(1)
				hpa.Spec.MinReplicas = &minReplicas
				hpa.Status.CurrentReplicas = 1
				hpa.Status.DesiredReplicas = 1
				hpa.Spec.MaxReplicas = 10
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooFewReplicas"},
				}
			},
			wantSuggestion: "Lower minReplicas",
			wantPresent:    false,
		},
		{
			name: "LowerMinReplicasWhenMinAboveOne",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				minReplicas := int32(3)
				hpa.Spec.MinReplicas = &minReplicas
				hpa.Status.CurrentReplicas = 3
				hpa.Status.DesiredReplicas = 3
				hpa.Spec.MaxReplicas = 10
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooFewReplicas"},
				}
			},
			wantSuggestion: "Lower minReplicas",
			wantPresent:    true,
		},
		{
			name: "NoShortenStabilizationAtDefault300s",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				window := int32(300)
				hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
				}
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
				}
			},
			wantSuggestion: "Shorten scale-down stabilization",
			wantPresent:    false,
		},
		{
			name: "ShortenStabilizationAtExplicitlyHighWindow",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				window := int32(600)
				hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
				}
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
				}
			},
			wantSuggestion: "Shorten scale-down stabilization",
			wantPresent:    true,
		},
		{
			name: "ShortenStabilizationAtExplicitlySetBelow300s",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				window := int32(120)
				hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
				}
				hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
				}
			},
			wantSuggestion: "Shorten scale-down stabilization",
			wantPresent:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := baseHPA()
			tc.setup(hpa)
			minReplicas := *hpa.Spec.MinReplicas
			suggestions := BuildSuggestions(hpa, minReplicas)
			got := containsSuggestion(suggestions, tc.wantSuggestion)
			if got != tc.wantPresent {
				t.Fatalf("suggestion %q present = %v, want %v (suggestions: %#v)", tc.wantSuggestion, got, tc.wantPresent, suggestions)
			}
		})
	}
}

func TestExternalMetricMatching_DistinguishesSelector(t *testing.T) {
	target := resource.MustParse("10")
	currentA := resource.MustParse("20")
	currentB := resource.MustParse("5")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "queue_depth",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "payments"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "queue_depth",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "orders"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}
	// Only the "payments" selector metric is present in currentMetrics.
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "payments"}}},
				Current: autoscalingv2.MetricValueStatus{Value: &currentA},
			},
		},
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "orders"}}},
				Current: autoscalingv2.MetricValueStatus{Value: &currentB},
			},
		},
	}

	got := Analyze(hpa, true)

	// Both metrics should be found (no "missing" diagnostic for either)
	paymentsFound := false
	ordersFound := false
	for _, line := range got.Interpretation {
		if strings.Contains(line, `queue_depth`) && strings.Contains(line, "payments") && strings.Contains(line, "is configured but no matching") {
			t.Errorf("payments metric should not be reported missing: %s", line)
		}
		if strings.Contains(line, `queue_depth`) && strings.Contains(line, "2.000x") {
			paymentsFound = true
		}
		if strings.Contains(line, `queue_depth`) && strings.Contains(line, "0.500x") {
			ordersFound = true
		}
	}
	if !paymentsFound {
		t.Fatal("expected payments external metric ratio diagnostic")
	}
	if !ordersFound {
		t.Fatal("expected orders external metric ratio diagnostic")
	}

	// Diagnostics should show "payments" selector and "orders" selector separately
	pipeline := DiagnoseMetricsPipeline(hpa)
	if pipeline.OverallStatus != "healthy" {
		t.Fatalf("expected healthy pipeline, got %s", pipeline.OverallStatus)
	}
}

func TestExternalMetricMatching_SameNameDifferentSelector_MissingDetected(t *testing.T) {
	target := resource.MustParse("10")
	currentA := resource.MustParse("20")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "queue_depth",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "payments"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "queue_depth",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "orders"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}
	// Only "payments" is present — "orders" should be detected as missing.
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "payments"}}},
				Current: autoscalingv2.MetricValueStatus{Value: &currentA},
			},
		},
	}

	pipeline := DiagnoseMetricsPipeline(hpa)
	if pipeline.OverallStatus != "degraded" {
		t.Fatalf("expected degraded pipeline, got %s", pipeline.OverallStatus)
	}
	healthyCount := 0
	missingCount := 0
	for _, check := range pipeline.PerMetricChecks {
		switch check.Status {
		case "healthy":
			healthyCount++
		case "missing":
			missingCount++
		}
	}
	if healthyCount != 1 {
		t.Fatalf("expected 1 healthy, got %d", healthyCount)
	}
	if missingCount != 1 {
		t.Fatalf("expected 1 missing, got %d", missingCount)
	}
}

func TestObjectMetricMatching_DistinguishesDescribedObject(t *testing.T) {
	target := resource.MustParse("100")
	currentA := resource.MustParse("150")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricSource{
				DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: "Service", Name: "web"},
				Metric:          autoscalingv2.MetricIdentifier{Name: "requests"},
				Target:          autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
		{
			Type: autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricSource{
				DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: "Service", Name: "api"},
				Metric:          autoscalingv2.MetricIdentifier{Name: "requests"},
				Target:          autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}
	// Only the "web" object is present — "api" should be missing.
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricStatus{
				DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: "Service", Name: "web"},
				Metric:          autoscalingv2.MetricIdentifier{Name: "requests"},
				Current:         autoscalingv2.MetricValueStatus{Value: &currentA},
			},
		},
	}

	got := Analyze(hpa, true)
	if !containsLine(got.Interpretation, "Object metric \"requests\"") {
		t.Fatal("expected object metric diagnostic")
	}

	// Diagnostics pipeline should detect "api" as missing.
	pipeline := DiagnoseMetricsPipeline(hpa)
	if pipeline.OverallStatus != "degraded" {
		t.Fatalf("expected degraded pipeline, got %s", pipeline.OverallStatus)
	}
}

func TestPodsMetricMatching_DistinguishesSelector(t *testing.T) {
	averageTarget := resource.MustParse("100m")
	averageCurrentA := resource.MustParse("120m")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "requests_per_second",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: &averageTarget},
			},
		},
		{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "requests_per_second",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: &averageTarget},
			},
		},
	}
	// Only the "web" selector metric is present.
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "requests_per_second", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}},
				Current: autoscalingv2.MetricValueStatus{AverageValue: &averageCurrentA},
			},
		},
	}

	pipeline := DiagnoseMetricsPipeline(hpa)
	if pipeline.OverallStatus != "degraded" {
		t.Fatalf("expected degraded pipeline, got %s", pipeline.OverallStatus)
	}
	healthyCount := 0
	missingCount := 0
	for _, check := range pipeline.PerMetricChecks {
		switch check.Status {
		case "healthy":
			healthyCount++
		case "missing":
			missingCount++
		}
	}
	if healthyCount != 1 {
		t.Fatalf("expected 1 healthy, got %d", healthyCount)
	}
	if missingCount != 1 {
		t.Fatalf("expected 1 missing, got %d", missingCount)
	}
}

func TestHealthScorePenaltyGating(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(hpa *autoscalingv2.HorizontalPodAutoscaler)
		wantFullScore bool // true: expect score==100; false: expect score<100
	}{
		{
			name: "NoMinReplicasPenaltyWithoutScalingLimited",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				// At minReplicas=2 but no ScalingLimited — normal low-traffic state.
				hpa.Status.CurrentReplicas = 2
				hpa.Status.DesiredReplicas = 2
			},
			wantFullScore: true,
		},
		{
			name: "MinReplicasPenaltyWithScalingLimited",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 2
				hpa.Status.DesiredReplicas = 2
				hpa.Status.Conditions = append(hpa.Status.Conditions,
					autoscalingv2.HorizontalPodAutoscalerCondition{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooFewReplicas"},
				)
			},
			wantFullScore: false,
		},
		{
			name: "ImplicitMaxReplicas_NoPenaltyWithoutPressure",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 10
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				// No ScalingLimited, no metric above target — no penalty expected.
			},
			wantFullScore: true,
		},
		{
			name: "ImplicitMaxReplicas_PenaltyWithMetricPressure",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 10
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				hpa.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceCPU, 80)}
				hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceCPU, 90)} // ratio > 1.0
			},
			wantFullScore: false,
		},
		{
			name: "ImplicitMaxReplicas_PenaltyWithScalingLimited",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 10
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				hpa.Status.Conditions = append(hpa.Status.Conditions,
					autoscalingv2.HorizontalPodAutoscalerCondition{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
				)
			},
			wantFullScore: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := baseHPA()
			tc.setup(hpa)
			_, score := Health(hpa, 2)
			if tc.wantFullScore && score != 100 {
				t.Fatalf("score = %d, want 100 (no penalty)", score)
			}
			if !tc.wantFullScore && score >= 100 {
				t.Fatalf("score = %d, want < 100 (penalty expected)", score)
			}
		})
	}
}

func TestStructuredInterpretation_IncludesExternalMetricDiagnostics(t *testing.T) {
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
	found := false
	for _, msg := range got.StructuredInterpretation {
		if msg.Reason == "ExternalMetricDiagnostic" && strings.Contains(msg.Message, "queue_depth") {
			found = true
			if msg.Severity == "" {
				t.Fatalf("expected non-empty severity for ExternalMetricDiagnostic")
			}
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=ExternalMetricDiagnostic, got %#v", got.StructuredInterpretation)
	}
}

func TestStructuredInterpretation_IncludesLimitation(t *testing.T) {
	hpa := baseHPA()
	got := Analyze(hpa, true)
	found := false
	for _, msg := range got.StructuredInterpretation {
		if msg.Reason == "Limitation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=Limitation, got %#v", got.StructuredInterpretation)
	}
}

func TestInterpret_NoDuplicateLimitation(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 3
	hpa.Status.DesiredReplicas = 0
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionFalse, Reason: "FailedGetResourceMetric", Message: "missing cpu metrics"},
	}
	got := Analyze(hpa, true)
	count := 0
	for _, line := range got.Interpretation {
		if strings.Contains(line, "This plugin uses existing HPA status") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 limitation line, got %d", count)
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

func TestHealthWeightsExplicitZeroDisablesPenalty(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.Conditions = append(hpa.Status.Conditions,
		autoscalingv2.HorizontalPodAutoscalerCondition{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
	)
	_, defaultScore := Health(hpa, 2)

	// With ScalingLimited explicitly set to 0, penalty should be disabled.
	zeroResult := HealthWithWeights(hpa, 2, HealthWeights{ScalingLimited: IntWeight(0)})
	zeroScore := zeroResult.Score
	if zeroScore != defaultScore+healthPenaltyScalingLimited {
		t.Fatalf("expected %d (score without ScalingLimited penalty), got %d", defaultScore+healthPenaltyScalingLimited, zeroScore)
	}
}

func TestCollectDiagnosticsIncludesAllPhases(t *testing.T) {
	hpa := baseHPA()
	hpa.Name = "keda-hpa-worker"
	hpa.Labels = map[string]string{"scaledobject.keda.sh/name": "worker"}
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

	entries := CollectDiagnostics(hpa, 2)
	reasons := make(map[string]int)
	for _, e := range entries {
		reasons[e.Reason]++
	}

	// Should have core cases (NoScaleVisible, Limitation)
	if reasons["NoScaleVisible"] == 0 {
		t.Fatal("expected NoScaleVisible from core cases")
	}
	// Should have ExternalMetricDiagnostic
	if reasons["ExternalMetricDiagnostic"] == 0 {
		t.Fatal("expected ExternalMetricDiagnostic")
	}
	// Should have KEDADiagnostic
	if reasons["KEDADiagnostic"] == 0 {
		t.Fatal("expected KEDADiagnostic")
	}
	// Should have Limitation
	if reasons["Limitation"] == 0 {
		t.Fatal("expected Limitation")
	}
}

func TestCollectDiagnosticsTextStructuredParity(t *testing.T) {
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

	text := Interpret(hpa, 2)
	structured := buildStructuredInterpretation(hpa, 2)

	if len(text) != len(structured) {
		t.Fatalf("text (%d) and structured (%d) should have same length", len(text), len(structured))
	}
	for i, msg := range structured {
		if text[i] != msg.Message {
			t.Fatalf("entry %d: text=%q != structured.Message=%q", i, text[i], msg.Message)
		}
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
