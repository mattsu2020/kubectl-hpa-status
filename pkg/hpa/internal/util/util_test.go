package util

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mapMeta is a tiny test helper to build an ObjectMeta with name/namespace/labels.
func mapMeta(name, namespace string, labels map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}
}

func TestLooksLikeKEDAManaged_NilHPA(t *testing.T) {
	if LooksLikeKEDAManaged(nil) {
		t.Fatalf("LooksLikeKEDAManaged(nil) = true, want false")
	}
}

func TestLooksLikeKEDAManaged(t *testing.T) {
	cases := []struct {
		name string
		hpa  *autoscalingv2.HorizontalPodAutoscaler
		want bool
	}{
		{
			name: "no signals",
			hpa:  &autoscalingv2.HorizontalPodAutoscaler{},
			want: false,
		},
		{
			name: "keda.sh annotation key",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: mapMeta("", "", map[string]string{"scaledobjects.keda.sh/name": "x"}),
			},
			want: true,
		},
		{
			name: "managed-by=keda label",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: mapMeta("", "", map[string]string{"app.kubernetes.io/managed-by": "keda"}),
			},
			want: true,
		},
		{
			name: "managed-by=KEDA case-insensitive",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: mapMeta("", "", map[string]string{"app.kubernetes.io/managed-by": "KEDA"}),
			},
			want: true,
		},
		{
			name: "keda-hpa- name prefix",
			hpa:  &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: mapMeta("keda-hpa-foo", "", nil)},
			want: true,
		},
		{
			name: "value containing keda fallback",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: mapMeta("", "", map[string]string{"note": "managed by Keda project"}),
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LooksLikeKEDAManaged(tc.hpa); got != tc.want {
				t.Fatalf("LooksLikeKEDAManaged = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasKEDAKeySignal(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]string
		want bool
	}{
		{"nil", nil, false},
		{"empty", map[string]string{}, false},
		{"unrelated", map[string]string{"app": "web"}, false},
		{"keda.sh prefix", map[string]string{"scaledobjects.keda.sh/name": "x"}, true},
		{"managed-by keda", map[string]string{"app.kubernetes.io/managed-by": "keda"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasKEDAKeySignal(tc.m); got != tc.want {
				t.Fatalf("HasKEDAKeySignal = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasKEDAValueFallback(t *testing.T) {
	if HasKEDAValueFallback(map[string]string{"note": "Keda"}) != true {
		t.Fatalf("expected true for value containing keda")
	}
	if HasKEDAValueFallback(map[string]string{"note": "argo"}) != false {
		t.Fatalf("expected false for value without keda")
	}
	if HasKEDAValueFallback(nil) != false {
		t.Fatalf("expected false for nil map")
	}
}

func TestMarshalJSON(t *testing.T) {
	t.Run("valid value", func(t *testing.T) {
		if got := MarshalJSON(map[string]any{"a": 1}); got != `{"a":1}` {
			t.Fatalf("MarshalJSON = %q, want {\"a\":1}", got)
		}
	})
	t.Run("invalid value returns empty object", func(t *testing.T) {
		// Channels cannot be marshalled to JSON.
		if got := MarshalJSON(make(chan int)); got != "{}" {
			t.Fatalf("MarshalJSON(chan) = %q, want {}", got)
		}
	})
}

func TestKubectlPatchCommand(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: mapMeta("my-hpa", "ns1", nil)}
	got := KubectlPatchCommand(hpa, `{"spec":{"maxReplicas":5}}`)
	want := `kubectl patch hpa my-hpa -n ns1 --type=merge -p '{"spec":{"maxReplicas":5}}' --dry-run=server`
	if got != want {
		t.Fatalf("KubectlPatchCommand =\n %q\nwant\n %q", got, want)
	}
}

func TestMissingPolicies(t *testing.T) {
	rules := func() *autoscalingv2.HPAScalingRules {
		return &autoscalingv2.HPAScalingRules{Policies: []autoscalingv2.HPAScalingPolicy{{Type: autoscalingv2.PodsScalingPolicy}}}
	}

	cases := []struct {
		name     string
		behavior *autoscalingv2.HorizontalPodAutoscalerBehavior
		dir      string
		want     bool
	}{
		{"nil behavior", nil, "scaleUp", true},
		{"unknown direction", &autoscalingv2.HorizontalPodAutoscalerBehavior{}, "sideways", true},
		{"scaleUp nil rules", &autoscalingv2.HorizontalPodAutoscalerBehavior{}, "scaleUp", true},
		{
			name:     "scaleUp has policies",
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{ScaleUp: rules()},
			dir:      "scaleUp",
			want:     false,
		},
		{
			name:     "scaleDown empty policies",
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{ScaleDown: &autoscalingv2.HPAScalingRules{}},
			dir:      "scaleDown",
			want:     true,
		},
		{
			name:     "scaleDown has policies",
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{ScaleDown: rules()},
			dir:      "scaleDown",
			want:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MissingPolicies(tc.behavior, tc.dir); got != tc.want {
				t.Fatalf("MissingPolicies = %v, want %v", got, tc.want)
			}
		})
	}
}
