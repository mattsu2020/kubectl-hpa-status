package keda

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// TestAnalyze_NilInputs confirms the nil guards on the canonical Analyze entry
// point (exercised through the pkg/hpa facade as AnalyzeKEDA in keda_test.go,
// but kept here too so the leaf package is independently testable).
func TestAnalyze_NilInputs(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if got := Analyze(hpa, nil); got != nil {
		t.Fatalf("Analyze(hpa, nil) = %v, want nil", got)
	}
	if got := Analyze(nil, &Analysis{}); got != nil {
		t.Fatalf("Analyze(nil, keda) = %v, want nil", got)
	}
}

func TestAnalyze_ReplicaBoundMismatch(t *testing.T) {
	minRepl := int32(2)
	maxRepl := int32(20)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minRepl,
			MaxReplicas: 10,
		},
	}
	k := &Analysis{
		MinReplicaCount: &minRepl,
		MaxReplicaCount: &maxRepl,
	}
	lines := Analyze(hpa, k)
	joined := strings.Join(lines, "\n")
	// Min matches so no min line; max differs so we expect the max mismatch line.
	if strings.Contains(joined, "minReplicaCount=2 differs") {
		t.Errorf("did not expect min mismatch line when values match:\n%s", joined)
	}
	if !strings.Contains(joined, "maxReplicaCount=20 differs from HPA maxReplicas=10") {
		t.Errorf("expected max mismatch line, got:\n%s", joined)
	}
}
