package keda

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Ported from the pkg/hpa facade test suite when the AnalyzeKEDA
// compatibility forwarder was removed in v3.
func TestAnalyze_Fallback(t *testing.T) {
	minReplicas := int32(1)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keda-hpa-worker",
			Namespace: "default",
			Labels:    map[string]string{"scaledobject.keda.sh/name": "worker"},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
		},
	}

	k := &Analysis{
		ScaledObjectName: "worker",
		Triggers:         []TriggerSummary{{Type: "cpu"}},
		Fallback: &FallbackInfo{
			FailureThreshold: 3,
			Replicas:         5,
		},
	}

	lines := Analyze(hpa, k)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"fallback configured", "failureThreshold=3", "replicas=5"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in output, got %v", want, lines)
		}
	}
}
