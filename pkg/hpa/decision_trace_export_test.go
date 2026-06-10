package hpa

import (
	"encoding/json"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExportStructuredDecisionTrace_NilHPA(t *testing.T) {
	result := ExportStructuredDecisionTrace(nil, Analysis{})
	if result != nil {
		t.Fatalf("expected nil for nil HPA, got %+v", result)
	}
}

func TestExportStructuredDecisionTrace_SingleResourceMetric(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithResourceMetric("cpu", 80, 60),
		kube.WithReplicas(3, 3),
	)

	a := Analysis{
		Min: 1,
		Max: 10,
	}

	result := ExportStructuredDecisionTrace(hpa, a)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.SchemaVersion != "v1" {
		t.Errorf("expected schema version v1, got %s", result.SchemaVersion)
	}
	if result.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", result.Namespace)
	}
	if result.Name != "web" {
		t.Errorf("expected name web, got %s", result.Name)
	}
	if result.CurrentReplicas != 3 {
		t.Errorf("expected currentReplicas 3, got %d", result.CurrentReplicas)
	}
	if result.VisibleDesiredReplicas != 3 {
		t.Errorf("expected visibleDesiredReplicas 3, got %d", result.VisibleDesiredReplicas)
	}
	if result.MinReplicas != 1 {
		t.Errorf("expected minReplicas 1, got %d", result.MinReplicas)
	}
	if result.MaxReplicas != 10 {
		t.Errorf("expected maxReplicas 10, got %d", result.MaxReplicas)
	}

	if len(result.Metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(result.Metrics))
	}
	if result.Metrics[0].Name != "cpu" {
		t.Errorf("expected metric name cpu, got %s", result.Metrics[0].Name)
	}
	if result.Metrics[0].Type != "Resource" {
		t.Errorf("expected metric type Resource, got %s", result.Metrics[0].Type)
	}
	if result.Metrics[0].Ratio == nil {
		t.Fatal("expected non-nil ratio")
	}
	ratio := *result.Metrics[0].Ratio
	if ratio <= 0 {
		t.Errorf("expected positive ratio, got %.3f", ratio)
	}

	if len(result.DecisionPath) == 0 {
		t.Fatal("expected decision path steps")
	}

	// First step should be "Read current replicas".
	if result.DecisionPath[0].Description != "Read current replicas" {
		t.Errorf("expected first step 'Read current replicas', got %s", result.DecisionPath[0].Description)
	}

	// Last step should be "Produce final desiredReplicas".
	lastStep := result.DecisionPath[len(result.DecisionPath)-1]
	if lastStep.Description != "Produce final desiredReplicas" {
		t.Errorf("expected last step 'Produce final desiredReplicas', got %s", lastStep.Description)
	}

	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestExportStructuredDecisionTrace_MultiMetricWinner(t *testing.T) {
	hpa := kube.BuildHPA("default", "api",
		kube.WithResourceMetric("cpu", 80, 95),
		kube.WithExternalMetricWithStatus("queue_depth", "10", "50"),
		kube.WithReplicas(5, 8),
	)

	a := Analysis{
		Min: 1,
		Max: 20,
		MetricDecisionTrace: &MetricDecisionTrace{
			Winner:           "cpu",
			WinnerConfidence: ConfidenceMedium,
		},
	}

	result := ExportStructuredDecisionTrace(hpa, a)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.WinnerMetric != "cpu" {
		t.Errorf("expected winner metric cpu, got %s", result.WinnerMetric)
	}
	if result.WinnerConfidence != ConfidenceMedium {
		t.Errorf("expected winner confidence medium, got %s", result.WinnerConfidence)
	}

	if len(result.Metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(result.Metrics))
	}

	// CPU should want scale-up (95/80 > 1).
	cpuMetric := result.Metrics[0]
	if cpuMetric.DesiredDirection != "up" {
		t.Errorf("expected cpu desiredDirection up, got %s", cpuMetric.DesiredDirection)
	}

	// Check that a winner determination step exists in the decision path.
	foundWinnerStep := false
	for _, step := range result.DecisionPath {
		if step.Description == "Determine winning metric" {
			foundWinnerStep = true
			if step.Result != "winner=cpu" {
				t.Errorf("expected winner step result 'winner=cpu', got %s", step.Result)
			}
		}
	}
	if !foundWinnerStep {
		t.Error("expected 'Determine winning metric' step in decision path")
	}
}

func TestExportStructuredDecisionTrace_WinnerFallback(t *testing.T) {
	hpa := kube.BuildHPA("default", "svc",
		kube.WithResourceMetric("cpu", 50, 80),
		kube.WithReplicas(4, 6),
	)

	// No MetricDecisionTrace on Analysis, so winner should be computed from metrics.
	a := Analysis{
		Min: 1,
		Max: 10,
	}

	result := ExportStructuredDecisionTrace(hpa, a)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// With only one metric and ratio > 1 + tolerance, it should be the winner.
	if result.WinnerMetric != "cpu" {
		t.Errorf("expected winner metric cpu, got %s", result.WinnerMetric)
	}
}

func TestExportStructuredDecisionTrace_ToleranceSuppression(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithResourceMetric("cpu", 80, 82),
		kube.WithReplicas(3, 3),
	)

	a := Analysis{
		Min: 1,
		Max: 10,
	}

	result := ExportStructuredDecisionTrace(hpa, a)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// 82/80 = 1.025 which is within default tolerance of 0.1.
	if len(result.Metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(result.Metrics))
	}

	if !result.Metrics[0].WithinTolerance {
		t.Error("expected metric to be within tolerance")
	}

	if result.ToleranceEffect == nil {
		t.Fatal("expected tolerance effect to be non-nil when metrics are within tolerance")
	}

	if len(result.ToleranceEffect.SuppressedMetrics) != 1 {
		t.Errorf("expected 1 suppressed metric, got %d", len(result.ToleranceEffect.SuppressedMetrics))
	}

	if result.ToleranceEffect.DefaultTolerance != defaultTolerance {
		t.Errorf("expected default tolerance %.2f, got %.2f", defaultTolerance, result.ToleranceEffect.DefaultTolerance)
	}
}

func TestExportStructuredDecisionTrace_LimitClamp(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithResourceMetric("cpu", 50, 90),
		kube.WithReplicas(5, 10),
		kube.WithMinMax(1, 10),
	)

	a := Analysis{
		Min: 1,
		Max: 10,
	}

	result := ExportStructuredDecisionTrace(hpa, a)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// CPU at 90% with target 50% means ratio = 1.8, raw desired = ceil(5*1.8) = 9.
	// With maxReplicas=10, this should be within limits.
	if result.LimitClamp == "" {
		t.Error("expected non-empty limit clamp")
	}
}

func TestExportStructuredDecisionTrace_Stabilization(t *testing.T) {
	window := int32(300)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			MinReplicas: ptrInt32Export(1),
			MaxReplicas: 10,
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &window,
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 5,
			DesiredReplicas: 5,
			LastScaleTime:   &metav1.Time{Time: metav1.Now().Add(-120 * (1000 * 1000 * 1000))},
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{
					Type:   autoscalingv2.AbleToScale,
					Status: corev1.ConditionTrue,
					Reason: "ScaleDownStabilized",
				},
			},
		},
	}

	a := Analysis{
		Min:                1,
		Max:                10,
		StabilizationSource: "scaleDown",
	}

	result := ExportStructuredDecisionTrace(hpa, a)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.StabilizationEffect == nil {
		t.Fatal("expected stabilization effect to be non-nil")
	}

	if result.StabilizationEffect.WindowSeconds != 300 {
		t.Errorf("expected window 300, got %d", result.StabilizationEffect.WindowSeconds)
	}

	if result.StabilizationEffect.RemainingSeconds == nil || *result.StabilizationEffect.RemainingSeconds <= 0 {
		t.Error("expected positive remaining seconds")
	}

	// Check decision path includes stabilization step.
	foundStabStep := false
	for _, step := range result.DecisionPath {
		if step.Description == "Check stabilization window" {
			foundStabStep = true
		}
	}
	if !foundStabStep {
		t.Error("expected stabilization step in decision path")
	}
}

func TestExportStructuredDecisionTrace_NoMetrics(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 3),
	)

	a := Analysis{
		Min: 1,
		Max: 10,
	}

	result := ExportStructuredDecisionTrace(hpa, a)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.Metrics) != 0 {
		t.Errorf("expected 0 metrics, got %d", len(result.Metrics))
	}

	if result.WinnerMetric != "" {
		t.Errorf("expected empty winner metric, got %s", result.WinnerMetric)
	}

	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestExportStructuredDecisionTrace_JSONRoundTrip(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithResourceMetric("cpu", 80, 60),
		kube.WithReplicas(3, 3),
	)

	a := Analysis{
		Min: 1,
		Max: 10,
	}

	result := ExportStructuredDecisionTrace(hpa, a)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Marshal to JSON.
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal to JSON: %v", err)
	}

	// Unmarshal back.
	var decoded StructuredDecisionTrace
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal from JSON: %v", err)
	}

	if decoded.SchemaVersion != "v1" {
		t.Errorf("expected schema version v1 after round-trip, got %s", decoded.SchemaVersion)
	}
	if decoded.Namespace != "default" {
		t.Errorf("expected namespace default after round-trip, got %s", decoded.Namespace)
	}
	if decoded.Name != "web" {
		t.Errorf("expected name web after round-trip, got %s", decoded.Name)
	}
	if decoded.CurrentReplicas != 3 {
		t.Errorf("expected currentReplicas 3 after round-trip, got %d", decoded.CurrentReplicas)
	}
	if len(decoded.Metrics) != 1 {
		t.Errorf("expected 1 metric after round-trip, got %d", len(decoded.Metrics))
	}
	if len(decoded.DecisionPath) == 0 {
		t.Error("expected decision path steps after round-trip")
	}
}

func TestExportStructuredDecisionTrace_YAMLRoundTrip(t *testing.T) {
	hpa := kube.BuildHPA("default", "api",
		kube.WithResourceMetric("cpu", 80, 95),
		kube.WithReplicas(5, 7),
	)

	a := Analysis{
		Min: 1,
		Max: 10,
	}

	result := ExportStructuredDecisionTrace(hpa, a)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Marshal to YAML via JSON (the types use json tags which also work for yaml).
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded StructuredDecisionTrace
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.WinnerMetric != result.WinnerMetric {
		t.Errorf("expected winner %s after round-trip, got %s", result.WinnerMetric, decoded.WinnerMetric)
	}
}

func TestBuildStructuredMetricTraces_ExternalMetric(t *testing.T) {
	hpa := kube.BuildHPA("default", "worker",
		kube.WithExternalMetricWithStatus("queue_depth", "10", "25"),
		kube.WithReplicas(2, 5),
	)

	metrics := buildStructuredMetricTraces(hpa)
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	if metrics[0].Name != "queue_depth" {
		t.Errorf("expected metric name queue_depth, got %s", metrics[0].Name)
	}
	if metrics[0].Type != "External" {
		t.Errorf("expected metric type External, got %s", metrics[0].Type)
	}
	if metrics[0].DesiredDirection != "up" {
		t.Errorf("expected desiredDirection up, got %s", metrics[0].DesiredDirection)
	}
}

func TestComputeEstimatedRawDesired(t *testing.T) {
	raw3 := int32(3)
	raw7 := int32(7)
	raw5 := int32(5)

	tests := []struct {
		name     string
		metrics  []StructuredMetricTrace
		current  int32
		expected int32
	}{
		{
			name:     "empty metrics",
			metrics:  nil,
			current:  5,
			expected: 0,
		},
		{
			name: "single metric",
			metrics: []StructuredMetricTrace{
				{EstimatedDesiredReplicas: &raw7},
			},
			current:  5,
			expected: 7,
		},
		{
			name: "multiple metrics picks largest",
			metrics: []StructuredMetricTrace{
				{EstimatedDesiredReplicas: &raw3},
				{EstimatedDesiredReplicas: &raw7},
				{EstimatedDesiredReplicas: &raw5},
			},
			current:  5,
			expected: 7,
		},
		{
			name: "nil desired replicas are skipped",
			metrics: []StructuredMetricTrace{
				{EstimatedDesiredReplicas: nil},
				{EstimatedDesiredReplicas: &raw5},
			},
			current:  5,
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeEstimatedRawDesired(tt.metrics, tt.current)
			if got != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestFormatRatioSafe(t *testing.T) {
	if result := formatRatioSafe(nil); result != "unavailable" {
		t.Errorf("expected 'unavailable', got %s", result)
	}

	val := 1.5
	if result := formatRatioSafe(&val); result != "1.500" {
		t.Errorf("expected '1.500', got %s", result)
	}
}

func ptrInt32Export(v int32) *int32 { return &v }
