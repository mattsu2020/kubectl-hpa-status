package vpa

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func hpaWithResourceMetric(resource corev1.ResourceName) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "hpa1", Namespace: "ns1"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: resource,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: ptr(int32(80)),
					},
				},
			}},
		},
	}
}

func hpaWithContainerResourceMetric(resource corev1.ResourceName) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "hpa1", Namespace: "ns1"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ContainerResourceMetricSourceType,
				ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
					Name:      resource,
					Container: "app",
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: ptr(int32(70)),
					},
				},
			}},
		},
	}
}

func hpaWithExternalMetricOnly() *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricSource{
					Metric: autoscalingv2.MetricIdentifier{Name: "requests-per-second"},
					Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType},
				},
			}},
		},
	}
}

func ptr[T any](v T) *T { return &v }

func TestAnalyze_Branches(t *testing.T) {
	t.Run("nil guards", func(t *testing.T) {
		if got := Analyze(nil, &Info{}); got != nil {
			t.Fatalf("Analyze(nil, info) = %v, want nil", got)
		}
		if got := Analyze(&autoscalingv2.HorizontalPodAutoscaler{}, nil); got != nil {
			t.Fatalf("Analyze(hpa, nil) = %v, want nil", got)
		}
	})
	t.Run("Off mode returns nil", func(t *testing.T) {
		hpa := hpaWithResourceMetric(corev1.ResourceCPU)
		v := &Info{Name: "v1", UpdateMode: "Off"}
		if got := Analyze(hpa, v); got != nil {
			t.Fatalf("Analyze Off mode = %v, want nil", got)
		}
	})
	t.Run("no resource metrics returns nil", func(t *testing.T) {
		v := &Info{Name: "v1", UpdateMode: "Auto"}
		if got := Analyze(hpaWithExternalMetricOnly(), v); got != nil {
			t.Fatalf("Analyze (external only) = %v, want nil", got)
		}
	})
	t.Run("Auto with CPU resource metric produces conflict lines", func(t *testing.T) {
		hpa := hpaWithResourceMetric(corev1.ResourceCPU)
		v := &Info{
			Name:       "v1",
			UpdateMode: "Auto",
			TargetKind: "Deployment",
			TargetName: "web",
			Recommendations: []RecommendationInfo{
				{Container: "app", Resource: "cpu", Target: "500m"},
				{Container: "app", Resource: "memory"}, // HPA doesn't scale on memory here
			},
		}
		got := Analyze(hpa, v)
		if len(got) < 3 {
			t.Fatalf("expected several conflict lines, got %d: %v", len(got), got)
		}
		joined := strings.Join(got, "\n")
		if !strings.Contains(joined, "Auto") || !strings.Contains(joined, "evict") {
			t.Fatalf("missing Auto-evict warning:\n%s", joined)
		}
		// cpu recommendation should be reported, memory recommendation should be skipped.
		if !strings.Contains(joined, "app") || !strings.Contains(joined, "cpu") {
			t.Fatalf("missing cpu recommendation line:\n%s", joined)
		}
		// memory recommendation should be skipped: HPA only scales on cpu here.
		// The "[estimated]" line is the recommendation rendering; it should
		// mention cpu but not "memory target=" or "recommends memory".
		for _, line := range got {
			if strings.HasPrefix(line, "[estimated]") && strings.Contains(line, "memory") {
				t.Fatalf("memory recommendation should be skipped, got: %s", line)
			}
		}
	})
	t.Run("ContainerResource metric also detected", func(t *testing.T) {
		hpa := hpaWithContainerResourceMetric(corev1.ResourceMemory)
		v := &Info{Name: "v1", UpdateMode: "Auto", TargetKind: "Deployment", TargetName: "web"}
		got := Analyze(hpa, v)
		if len(got) == 0 {
			t.Fatalf("expected conflict lines for container resource metric")
		}
	})
	t.Run("Auto with empty recommendation Target renders unknown", func(t *testing.T) {
		hpa := hpaWithResourceMetric(corev1.ResourceCPU)
		v := &Info{
			Name:       "v1",
			UpdateMode: "Initial",
			TargetKind: "Deployment",
			TargetName: "web",
			Recommendations: []RecommendationInfo{
				{Container: "app", Resource: "cpu"}, // empty Target
			},
		}
		got := Analyze(hpa, v)
		joined := strings.Join(got, "\n")
		if !strings.Contains(joined, "<unknown>") {
			t.Fatalf("expected <unknown> for empty Target:\n%s", joined)
		}
	})
}

func TestNewConflictInfo(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		if got := NewConflictInfo(nil); got != nil {
			t.Fatalf("NewConflictInfo(nil) = %v, want nil", got)
		}
	})
	t.Run("maps fields and copies slices", func(t *testing.T) {
		v := &Info{
			Name:                "v1",
			TargetKind:          "Deployment",
			TargetName:          "web",
			UpdateMode:          "Auto",
			ControlledResources: []string{"cpu", "memory"},
			Recommendations:     []RecommendationInfo{{Container: "app", Resource: "cpu", Target: "500m"}},
		}
		got := NewConflictInfo(v)
		if got == nil || got.VPAName != "v1" || got.UpdateMode != "Auto" {
			t.Fatalf("unexpected mapping: %+v", got)
		}
		if len(got.ControlledResources) != 2 {
			t.Fatalf("controlled resources: %+v", got.ControlledResources)
		}
		// Mutating the output must not affect the input.
		got.ControlledResources[0] = "mutated"
		if v.ControlledResources[0] != "cpu" {
			t.Fatalf("input ControlledResources mutated: %v", v.ControlledResources)
		}
		if got.Warning == "" || !strings.Contains(got.Warning, "VPA v1") {
			t.Fatalf("Warning not formatted: %q", got.Warning)
		}
	})
}

func TestAnalyzeAdvisory_AllLevels(t *testing.T) {
	t.Run("nil guards", func(t *testing.T) {
		if got := AnalyzeAdvisory(nil, &ConflictInfo{}); got != nil {
			t.Fatalf("AnalyzeAdvisory(nil, v) = %v, want nil", got)
		}
		if got := AnalyzeAdvisory(&autoscalingv2.HorizontalPodAutoscaler{}, nil); got != nil {
			t.Fatalf("AnalyzeAdvisory(hpa, nil) = %v, want nil", got)
		}
	})

	t.Run("Off mode -> NONE", func(t *testing.T) {
		hpa := hpaWithResourceMetric(corev1.ResourceCPU)
		v := &ConflictInfo{VPAName: "v1", UpdateMode: "Off", TargetKind: "Deployment", TargetName: "web"}
		got := AnalyzeAdvisory(hpa, v)
		if got.Level != ConflictNone {
			t.Fatalf("Level = %s, want NONE", got.Level)
		}
		if got.SafeCoexistence != true {
			t.Fatalf("SafeCoexistence = %v, want true", got.SafeCoexistence)
		}
		if !strings.Contains(got.Explanation, "Off") {
			t.Fatalf("expected Off explanation: %s", got.Explanation)
		}
	})

	t.Run("Recommender mode -> NONE", func(t *testing.T) {
		hpa := hpaWithResourceMetric(corev1.ResourceCPU)
		v := &ConflictInfo{VPAName: "v1", UpdateMode: "Recommender", ControlledResources: []string{"cpu"}}
		got := AnalyzeAdvisory(hpa, v)
		if got.Level != ConflictNone {
			t.Fatalf("Level = %s, want NONE", got.Level)
		}
	})

	t.Run("Initial mode + CPU -> WARNING", func(t *testing.T) {
		hpa := hpaWithResourceMetric(corev1.ResourceCPU)
		v := &ConflictInfo{VPAName: "v1", UpdateMode: "Initial", ControlledResources: []string{"cpu"}}
		got := AnalyzeAdvisory(hpa, v)
		if got.Level != ConflictWarning {
			t.Fatalf("Level = %s, want WARNING", got.Level)
		}
		if got.RecommendedMode != "Off" {
			t.Fatalf("RecommendedMode = %q, want Off", got.RecommendedMode)
		}
		if got.VPAPatch != "" {
			t.Fatalf("WARNING should not produce a patch, got %q", got.VPAPatch)
		}
		if len(got.VPAActions) == 0 {
			t.Fatalf("expected VPAActions for WARNING")
		}
	})

	t.Run("Auto mode + CPU -> ERROR with patch", func(t *testing.T) {
		hpa := hpaWithResourceMetric(corev1.ResourceCPU)
		v := &ConflictInfo{VPAName: "v1", UpdateMode: "Auto", ControlledResources: []string{"cpu", "memory"}, TargetKind: "Deployment", TargetName: "web"}
		got := AnalyzeAdvisory(hpa, v)
		if got.Level != ConflictError {
			t.Fatalf("Level = %s, want ERROR", got.Level)
		}
		if got.SafeCoexistence {
			t.Fatalf("SafeCoexistence should be false for ERROR")
		}
		if got.VPAPatch != `{"spec":{"updatePolicy":{"updateMode":"Initial"}}}` {
			t.Fatalf("VPAPatch = %q", got.VPAPatch)
		}
		// Only cpu is a conflict (HPA scales on cpu, not memory here).
		if len(got.ConflictResources) != 1 || got.ConflictResources[0] != "cpu" {
			t.Fatalf("ConflictResources = %v, want [cpu]", got.ConflictResources)
		}
		if len(got.HPAActions) < 2 {
			t.Fatalf("expected multiple HPAActions, got %v", got.HPAActions)
		}
	})

	t.Run("Active mode but no resource metrics -> NONE", func(t *testing.T) {
		v := &ConflictInfo{VPAName: "v1", UpdateMode: "Auto", ControlledResources: []string{"cpu"}}
		got := AnalyzeAdvisory(hpaWithExternalMetricOnly(), v)
		if got.Level != ConflictNone {
			t.Fatalf("Level = %s, want NONE (no resource metrics)", got.Level)
		}
	})

	t.Run("Unknown mode treated as ERROR", func(t *testing.T) {
		hpa := hpaWithResourceMetric(corev1.ResourceCPU)
		v := &ConflictInfo{VPAName: "v1", UpdateMode: "Weird"}
		got := AnalyzeAdvisory(hpa, v)
		if got.Level != ConflictError {
			t.Fatalf("Level = %s, want ERROR for unknown active mode", got.Level)
		}
	})
}
