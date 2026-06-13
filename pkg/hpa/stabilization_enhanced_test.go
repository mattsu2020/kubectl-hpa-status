package hpa

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDetectStabilizationSource(t *testing.T) {
	tests := []struct {
		name string
		hpa  *autoscalingv2.HorizontalPodAutoscaler
		want string
	}{
		{
			name: "nil HPA returns scaleDown default",
			hpa:  nil,
			want: StabilizationSourceScaleDown,
		},
		{
			name: "ScaleDownStabilized reason returns scaleDown",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   autoscalingv2.AbleToScale,
							Reason: "ScaleDownStabilized",
						},
					},
				},
			},
			want: StabilizationSourceScaleDown,
		},
		{
			name: "scaleUp window configured returns scaleUp",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleUp: &autoscalingv2.HPAScalingRules{
							StabilizationWindowSeconds: ptrInt32ForTest(120),
						},
					},
				},
			},
			want: StabilizationSourceScaleUp,
		},
		{
			name: "no behavior returns scaleDown default",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			want: StabilizationSourceScaleDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectStabilizationSource(tt.hpa)
			if got != tt.want {
				t.Errorf("detectStabilizationSource() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatASCIIProgressBar(t *testing.T) {
	tests := []struct {
		name  string
		ratio float64
		width int
		check func(t *testing.T, got string)
	}{
		{
			name:  "zero ratio shows 0%",
			ratio: 0.0,
			width: 10,
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "0%") {
					t.Errorf("expected 0%% in %q", got)
				}
				if !strings.HasPrefix(got, "[") {
					t.Errorf("expected bracket prefix in %q", got)
				}
			},
		},
		{
			name:  "half ratio shows 50%",
			ratio: 0.5,
			width: 12,
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "50%") {
					t.Errorf("expected 50%% in %q", got)
				}
			},
		},
		{
			name:  "full ratio shows 100%",
			ratio: 1.0,
			width: 10,
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "100%") {
					t.Errorf("expected 100%% in %q", got)
				}
			},
		},
		{
			name:  "over-clamped ratio shows 100%",
			ratio: 1.5,
			width: 10,
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "100%") {
					t.Errorf("expected 100%% for clamped ratio in %q", got)
				}
			},
		},
		{
			name:  "negative ratio treated as 0%",
			ratio: -0.5,
			width: 10,
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "0%") {
					t.Errorf("expected 0%% for negative ratio in %q", got)
				}
			},
		},
		{
			name:  "zero width defaults to 24",
			ratio: 0.5,
			width: 0,
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "50%") {
					t.Errorf("expected 50%% with default width in %q", got)
				}
			},
		},
		{
			name:  "62% ratio shows correct percentage",
			ratio: 0.62,
			width: 24,
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "62%") {
					t.Errorf("expected 62%% in %q", got)
				}
				if !strings.Contains(got, "elapsed") {
					t.Errorf("expected 'elapsed' label in %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatASCIIProgressBar(tt.ratio, tt.width)
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestFormatStabilizationExplain(t *testing.T) {
	remaining := int64(187)
	window := int32(300)

	tests := []struct {
		name         string
		analysis     Analysis
		wantContains []string
		wantEmpty    bool
	}{
		{
			name: "full stabilization display",
			analysis: Analysis{
				StabilizationRemaining:     &remaining,
				StabilizationWindowSeconds: &window,
				StabilizationSource:        StabilizationSourceScaleDown,
				StabilizationConfidence:    stabilizationConfidenceLabel,
			},
			wantContains: []string{
				"ScalingDownStabilized: True",
				"38%",
				"scaleDown behavior",
				stabilizationConfidenceLabel,
				"Scale down will be enabled",
			},
		},
		{
			name: "nil remaining returns empty",
			analysis: Analysis{
				StabilizationRemaining: nil,
			},
			wantEmpty: true,
		},
		{
			name: "zero remaining returns empty",
			analysis: Analysis{
				StabilizationRemaining:     int64PtrVal(0),
				StabilizationWindowSeconds: &window,
			},
			wantEmpty: true,
		},
		{
			name: "without window still shows remaining",
			analysis: Analysis{
				StabilizationRemaining:     &remaining,
				StabilizationWindowSeconds: nil,
				StabilizationSource:        StabilizationSourceScaleUp,
				StabilizationConfidence:    stabilizationConfidenceLabel,
			},
			wantContains: []string{
				"ScalingDownStabilized: True",
				"scaleUp behavior",
				"Scale down will be enabled",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatStabilizationExplain(tt.analysis)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("FormatStabilizationExplain() = %q, want empty", got)
				}
				return
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatStabilizationExplain() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

func TestFormatStabilizationWithSource(t *testing.T) {
	remaining := int64(120)
	window := int32(300)

	tests := []struct {
		name         string
		remaining    *int64
		window       *int32
		source       string
		wantContains []string
		wantEmpty    bool
	}{
		{
			name:         "with source shows direction",
			remaining:    &remaining,
			window:       &window,
			source:       StabilizationSourceScaleDown,
			wantContains: []string{"scaleDown stabilization"},
		},
		{
			name:      "nil remaining returns empty",
			remaining: nil,
			window:    &window,
			source:    StabilizationSourceScaleDown,
			wantEmpty: true,
		},
		{
			name:         "empty source returns progress only",
			remaining:    &remaining,
			window:       &window,
			source:       "",
			wantContains: []string{"remaining (of"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatStabilizationWithSource(tt.remaining, tt.window, tt.source)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("FormatStabilizationWithSource() = %q, want empty", got)
				}
				return
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatStabilizationWithSource() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

// Helper for test readability.
func int64PtrVal(v int64) *int64     { return &v }
func ptrInt32ForTest(v int32) *int32 { return &v }
