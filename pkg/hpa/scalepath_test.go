package hpa

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func TestAnalyzeScalePathSchedulerBlocker(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 12,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 8,
			DesiredReplicas: 12,
		},
	}
	path := AnalyzeScalePath(hpa, ScalePathInput{
		Target: &ScalePathTarget{
			Kind:            "Deployment",
			Name:            "web",
			DesiredReplicas: 12,
			CurrentReplicas: 12,
			ReadyReplicas:   8,
		},
		ReplicaSets: []ScalePathReplicaSet{{
			Name:            "web-abc",
			DesiredReplicas: 12,
			CurrentReplicas: 12,
			ReadyReplicas:   8,
		}},
		Pods: []ScalePathPod{
			{Name: "web-1", Phase: "Running", Ready: true},
			{Name: "web-2", Phase: "Running", Ready: true},
			{Name: "web-3", Phase: "Pending", Unschedulable: true, Reasons: []string{"0/5 nodes available: insufficient cpu"}},
			{Name: "web-4", Phase: "Pending", Unschedulable: true},
		},
		Events: []Event{{
			Reason:  "FailedScheduling",
			Message: "0/5 nodes available: insufficient cpu",
		}},
	})

	if path == nil {
		t.Fatal("expected scale path")
	}
	if path.BlockingPoint != "Scheduler cannot place 2 pods" {
		t.Fatalf("unexpected blocker: %q", path.BlockingPoint)
	}
	if !containsScalePathLine(path.Evidence, "maxReplicas is not the current blocker") {
		t.Fatalf("expected maxReplicas evidence, got %#v", path.Evidence)
	}
	if !containsSubstring(path.NextActions, "Cluster Autoscaler/Karpenter") {
		t.Fatalf("expected autoscaler next action, got %#v", path.NextActions)
	}
}

func TestWriteScalePathText(t *testing.T) {
	path := &ScalePath{
		Steps: []ScalePathStep{
			{Name: "HPA", Summary: "wants 3 replicas"},
			{Name: "Pods", Summary: "2 Ready / 3 desired"},
		},
		BlockingPoint: "Pods are created but only 2 of 3 are Ready",
		Evidence:      []string{"1 pods are not Ready"},
		NextActions:   []string{"Check readiness probes"},
	}
	var b strings.Builder
	if err := WriteScalePathText(&b, path); err != nil {
		t.Fatalf("WriteScalePathText returned error: %v", err)
	}
	out := b.String()
	for _, want := range []string{"Scale Path:", "Blocking point:", "Evidence:", "Next actions:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func containsScalePathLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func containsSubstring(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}
