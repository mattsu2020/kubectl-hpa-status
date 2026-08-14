package util

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func TestKubectlPatchCommand(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: mapMeta("my-hpa", "ns1", nil)}
	got := KubectlPatchCommand(hpa, `{"spec":{"maxReplicas":5}}`)
	want := `kubectl patch hpa my-hpa -n ns1 --type=merge -p '{"spec":{"maxReplicas":5}}' --dry-run=server`
	if got != want {
		t.Fatalf("KubectlPatchCommand =\n %q\nwant\n %q", got, want)
	}
}
