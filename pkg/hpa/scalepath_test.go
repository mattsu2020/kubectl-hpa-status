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

func TestAnalyzeScalePathWithProbeWarnings(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 5,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment", Name: "web",
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 5,
		},
	}
	path := AnalyzeScalePath(hpa, ScalePathInput{
		Target: &ScalePathTarget{
			Kind: "Deployment", Name: "web",
			DesiredReplicas: 5, CurrentReplicas: 5, ReadyReplicas: 3,
		},
		Pods: []ScalePathPod{
			{Name: "web-1", Phase: "Running", Ready: true},
			{Name: "web-2", Phase: "Running", Ready: true},
			{Name: "web-3", Phase: "Running", Ready: true},
			{Name: "web-4", Phase: "Running", Ready: false},
			{Name: "web-5", Phase: "Running", Ready: false},
		},
		PodTemplate: &ScalePathPodTemplate{
			ReadinessProbe: &ProbeInfo{
				InitialDelaySeconds: 60,
				PeriodSeconds:       10,
				FailureThreshold:    10,
			},
		},
	})

	if len(path.ProbeWarnings) == 0 {
		t.Fatal("expected probe warnings for slow readinessProbe")
	}
	if !strings.Contains(path.ProbeWarnings[0], "readinessProbe") {
		t.Fatalf("expected readinessProbe warning, got: %q", path.ProbeWarnings[0])
	}
}

func TestAnalyzeScalePathWithSchedulerConstraints(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment", Name: "web",
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 4,
			DesiredReplicas: 8,
		},
	}
	path := AnalyzeScalePath(hpa, ScalePathInput{
		Target: &ScalePathTarget{
			Kind: "Deployment", Name: "web",
			DesiredReplicas: 8, CurrentReplicas: 8, ReadyReplicas: 4,
		},
		Pods: []ScalePathPod{
			{Name: "web-1", Phase: "Pending", Unschedulable: true},
		},
		PodTemplate: &ScalePathPodTemplate{
			NodeSelector:    map[string]string{"tier": "frontend", "env": "prod"},
			AffinitySummary: "requiredDuringScheduling: zone=us-east-1a",
			TopologySpread:  []string{"zone maxSkew=1"},
		},
	})

	if path.SchedulerInfo == nil {
		t.Fatal("expected scheduler info")
	}
	if path.SchedulerInfo.NodeSelectorLabels != 2 {
		t.Fatalf("expected 2 nodeSelector labels, got %d", path.SchedulerInfo.NodeSelectorLabels)
	}
	if len(path.SchedulerInfo.AffinityConstraints) != 1 {
		t.Fatalf("expected 1 affinity constraint, got %d", len(path.SchedulerInfo.AffinityConstraints))
	}
	if len(path.SchedulerInfo.TopologySpreadConstraints) != 1 {
		t.Fatalf("expected 1 topology spread constraint, got %d", len(path.SchedulerInfo.TopologySpreadConstraints))
	}
}

func TestAnalyzeScalePathWithQuotaBlocking(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment", Name: "web",
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 4,
			DesiredReplicas: 6,
		},
	}
	path := AnalyzeScalePath(hpa, ScalePathInput{
		Target: &ScalePathTarget{
			Kind: "Deployment", Name: "web",
			DesiredReplicas: 6, CurrentReplicas: 6, ReadyReplicas: 4,
		},
		Pods: []ScalePathPod{
			{Name: "web-1", Phase: "Pending"},
		},
		ResourceQuotas: []ScalePathQuotaCheck{
			{Name: "compute-quota", Resource: "cpu", Used: "14", Hard: "16", Blocking: true},
			{Name: "mem-quota", Resource: "memory", Used: "28Gi", Hard: "32Gi", Blocking: false},
		},
	})

	if len(path.QuotaChecks) != 1 {
		t.Fatalf("expected 1 blocking quota, got %d", len(path.QuotaChecks))
	}
	if path.QuotaChecks[0].Name != "compute-quota" {
		t.Fatalf("expected compute-quota, got %q", path.QuotaChecks[0].Name)
	}
	if !containsSubstring(path.Evidence, "ResourceQuota") {
		t.Fatal("expected ResourceQuota evidence")
	}
}

func TestAnalyzeScalePathWithAutoscalerEvents(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment", Name: "web",
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 4,
			DesiredReplicas: 8,
		},
	}
	path := AnalyzeScalePath(hpa, ScalePathInput{
		Target: &ScalePathTarget{
			Kind: "Deployment", Name: "web",
			DesiredReplicas: 8, CurrentReplicas: 8, ReadyReplicas: 4,
		},
		Pods: []ScalePathPod{
			{Name: "web-1", Phase: "Pending"},
		},
		AutoscalerEvents: []string{
			"Cluster Autoscaler: scale up triggered for node group ng-1",
		},
	})

	if len(path.AutoscalerEvents) != 1 {
		t.Fatalf("expected 1 autoscaler event, got %d", len(path.AutoscalerEvents))
	}
	if !containsSubstring(path.NextActions, "Node provisioning") {
		t.Fatal("expected node provisioning next action")
	}
}

func TestAnalyzeScalePathWithNotReadyPods(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 5,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment", Name: "web",
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 5,
		},
	}
	path := AnalyzeScalePath(hpa, ScalePathInput{
		Target: &ScalePathTarget{
			Kind: "Deployment", Name: "web",
			DesiredReplicas: 5, CurrentReplicas: 5, ReadyReplicas: 3,
		},
		Pods: []ScalePathPod{
			{Name: "web-1", Phase: "Running", Ready: true},
			{Name: "web-2", Phase: "Running", Ready: true},
			{Name: "web-3", Phase: "Running", Ready: true},
			{Name: "web-4", Phase: "Running", Ready: false},
			{Name: "web-5", Phase: "Running", Ready: false},
		},
		NotReadyPods: []ScalePathPod{
			{Name: "web-4", Phase: "Running", Ready: false},
			{Name: "web-5", Phase: "Running", Ready: false},
		},
	})

	if !containsSubstring(path.Evidence, "not Ready") {
		t.Fatalf("expected not-Ready evidence, got: %#v", path.Evidence)
	}
}

func TestWriteScalePathTextWithNewSections(t *testing.T) {
	path := &ScalePath{
		Steps: []ScalePathStep{
			{Name: "HPA", Summary: "wants 5 replicas"},
		},
		ProbeWarnings: []string{"readinessProbe may delay by 160s"},
		SchedulerInfo: &ScalePathSchedulerInfo{
			NodeSelectorLabels: 2,
			Warning:            "Scheduling constraints may contribute",
		},
		QuotaChecks: []ScalePathQuotaCheck{
			{Name: "compute", Resource: "cpu", Used: "14", Hard: "16", Blocking: true},
		},
		AutoscalerEvents: []string{"scale up triggered"},
	}
	var b strings.Builder
	if err := WriteScalePathText(&b, path); err != nil {
		t.Fatalf("WriteScalePathText returned error: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		"Probe warnings:",
		"readinessProbe may delay by 160s",
		"Scheduler constraints:",
		"nodeSelector: 2 labels",
		"Quota checks:",
		"[BLOCKING] compute",
		"Autoscaler events:",
		"scale up triggered",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}
