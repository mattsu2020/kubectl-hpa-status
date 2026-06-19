package hpa

import (
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// TestSummarizeDirectionWithKey_AllBranchesPopulateKey exhaustively exercises
// every branch of SummarizeDirectionWithKey and asserts each one returns a
// non-empty SummaryKey. This is the contract that lets cmd/ renderers
// translate Summary via the key without re-deriving the decision switch. If a
// new branch is added to SummarizeDirectionWithKey without populating the key,
// this test fails.
func TestSummarizeDirectionWithKey_AllBranchesPopulateKey(t *testing.T) {
	// The complete set of keys that SummarizeDirectionWithKey can emit in
	// practice. Note: "dir_at_min_scale_to_zero" is defined in the switch for
	// completeness but is effectively unreachable through SummarizeDirection's
	// outer guard branches (a desired==0 && minReplicas==0 state is caught
	// earlier as dir_scale_to_zero or dir_scaled_to_zero), so it is not in
	// this set. summarizeDirectionFromReplicas is exercised directly below to
	// cover that branch.
	wantKeys := map[string]bool{
		"dir_unavailable":       false,
		"dir_inactive":          false,
		"dir_scale_to_zero":     false,
		"dir_no_recommendation": false,
		"dir_scaled_to_zero":    false,
		"dir_scale_up":          false,
		"dir_scale_down":        false,
		"dir_at_max":            false,
		"dir_at_min":            false,
		"dir_unchanged":         false,
	}

	cases := []struct {
		name       string
		hpa        *autoscalingv2.HorizontalPodAutoscaler
		minRepl    int32
		wantKey    string
		wantNonNil bool // false = expect nil hpa branch
	}{
		{
			name:       "nil hpa -> unavailable",
			hpa:        nil,
			wantKey:    "dir_unavailable",
			wantNonNil: false,
		},
		{
			name: "scaling active false -> inactive",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 3,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{Type: "ScalingActive", Status: corev1.ConditionFalse},
					},
				},
			},
			minRepl: 1,
			wantKey: "dir_inactive",
		},
		{
			name: "desired 0 current>0 minReplicas=0 -> scale_to_zero",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 3,
					DesiredReplicas: 0,
				},
			},
			minRepl: 0,
			wantKey: "dir_scale_to_zero",
		},
		{
			name: "desired 0 current>0 minReplicas>0 -> no_recommendation",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 3,
					DesiredReplicas: 0,
				},
			},
			minRepl: 1,
			wantKey: "dir_no_recommendation",
		},
		{
			name: "all zero minReplicas=0 -> scaled_to_zero",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 0,
					DesiredReplicas: 0,
				},
			},
			minRepl: 0,
			wantKey: "dir_scaled_to_zero",
		},
		{
			name: "desired>current -> scale_up",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 10},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 3,
					DesiredReplicas: 5,
				},
			},
			minRepl: 1,
			wantKey: "dir_scale_up",
		},
		{
			name: "desired<current -> scale_down",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 10},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 5,
					DesiredReplicas: 3,
				},
			},
			minRepl: 1,
			wantKey: "dir_scale_down",
		},
		{
			name: "desired==max -> at_max",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 10},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 10,
					DesiredReplicas: 10,
				},
			},
			minRepl: 1,
			wantKey: "dir_at_max",
		},
		{
			name: "desired==minReplicas==0 would be at_min_scale_to_zero, but unreachable via SummarizeDirection (covered below)",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 10},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 0,
					DesiredReplicas: 0,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{Type: "ScalingActive", Status: corev1.ConditionTrue},
					},
				},
			},
			minRepl: 0,
			// Outer guard catches this as dir_scaled_to_zero before the inner switch runs.
			wantKey: "dir_scaled_to_zero",
		},
		{
			name: "desired==minReplicas -> at_min",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 10},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 2,
					DesiredReplicas: 2,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{Type: "ScalingActive", Status: corev1.ConditionTrue},
					},
				},
			},
			minRepl: 2,
			wantKey: "dir_at_min",
		},
		{
			name: "steady state -> unchanged",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 10},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 5,
					DesiredReplicas: 5,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{Type: "ScalingActive", Status: corev1.ConditionTrue},
					},
				},
			},
			minRepl: 1,
			wantKey: "dir_unchanged",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary, key := SummarizeDirectionWithKey(tc.hpa, tc.minRepl)
			if key == "" {
				t.Fatalf("SummaryKey is empty; every branch must populate it. summary=%q", summary)
			}
			if summary == "" {
				t.Fatalf("Summary is empty for key=%q", key)
			}
			if key != tc.wantKey {
				t.Errorf("SummaryKey: got %q want %q (summary=%q)", key, tc.wantKey, summary)
			}
			if _, ok := wantKeys[key]; !ok {
				t.Errorf("SummaryKey %q is not in the expected key set; update wantKeys", key)
			}
			wantKeys[key] = true
		})
	}

	// Verify every expected key was actually produced by at least one case.
	for k, seen := range wantKeys {
		if !seen {
			t.Errorf("expected key %q was never produced by any test case; add a case covering that branch", k)
		}
	}

	// summarizeDirectionFromReplicas has one additional branch
	// (dir_at_min_scale_to_zero) that is unreachable through
	// SummarizeDirection's outer guards but still emits a key + message pair
	// that must stay locale-translatable. Exercise it directly so the branch
	// is covered and the contract holds if the outer guards ever change.
	t.Run("inner switch at_min_scale_to_zero branch", func(t *testing.T) {
		summary, key := summarizeDirectionFromReplicas(0, 0, 10, 0)
		if key != "dir_at_min_scale_to_zero" {
			t.Fatalf("SummaryKey: got %q want %q", key, "dir_at_min_scale_to_zero")
		}
		if summary != "HPA is at minReplicas (scale-to-zero enabled)." {
			t.Fatalf("Summary: got %q", summary)
		}
	})
}

// TestAnalyzePopulatesSummaryKey verifies that Analyze propagates SummaryKey
// into the Analysis struct alongside Summary, so cmd/ renderers can rely on
// the field instead of re-deriving the key from the English text.
func TestAnalyzePopulatesSummaryKey(t *testing.T) {
	hpa := testutil.BuildHPA("ns", "test",
		testutil.WithMinMax(1, 10),
		testutil.WithReplicas(5, 7),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)

	got := Analyze(hpa, false)
	if got.Summary != "HPA currently wants to scale up." {
		t.Fatalf("Summary: got %q", got.Summary)
	}
	if got.SummaryKey != "dir_scale_up" {
		t.Errorf("SummaryKey: got %q want %q", got.SummaryKey, "dir_scale_up")
	}
}
