package hpa

import (
	"strings"
	"testing"
)

func TestAnalyzeAutoscalerMap_Healthy(t *testing.T) {
	input := AutoscalerMapInput{
		Namespace:               "production",
		HPAName:                 "web",
		Target:                  "Deployment/web",
		CurrentReplicas:         5,
		DesiredReplicas:         5,
		MaxReplicas:             10,
		WorkloadReadyReplicas:   5,
		WorkloadDesiredReplicas: 5,
		PodSummary: AutoscalerMapPodSummary{
			Total:   5,
			Running: 5,
			Pending: 0,
			Ready:   5,
		},
		NodeSummary: AutoscalerMapNodeSummary{
			TotalNodes:        3,
			AllocatableCPU:    "12",
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
	if am.Risk != "none" {
		t.Errorf("Risk = %q, want none", am.Risk)
	}

	// All layers should be healthy.
	for _, layer := range am.Layers {
		if !layer.Healthy {
			t.Errorf("layer %q should be healthy", layer.Name)
		}
	}
}

func TestAnalyzeAutoscalerMap_NodeReadFailureIsUnknown(t *testing.T) {
	input := AutoscalerMapInput{
		Namespace:               "default",
		HPAName:                 "web",
		Target:                  "Deployment/web",
		CurrentReplicas:         3,
		DesiredReplicas:         3,
		MaxReplicas:             10,
		WorkloadReadyReplicas:   3,
		WorkloadDesiredReplicas: 3,
		ScalingActive:           true,
		NodeFetchError:          "nodes is forbidden",
	}
	am := AnalyzeAutoscalerMap(input)
	for _, blocker := range am.Blockers {
		if blocker.Message == "No schedulable nodes found in cluster" {
			t.Fatalf("RBAC failure was converted into a confirmed blocker: %#v", blocker)
		}
	}
	if len(am.Warnings) == 0 || !strings.Contains(am.Warnings[0], "forbidden") {
		t.Fatalf("expected node read warning, got %#v", am.Warnings)
	}
}

func TestAnalyzeAutoscalerMap_PendingPods(t *testing.T) {
	input := AutoscalerMapInput{
		Namespace:               "production",
		HPAName:                 "web",
		Target:                  "Deployment/web",
		CurrentReplicas:         8,
		DesiredReplicas:         10,
		MaxReplicas:             15,
		WorkloadReadyReplicas:   8,
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
		Namespace:               "production",
		HPAName:                 "web",
		Target:                  "Deployment/web",
		CurrentReplicas:         5,
		DesiredReplicas:         5,
		MaxReplicas:             10,
		WorkloadReadyReplicas:   5,
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
		Namespace:               "production",
		HPAName:                 "web",
		Target:                  "Deployment/web",
		CurrentReplicas:         5,
		DesiredReplicas:         5,
		MaxReplicas:             10,
		WorkloadReadyReplicas:   5,
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

func TestAnalyzeAutoscalerMap_KEDALayer(t *testing.T) {
	tests := []struct {
		name       string
		kedaInfo   *AutoscalerMapKEDAInfo
		wantLayers int
		wantRisk   string
		check      func(t *testing.T, am *AutoscalerMap)
	}{
		{
			name: "active KEDA adds external-scaler layer",
			kedaInfo: &AutoscalerMapKEDAInfo{
				ScaledObjectName: "web-orders",
				TriggerCount:     2,
				Active:           true,
			},
			wantLayers: 6,
			wantRisk:   "none",
			check: func(t *testing.T, am *AutoscalerMap) {
				found := false
				for _, l := range am.Layers {
					if l.Name == "external-scaler" {
						found = true
						if !l.Healthy {
							t.Error("expected external-scaler layer to be healthy when active")
						}
					}
				}
				if !found {
					t.Error("expected external-scaler layer when KEDA info is present")
				}
			},
		},
		{
			name: "inactive KEDA produces high severity blocker",
			kedaInfo: &AutoscalerMapKEDAInfo{
				ScaledObjectName: "web-orders",
				TriggerCount:     1,
				Active:           false,
			},
			wantLayers: 6,
			wantRisk:   "high",
			check: func(t *testing.T, am *AutoscalerMap) {
				found := false
				for _, b := range am.Blockers {
					if b.Layer == "external-scaler" && b.Severity == "high" {
						found = true
					}
				}
				if !found {
					t.Error("expected high-severity blocker for inactive KEDA triggers")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := AutoscalerMapInput{
				Namespace:               "prod",
				HPAName:                 "web",
				Target:                  "Deployment/web",
				WorkloadReadyReplicas:   5,
				WorkloadDesiredReplicas: 5,
				PodSummary:              AutoscalerMapPodSummary{Total: 5, Running: 5, Ready: 5},
				NodeSummary:             AutoscalerMapNodeSummary{TotalNodes: 3},
				ScalingActive:           true,
				KEDAInfo:                tt.kedaInfo,
			}

			am := AnalyzeAutoscalerMap(input)
			if len(am.Layers) != tt.wantLayers {
				t.Errorf("expected %d layers, got %d", tt.wantLayers, len(am.Layers))
			}
			if am.Risk != tt.wantRisk {
				t.Errorf("expected risk %q, got %q", tt.wantRisk, am.Risk)
			}
			tt.check(t, am)
		})
	}
}

func TestAnalyzeAutoscalerMap_VPAConflict(t *testing.T) {
	input := AutoscalerMapInput{
		Namespace:               "prod",
		HPAName:                 "web",
		Target:                  "Deployment/web",
		WorkloadReadyReplicas:   5,
		WorkloadDesiredReplicas: 5,
		PodSummary:              AutoscalerMapPodSummary{Total: 5, Running: 5, Ready: 5},
		NodeSummary:             AutoscalerMapNodeSummary{TotalNodes: 3},
		ScalingActive:           true,
		VPAInfo: &AutoscalerMapVPAInfo{
			VPAName:             "web-vpa",
			TargetRef:           "Deployment/web",
			UpdateMode:          "Auto",
			ControlledResources: []string{"cpu", "memory"},
			ConflictResources:   []string{"cpu", "memory"},
		},
	}

	am := AnalyzeAutoscalerMap(input)

	if am.Risk != "medium" {
		t.Errorf("expected risk medium with VPA conflict, got %q", am.Risk)
	}

	foundVPABlocker := false
	for _, b := range am.Blockers {
		if b.Layer == "constraints" && strings.Contains(b.Message, "VPA") {
			foundVPABlocker = true
		}
	}
	if !foundVPABlocker {
		t.Error("expected VPA conflict blocker at constraints layer")
	}

	foundConstraintLayer := false
	for _, l := range am.Layers {
		if l.Name == "constraints" {
			foundConstraintLayer = true
		}
	}
	if !foundConstraintLayer {
		t.Error("expected constraints layer when VPA conflict detected")
	}
}

func TestAnalyzeAutoscalerMap_QuotaAndPDB(t *testing.T) {
	input := AutoscalerMapInput{
		Namespace:               "prod",
		HPAName:                 "web",
		Target:                  "Deployment/web",
		MaxReplicas:             50,
		WorkloadReadyReplicas:   5,
		WorkloadDesiredReplicas: 5,
		PodSummary:              AutoscalerMapPodSummary{Total: 5, Running: 5, Ready: 5},
		NodeSummary:             AutoscalerMapNodeSummary{TotalNodes: 3},
		ScalingActive:           true,
		PDBs: []AutoscalerMapPDB{
			{Name: "web-pdb", MinAvailable: "80%"},
		},
		Quotas: []AutoscalerMapQuota{
			{Name: "compute", Resource: "limits.cpu", Used: "90", Hard: "100", Ratio: 0.9},
		},
	}

	am := AnalyzeAutoscalerMap(input)

	if am.Risk != "high" {
		t.Errorf("expected risk high with quota at 90%%, got %q", am.Risk)
	}

	foundQuotaBlocker := false
	foundPDBBlocker := false
	for _, b := range am.Blockers {
		if b.Layer == "constraints" && strings.Contains(b.Message, "Quota") {
			foundQuotaBlocker = true
		}
		if b.Layer == "constraints" && strings.Contains(b.Message, "PDB") {
			foundPDBBlocker = true
		}
	}
	if !foundQuotaBlocker {
		t.Error("expected quota blocker at constraints layer")
	}
	if !foundPDBBlocker {
		t.Error("expected PDB blocker at constraints layer")
	}
}
