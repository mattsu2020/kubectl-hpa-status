package hpa

import (
	"strings"
	"testing"
)

func TestAnalyzeAutoscalerMap_Healthy(t *testing.T) {
	input := AutoscalerMapInput{
		Namespace:              "production",
		HPAName:                "web",
		Target:                 "Deployment/web",
		CurrentReplicas:        5,
		DesiredReplicas:        5,
		MaxReplicas:            10,
		WorkloadReadyReplicas:  5,
		WorkloadDesiredReplicas: 5,
		PodSummary: AutoscalerMapPodSummary{
			Total:   5,
			Running: 5,
			Pending: 0,
			Ready:   5,
		},
		NodeSummary: AutoscalerMapNodeSummary{
			TotalNodes:     3,
			AllocatableCPU: "12",
			AllocatableMemory: "48Gi",
		},
		ClusterAutoscaler: true,
		ScalingActive:     true,
	}

	am := AnalyzeAutoscalerMap(input)

	if am == nil {
		t.Fatal("expected non-nil map")
	}
	if am.Namespace != "production" {
		t.Errorf("Namespace = %q, want %q", am.Namespace, "production")
	}
	if len(am.Layers) != 5 {
		t.Errorf("Layers = %d, want 5", len(am.Layers))
	}
	if len(am.Blockers) != 0 {
		t.Errorf("Blockers = %d, want 0 (healthy)", len(am.Blockers))
	}

	// All layers should be healthy.
	for _, layer := range am.Layers {
		if !layer.Healthy {
			t.Errorf("layer %q should be healthy", layer.Name)
		}
	}
}

func TestAnalyzeAutoscalerMap_PendingPods(t *testing.T) {
	input := AutoscalerMapInput{
		Namespace:              "production",
		HPAName:                "web",
		Target:                 "Deployment/web",
		CurrentReplicas:        8,
		DesiredReplicas:        10,
		MaxReplicas:            15,
		WorkloadReadyReplicas:  8,
		WorkloadDesiredReplicas: 10,
		PodSummary: AutoscalerMapPodSummary{
			Total:   10,
			Running: 8,
			Pending: 2,
			Ready:   8,
		},
		NodeSummary: AutoscalerMapNodeSummary{
			TotalNodes:     3,
			AllocatableCPU: "12",
		},
		PendingPods: []PendingPodInfo{
			{Name: "web-1", Phase: "Pending", Unschedulable: true, Reasons: []string{"Insufficient CPU"}},
			{Name: "web-2", Phase: "Pending", Unschedulable: true, Reasons: []string{"Insufficient memory"}},
		},
		ScalingActive: true,
	}

	am := AnalyzeAutoscalerMap(input)

	if len(am.Blockers) == 0 {
		t.Error("expected blockers for pending pods")
	}

	foundPodBlocker := false
	for _, b := range am.Blockers {
		if b.Layer == "pods" && b.Severity == "high" {
			foundPodBlocker = true
		}
	}
	if !foundPodBlocker {
		t.Error("expected high-severity blocker at pods layer")
	}
}

func TestAnalyzeAutoscalerMap_NoAutoscaler(t *testing.T) {
	input := AutoscalerMapInput{
		Namespace:              "production",
		HPAName:                "web",
		Target:                 "Deployment/web",
		CurrentReplicas:        5,
		DesiredReplicas:        5,
		MaxReplicas:            10,
		WorkloadReadyReplicas:  5,
		WorkloadDesiredReplicas: 5,
		PodSummary: AutoscalerMapPodSummary{
			Total:   5,
			Running: 5,
			Ready:   5,
		},
		NodeSummary: AutoscalerMapNodeSummary{
			TotalNodes: 3,
		},
		ClusterAutoscaler: false,
		Karpenter:         false,
		ScalingActive:     true,
	}

	am := AnalyzeAutoscalerMap(input)

	// Autoscaler layer should not be healthy.
	for _, layer := range am.Layers {
		if layer.Name == "autoscaler" && layer.Healthy {
			t.Error("autoscaler layer should not be healthy when no autoscaler is detected")
		}
	}
}

func TestAnalyzeAutoscalerMap_ScalingInactive(t *testing.T) {
	input := AutoscalerMapInput{
		Namespace:              "production",
		HPAName:                "web",
		Target:                 "Deployment/web",
		CurrentReplicas:        5,
		DesiredReplicas:        5,
		MaxReplicas:            10,
		WorkloadReadyReplicas:  5,
		WorkloadDesiredReplicas: 5,
		PodSummary: AutoscalerMapPodSummary{
			Total:   5,
			Running: 5,
			Ready:   5,
		},
		NodeSummary: AutoscalerMapNodeSummary{
			TotalNodes: 3,
		},
		ScalingActive: false,
	}

	am := AnalyzeAutoscalerMap(input)

	foundHPABlocker := false
	for _, b := range am.Blockers {
		if b.Layer == "hpa" && b.Severity == "high" {
			foundHPABlocker = true
		}
	}
	if !foundHPABlocker {
		t.Error("expected high-severity blocker at HPA layer when ScalingActive is false")
	}
}

func TestAnalyzeAutoscalerMap_LayerCount(t *testing.T) {
	input := AutoscalerMapInput{
		Namespace:  "default",
		HPAName:    "test",
		Target:     "Deployment/test",
		PodSummary: AutoscalerMapPodSummary{},
	}

	am := AnalyzeAutoscalerMap(input)

	if len(am.Layers) != 5 {
		t.Errorf("expected 5 layers (hpa, workload, pods, nodes, autoscaler), got %d", len(am.Layers))
	}

	expectedNames := []string{"hpa", "workload", "pods", "nodes", "autoscaler"}
	for i, name := range expectedNames {
		if am.Layers[i].Name != name {
			t.Errorf("layer[%d].Name = %q, want %q", i, am.Layers[i].Name, name)
		}
	}
}

func TestSummarizePendingPods(t *testing.T) {
	pods := []PendingPodInfo{
		{Name: "a", Unschedulable: true, Reasons: []string{"Insufficient CPU"}},
		{Name: "b", Unschedulable: true, Reasons: []string{"Insufficient CPU", "node(s) had taints"}},
		{Name: "c", Unschedulable: false, Reasons: nil},
	}

	reasons := summarizePendingPods(pods)
	if len(reasons) == 0 {
		t.Error("expected reasons for unschedulable pods")
	}

	if !containsStr(reasons, "Insufficient CPU") {
		t.Error("expected 'Insufficient CPU' in reasons")
	}
	if !containsStr(reasons, "node(s) had taints") {
		t.Error("expected 'node(s) had taints' in reasons")
	}
}

func containsStr(slice []string, s string) bool {
	for _, item := range slice {
		if strings.Contains(item, s) {
			return true
		}
	}
	return false
}
