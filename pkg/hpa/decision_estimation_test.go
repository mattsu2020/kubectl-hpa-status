package hpa

import (
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEstimateDecisionSignals(t *testing.T) {
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		wantReasons  []string
		wantMinCount int
	}{
		{
			name: "nil HPA returns nil",
			hpa:  nil,
		},
		{
			name: "minimal HPA returns no signals",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						Kind: "Deployment", Name: "app",
					},
					MaxReplicas: 10,
				},
			},
			wantMinCount: 0,
		},
		{
			name: "stabilization active produces signal",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						Kind: "Deployment", Name: "app",
					},
					MaxReplicas: 10,
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleDown: &autoscalingv2.HPAScalingRules{
							StabilizationWindowSeconds: ptrInt32Test(300),
						},
					},
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 5,
					DesiredReplicas: 5,
					LastScaleTime:   &metav1.Time{Time: metav1.Now().Add(-120 * time.Second)},
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   autoscalingv2.AbleToScale,
							Status: "True",
							Reason: "ScaleDownStabilized",
						},
					},
				},
			},
			wantReasons: []string{"ScaleDownStabilized"},
		},
		{
			name: "ScalingActive false produces signal",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						Kind: "Deployment", Name: "app",
					},
					MaxReplicas: 10,
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 3,
					DesiredReplicas: 3,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:    autoscalingv2.ScalingActive,
							Status:  "False",
							Reason:  "FailedGetResourceMetric",
							Message: "unable to get metric",
						},
					},
				},
			},
			wantReasons: []string{"FailedGetResourceMetric"},
		},
		{
			name: "DesiredWithinTolerance produces signal",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						Kind: "Deployment", Name: "app",
					},
					MaxReplicas: 10,
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 5,
					DesiredReplicas: 5,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   autoscalingv2.AbleToScale,
							Status: "True",
							Reason: "DesiredWithinTolerance",
						},
					},
				},
			},
			wantReasons: []string{"DesiredWithinTolerance"},
		},
		{
			name: "ScalingLimited produces signal",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						Kind: "Deployment", Name: "app",
					},
					MaxReplicas: 10,
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 10,
					DesiredReplicas: 10,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:    autoscalingv2.ScalingLimited,
							Status:  "True",
							Reason:  "TooManyReplicas",
							Message: "desired replicas max is 10",
						},
					},
				},
			},
			wantReasons: []string{"ScalingLimited"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signals := EstimateDecisionSignals(tt.hpa)

			if tt.hpa == nil {
				if signals != nil {
					t.Errorf("EstimateDecisionSignals(nil) = %v, want nil", signals)
				}
				return
			}

			if tt.wantMinCount > 0 && len(signals) < tt.wantMinCount {
				t.Errorf("got %d signals, want at least %d", len(signals), tt.wantMinCount)
			}

			if len(tt.wantReasons) > 0 {
				reasons := make([]string, 0, len(signals))
				for _, s := range signals {
					reasons = append(reasons, s.Reason)
				}
				for _, want := range tt.wantReasons {
					found := false
					for _, got := range reasons {
						if strings.Contains(got, want) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("reason %q not found in %v", want, reasons)
					}
				}
			}

			// All signals should have adapter version.
			for _, s := range signals {
				if s.AdapterVersion != "estimation-v1" {
					t.Errorf("AdapterVersion = %q, want %q", s.AdapterVersion, "estimation-v1")
				}
			}
		})
	}
}

func TestBuildStabilizationDecisionSignal(t *testing.T) {
	t.Run("no stabilization returns nil", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}
		sig := buildStabilizationDecisionSignal(hpa)
		if sig != nil {
			t.Error("expected nil for non-stabilized HPA")
		}
	})
}

func TestBuildToleranceDecisionSignal(t *testing.T) {
	t.Run("no AbleToScale condition returns nil", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}
		sig := buildToleranceDecisionSignal(hpa)
		if sig != nil {
			t.Error("expected nil for HPA without conditions")
		}
	})

	t.Run("DesiredWithinTolerance returns signal", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Status: autoscalingv2.HorizontalPodAutoscalerStatus{
				Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
					{
						Type:    autoscalingv2.AbleToScale,
						Status:  "True",
						Reason:  "DesiredWithinTolerance",
						Message: "desired within tolerance",
					},
				},
			},
		}
		sig := buildToleranceDecisionSignal(hpa)
		if sig == nil {
			t.Fatal("expected signal, got nil")
		}
		if sig.Reason != "DesiredWithinTolerance" {
			t.Errorf("Reason = %q, want %q", sig.Reason, "DesiredWithinTolerance")
		}
		if sig.Confidence != string(ConfidenceHigh) {
			t.Errorf("Confidence = %q, want %q", sig.Confidence, ConfidenceHigh)
		}
	})
}

func TestBuildConditionDecisionSignals(t *testing.T) {
	t.Run("empty conditions returns nil", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}
		signals := buildConditionDecisionSignals(hpa)
		if len(signals) != 0 {
			t.Errorf("expected 0 signals, got %d", len(signals))
		}
	})
}

// TestEstimateDecisionSignalsSetsClassification verifies every signal
// produced by EstimateDecisionSignals carries a Classification derived from
// its Confidence, so structured output can render a consistent [observed]/
// [estimated]/[unknown] evidence label. This is the C-9 guarantee: a user can
// tell at a glance whether a decision signal is read from the API, inferred,
// or assumed.
func TestEstimateDecisionSignalsSetsClassification(t *testing.T) {
	// Use an AbleToScale/DesiredWithinTolerance condition so the tolerance
	// signal is produced (ConfidenceHigh -> Classification observed).
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "ns"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "test"},
			MinReplicas:    ptrInt32Test(1),
			MaxReplicas:    10,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 3,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{
					Type:    autoscalingv2.AbleToScale,
					Status:  corev1.ConditionTrue,
					Reason:  "DesiredWithinTolerance",
					Message: "desired within tolerance",
				},
			},
		},
	}
	signals := EstimateDecisionSignals(hpa)
	if len(signals) == 0 {
		t.Fatal("expected at least one decision signal")
	}
	// classifyConfidence maps high→observed, medium→estimated, else→unknown.
	// Every signal must have a non-empty Classification consistent with the
	// confidence→classification map.
	for _, sig := range signals {
		if sig.Classification == "" {
			t.Errorf("signal %q has empty Classification; it should be derived from Confidence %q", sig.Reason, sig.Confidence)
		}
		want := classifyConfidence(sig.Confidence)
		if sig.Classification != want {
			t.Errorf("signal %q Classification = %q, want %q (from Confidence %q)", sig.Reason, sig.Classification, want, sig.Confidence)
		}
	}
}

// TestClassifyConfidence verifies the confidence→classification mapping that
// DecisionSignal.Classification is derived from.
func TestClassifyConfidence(t *testing.T) {
	cases := []struct {
		confidence string
		want       string
	}{
		{string(ConfidenceHigh), string(ClassificationObserved)},
		{string(ConfidenceMedium), string(ClassificationEstimated)},
		{string(ConfidenceLow), string(ClassificationUnknown)},
		{"", string(ClassificationUnknown)},
		{"garbage", string(ClassificationUnknown)},
	}
	for _, c := range cases {
		if got := classifyConfidence(c.confidence); got != c.want {
			t.Errorf("classifyConfidence(%q) = %q, want %q", c.confidence, got, c.want)
		}
	}
}

// Helper for tests.
func ptrInt32Test(v int32) *int32 { return &v }
