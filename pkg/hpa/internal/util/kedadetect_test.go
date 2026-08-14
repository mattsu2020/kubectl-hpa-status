package util

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

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
