package hpa

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAnalyzeCapacityPlan_AllChecksPass(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "production",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   10,
		MaxReplicas:       10,
		TargetMaxReplicas: 20,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "250m", Memory: "512Mi"},
		},
		ReadyPods: 10,
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Namespace != "production" {
		t.Errorf("expected namespace 'production', got %q", plan.Namespace)
	}
	if plan.Name != "web" {
		t.Errorf("expected name 'web', got %q", plan.Name)
	}
	if plan.TargetMaxReplicas != 20 {
		t.Errorf("expected targetMaxReplicas 20, got %d", plan.TargetMaxReplicas)
	}
	if plan.AdditionalPods != 10 {
		t.Errorf("expected additionalPods 10, got %d", plan.AdditionalPods)
	}
	// 250m * 10 = 2500m, preserved by MilliValue-based multiplication.
	if plan.RequiredCPU != "2500m" {
		t.Errorf("expected requiredCPU '2500m', got %q", plan.RequiredCPU)
	}
	if !plan.Safe {
		t.Errorf("expected plan to be safe, got checks: %+v", plan.Checks)
	}
	if !strings.Contains(plan.Recommendation, "Safe to raise maxReplicas to 20") {
		t.Errorf("expected safe recommendation, got %q", plan.Recommendation)
	}
}

func TestAnalyzeCapacityPlan_DefaultTargetMax(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "api",
		Target:            "Deployment/api",
		CurrentReplicas:   5,
		MaxReplicas:       5,
		TargetMaxReplicas: 0, // should default to 10 (5*2)
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "100m", Memory: "128Mi"},
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.TargetMaxReplicas != 10 {
		t.Errorf("expected default targetMaxReplicas 10, got %d", plan.TargetMaxReplicas)
	}
	if plan.AdditionalPods != 5 {
		t.Errorf("expected additionalPods 5, got %d", plan.AdditionalPods)
	}
}

func TestAnalyzeCapacityPlan_TargetMaxOverride(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "api",
		Target:            "Deployment/api",
		CurrentReplicas:   5,
		MaxReplicas:       5,
		TargetMaxReplicas: 30, // explicit override
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "100m", Memory: "128Mi"},
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.TargetMaxReplicas != 30 {
		t.Errorf("expected targetMaxReplicas 30, got %d", plan.TargetMaxReplicas)
	}
	if plan.AdditionalPods != 25 {
		t.Errorf("expected additionalPods 25, got %d", plan.AdditionalPods)
	}
}

func TestAnalyzeCapacityPlan_QuotaShortfall(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "production",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   10,
		MaxReplicas:       10,
		TargetMaxReplicas: 20,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "250m", Memory: "512Mi"},
		},
		Quotas: []CapacityQuotaInfo{
			{Name: "compute", Resource: "requests.cpu", Used: "9", Hard: "10"},
			{Name: "compute", Resource: "requests.memory", Used: "8Gi", Hard: "10Gi"},
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Errorf("expected plan to be unsafe due to quota shortfall")
	}
	if !strings.Contains(plan.Recommendation, "Do not raise maxReplicas to 20") {
		t.Errorf("expected unsafe recommendation, got %q", plan.Recommendation)
	}

	// Should have failing quota checks.
	foundCPUFail := false
	foundMemFail := false
	for _, c := range plan.Checks {
		if !c.Pass && strings.Contains(c.Message, "CPU remaining") {
			foundCPUFail = true
		}
		if !c.Pass && strings.Contains(c.Message, "memory remaining") {
			foundMemFail = true
		}
	}
	if !foundCPUFail {
		t.Error("expected failing CPU quota check")
	}
	if !foundMemFail {
		t.Error("expected failing memory quota check")
	}
	if len(plan.NextActions) == 0 {
		t.Error("expected next actions for quota shortfall")
	}
}

func TestAnalyzeCapacityPlan_LimitRangeViolation(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   5,
		MaxReplicas:       5,
		TargetMaxReplicas: 10,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "2", Memory: "4Gi"},
		},
		LimitRanges: []LimitRangeConstraint{
			{Name: "cpu-limits", Type: "Container", Resource: "cpu", Max: "1"},
			{Name: "mem-limits", Type: "Container", Resource: "memory", Max: "2Gi"},
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Errorf("expected plan to be unsafe due to LimitRange violation")
	}

	foundCPUViolation := false
	foundMemViolation := false
	for _, c := range plan.Checks {
		if !c.Pass && strings.Contains(c.Message, "exceeds LimitRange") && strings.Contains(c.Message, "cpu-limits") {
			foundCPUViolation = true
		}
		if !c.Pass && strings.Contains(c.Message, "exceeds LimitRange") && strings.Contains(c.Message, "mem-limits") {
			foundMemViolation = true
		}
	}
	if !foundCPUViolation {
		t.Error("expected CPU LimitRange violation")
	}
	if !foundMemViolation {
		t.Error("expected memory LimitRange violation")
	}
}

func TestAnalyzeCapacityPlan_PendingPods(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   5,
		MaxReplicas:       5,
		TargetMaxReplicas: 10,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "100m", Memory: "128Mi"},
		},
		PendingPods: []PendingPodInfo{
			{Name: "pod-1", Phase: "Pending", Unschedulable: true, Reasons: []string{"Insufficient memory"}},
			{Name: "pod-2", Phase: "Pending", Unschedulable: true, Reasons: []string{"Insufficient memory"}},
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Errorf("expected plan to be unsafe due to pending pods")
	}

	foundPending := false
	for _, c := range plan.Checks {
		if !c.Pass && strings.Contains(c.Message, "Pending") {
			foundPending = true
		}
	}
	if !foundPending {
		t.Errorf("expected failing pending pod check, got checks: %+v", plan.Checks)
	}
}

func TestAnalyzeCapacityPlan_NodeCapacityInsufficient(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   10,
		MaxReplicas:       10,
		TargetMaxReplicas: 20,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "500m", Memory: "1Gi"},
		},
		NodeCapacity: &NodeCapacitySummary{
			TotalNodes:  3,
			AllocCPU:    "2",
			AllocMemory: "4Gi",
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Errorf("expected plan to be unsafe due to insufficient node capacity")
	}

	foundNodeCPUFail := false
	for _, c := range plan.Checks {
		if !c.Pass && strings.Contains(c.Message, "node allocatable CPU") {
			foundNodeCPUFail = true
		}
	}
	if !foundNodeCPUFail {
		t.Errorf("expected failing node CPU check, got checks: %+v", plan.Checks)
	}
}

func TestAnalyzeCapacityPlan_ClusterAutoscalerDetected(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   10,
		MaxReplicas:       10,
		TargetMaxReplicas: 20,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "500m", Memory: "1Gi"},
		},
		NodeCapacity: &NodeCapacitySummary{
			TotalNodes:  3,
			AllocCPU:    "2",
			AllocMemory: "4Gi",
		},
		ClusterAutoscaler: true,
	}

	plan := AnalyzeCapacityPlan(input)

	// With CA and only node capacity failing, it should be "likely safe".
	if !plan.Safe {
		t.Errorf("expected plan to be safe with CA, got recommendation: %q", plan.Recommendation)
	}
	if !strings.Contains(plan.Recommendation, "Cluster Autoscaler will provision nodes") {
		t.Errorf("expected CA mention in recommendation, got %q", plan.Recommendation)
	}

	// Should have informational check about CA.
	foundCA := false
	for _, c := range plan.Checks {
		if c.Pass && strings.Contains(c.Message, "Cluster Autoscaler detected") {
			foundCA = true
		}
	}
	if !foundCA {
		t.Error("expected Cluster Autoscaler check result")
	}
}

func TestAnalyzeCapacityPlan_NotAtMaxReplicas(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   3,
		MaxReplicas:       10,
		TargetMaxReplicas: 20,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "100m", Memory: "128Mi"},
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if !strings.Contains(plan.Issue, "not at maxReplicas") {
		t.Errorf("expected issue about not being at max, got %q", plan.Issue)
	}
	// AdditionalPods should be 20 - 3 = 17.
	if plan.AdditionalPods != 17 {
		t.Errorf("expected additionalPods 17, got %d", plan.AdditionalPods)
	}
}

func TestAnalyzeCapacityPlan_PDBInformational(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   5,
		MaxReplicas:       5,
		TargetMaxReplicas: 10,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "100m", Memory: "128Mi"},
		},
		PDBs: []PDBInterference{
			{Name: "web-pdb", MinAvailable: "80%", Disruption: "none"},
		},
	}

	plan := AnalyzeCapacityPlan(input)

	// PDB is informational, should not block.
	if !plan.Safe {
		t.Errorf("expected plan to be safe (PDB is informational), got checks: %+v", plan.Checks)
	}

	foundPDB := false
	for _, c := range plan.Checks {
		if c.Pass && strings.Contains(c.Message, "web-pdb") {
			foundPDB = true
		}
	}
	if !foundPDB {
		t.Error("expected PDB informational check")
	}
}

func TestMultiplyQuantity_FractionalCPU(t *testing.T) {
	q := resource.MustParse("250m")
	result := multiplyQuantity(q, 10)
	if result.Cmp(resource.MustParse("2500m")) != 0 {
		t.Errorf("expected 2500m, got %s", result.String())
	}
}

func TestMultiplyQuantity_Memory(t *testing.T) {
	q := resource.MustParse("512Mi")
	result := multiplyQuantity(q, 5)
	if result.Cmp(resource.MustParse("2560Mi")) != 0 {
		t.Errorf("expected 2560Mi, got %s", result.String())
	}
}

func TestMultiplyQuantity_ZeroMultiplier(t *testing.T) {
	q := resource.MustParse("100m")
	result := multiplyQuantity(q, 0)
	if !result.IsZero() {
		t.Errorf("expected zero, got %s", result.String())
	}
}

func TestMultiplyQuantity_ZeroQuantity(t *testing.T) {
	q := resource.MustParse("0")
	result := multiplyQuantity(q, 10)
	if !result.IsZero() {
		t.Errorf("expected zero, got %s", result.String())
	}
}

func TestSumContainerResources_MultipleContainers(t *testing.T) {
	containers := []CapacityContainerResources{
		{Name: "app", CPU: "250m", Memory: "512Mi"},
		{Name: "sidecar", CPU: "100m", Memory: "128Mi"},
	}
	cpu, mem := sumContainerResources(containers)

	if cpu.Cmp(resource.MustParse("350m")) != 0 {
		t.Errorf("expected CPU 350m, got %s", cpu.String())
	}
	if mem.Cmp(resource.MustParse("640Mi")) != 0 {
		t.Errorf("expected memory 640Mi, got %s", mem.String())
	}
}

func TestSumContainerResources_EmptyValues(t *testing.T) {
	containers := []CapacityContainerResources{
		{Name: "app", CPU: "", Memory: "0"},
	}
	cpu, mem := sumContainerResources(containers)

	if !cpu.IsZero() {
		t.Errorf("expected zero CPU, got %s", cpu.String())
	}
	if !mem.IsZero() {
		t.Errorf("expected zero memory, got %s", mem.String())
	}
}

func TestAnalyzeCapacityPlan_NoContainerResources(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:          "default",
		HPAName:            "web",
		Target:             "Deployment/web",
		CurrentReplicas:    5,
		MaxReplicas:        5,
		TargetMaxReplicas:  10,
		ContainerResources: nil,
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.RequiredCPU != "0" {
		t.Errorf("expected zero CPU with no containers, got %q", plan.RequiredCPU)
	}
	if plan.RequiredMemory != "0" {
		t.Errorf("expected zero memory with no containers, got %q", plan.RequiredMemory)
	}
}
