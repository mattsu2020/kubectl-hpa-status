package hpa

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/resourceutil"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestCapacityCheckDecisionsUseTypedMetadata(t *testing.T) {
	nodeCheck := newCapacityCheckResult(
		CapacityCheckNodeCPU,
		CapacityCheckFail,
		"translated message with no node allocatable prefix",
	)
	if !hasNodeCapacityShortfall([]CapacityCheckResult{nodeCheck}) {
		t.Fatal("node shortfall should be detected from CheckID and Status")
	}

	plan := &CapacityPlan{
		TargetMaxReplicas: 20,
		Checks:            []CapacityCheckResult{nodeCheck},
	}
	safe, recommendation, _ := buildRecommendation(plan, CapacityPlanInput{ClusterAutoscaler: true})
	if safe || !strings.Contains(recommendation, "Cluster Autoscaler") {
		t.Fatalf("Cluster Autoscaler must stay advisory: safe=%t recommendation=%q", safe, recommendation)
	}

	quotaCheck := newCapacityCheckResult(
		CapacityCheckQuotaCPU,
		CapacityCheckFail,
		"translated message with no quota wording",
	)
	actions := capacityRemediationActions([]CapacityCheckResult{quotaCheck})
	if len(actions) != 1 || actions[0] != "Increase namespace CPU quota or reduce pod CPU requests" {
		t.Fatalf("typed quota failure produced unexpected remediation: %#v", actions)
	}
}

func TestCapacityCheckResultJSONCompatibility(t *testing.T) {
	check := newCapacityCheckResult(CapacityCheckObservation, CapacityCheckUnknown, "ResourceQuotas unknown: forbidden")
	data, err := json.Marshal(check)
	if err != nil {
		t.Fatal(err)
	}

	var legacy struct {
		Pass    bool   `json:"pass"`
		Unknown bool   `json:"unknown"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Pass || !legacy.Unknown || legacy.Message != check.Message {
		t.Fatalf("legacy fields changed in JSON: %s", data)
	}
	var typed map[string]any
	if err := json.Unmarshal(data, &typed); err != nil {
		t.Fatal(err)
	}
	if typed["checkId"] != string(CapacityCheckObservation) ||
		typed["status"] != string(CapacityCheckUnknown) {
		t.Fatalf("typed fields missing from JSON: %s", data)
	}

	var decoded CapacityCheckResult
	if err := json.Unmarshal([]byte(`{"pass":false,"unknown":true,"message":"legacy unknown"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.capacityStatus() != CapacityCheckUnknown {
		t.Fatalf("legacy JSON status = %q, want %q", decoded.capacityStatus(), CapacityCheckUnknown)
	}

	decoded.Status = CapacityCheckStatus("future-value")
	decoded.Unknown = false
	if decoded.capacityStatus() != CapacityCheckUnknown {
		t.Fatalf("unrecognized typed status = %q, want fail-closed %q", decoded.capacityStatus(), CapacityCheckUnknown)
	}
}

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
	for _, check := range plan.Checks {
		if check.CheckID == "" || check.Status == "" {
			t.Fatalf("check is missing typed metadata: %+v", check)
		}
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
		NodeCapacity: &blocker.NodeCapacitySummary{
			TotalNodes:            3,
			SchedulableNodes:      3,
			SchedulableNodesKnown: true,
			AllocCPU:              "2",
			AllocMemory:           "4Gi",
			PodCapacityKnown:      true,
			AvailablePods:         100,
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
		NodeCapacity: &blocker.NodeCapacitySummary{
			TotalNodes:            3,
			SchedulableNodes:      3,
			SchedulableNodesKnown: true,
			AllocCPU:              "2",
			AllocMemory:           "4Gi",
			PodCapacityKnown:      true,
			AllocPods:             330,
			AvailablePods:         300,
		},
		ClusterAutoscaler: true,
	}

	plan := AnalyzeCapacityPlan(input)

	// CA detection is advisory; compatible node-group capacity is not proven.
	if plan.Safe {
		t.Errorf("CA presence alone must not make the plan safe: %q", plan.Recommendation)
	}
	if !strings.Contains(plan.Recommendation, "Cluster Autoscaler") {
		t.Errorf("expected CA mention in recommendation, got %q; checks=%+v", plan.Recommendation, plan.Checks)
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
	result := resourceutil.Multiply(q, 10)
	if result.Cmp(resource.MustParse("2500m")) != 0 {
		t.Errorf("expected 2500m, got %s", result.String())
	}
}

func TestMultiplyQuantity_Memory(t *testing.T) {
	q := resource.MustParse("512Mi")
	result := resourceutil.Multiply(q, 5)
	if result.Cmp(resource.MustParse("2560Mi")) != 0 {
		t.Errorf("expected 2560Mi, got %s", result.String())
	}
}

func TestMultiplyQuantity_ZeroMultiplier(t *testing.T) {
	q := resource.MustParse("100m")
	result := resourceutil.Multiply(q, 0)
	if !result.IsZero() {
		t.Errorf("expected zero, got %s", result.String())
	}
}

func TestMultiplyQuantity_ZeroQuantity(t *testing.T) {
	q := resource.MustParse("0")
	result := resourceutil.Multiply(q, 10)
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

func TestAnalyzeCapacityPlan_ObservationErrorIsUnknownAndUnsafe(t *testing.T) {
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
		ObservationErrors: []CapacityObservationError{
			{Source: "ResourceQuotas", Message: "forbidden"},
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Fatal("an unknown observation must not produce a Safe recommendation")
	}
	if !strings.Contains(plan.Recommendation, "Cannot confirm") {
		t.Fatalf("expected unknown recommendation, got %q", plan.Recommendation)
	}
	foundUnknown := false
	for _, check := range plan.Checks {
		if check.Unknown && strings.Contains(check.Message, "ResourceQuotas unknown") {
			foundUnknown = true
		}
		if strings.Contains(check.Message, "no namespace ResourceQuotas found") {
			t.Fatalf("quota fetch failure must not be rendered as no quotas: %+v", plan.Checks)
		}
	}
	if !foundUnknown {
		t.Fatalf("expected an explicit unknown check, got %+v", plan.Checks)
	}
}

func TestAnalyzeCapacityPlan_EvaluatesEveryQuotaConstraint(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "production",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   10,
		MaxReplicas:       10,
		TargetMaxReplicas: 20,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "500m"},
		},
		Quotas: []CapacityQuotaInfo{
			{Name: "loose", Resource: "requests.cpu", Used: "0", Hard: "10"},
			{Name: "tight", Resource: "cpu", Used: "9", Hard: "10"},
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Fatal("the tightest applicable quota must block the plan")
	}
	foundTightFailure := false
	for _, check := range plan.Checks {
		if !check.Pass && strings.Contains(check.Message, `"tight"`) &&
			strings.Contains(check.Message, "CPU remaining") {
			foundTightFailure = true
		}
	}
	if !foundTightFailure {
		t.Fatalf("expected the tight quota to fail, got %+v", plan.Checks)
	}
}

func TestAnalyzeCapacityPlan_PodCountQuotaShortfall(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "production",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   8,
		MaxReplicas:       8,
		TargetMaxReplicas: 12,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "100m"},
		},
		Quotas: []CapacityQuotaInfo{
			{Name: "object-counts", Resource: "pods", Used: "98", Hard: "100"},
			{Name: "generic-object-counts", Resource: "count/pods", Used: "99", Hard: "100"},
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Fatal("Pod-count quota with only two slots must block four additional Pods")
	}
	foundPodQuotaFailures := map[string]bool{
		"object-counts":         false,
		"generic-object-counts": false,
	}
	for _, check := range plan.Checks {
		if check.Pass || !strings.Contains(check.Message, "quota pods remaining") {
			continue
		}
		for name := range foundPodQuotaFailures {
			if strings.Contains(check.Message, `"`+name+`"`) {
				foundPodQuotaFailures[name] = true
			}
		}
	}
	for name, found := range foundPodQuotaFailures {
		if !found {
			t.Fatalf("expected Pod-count quota failure for %q, got %+v", name, plan.Checks)
		}
	}
}

func TestAnalyzeCapacityPlan_UsesAvailableRequestHeadroom(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   10,
		MaxReplicas:       10,
		TargetMaxReplicas: 20,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "500m", Memory: "128Mi"},
		},
		NodeCapacity: &blocker.NodeCapacitySummary{
			TotalNodes:      3,
			AllocCPU:        "100",
			AllocMemory:     "100Gi",
			RequestedCPU:    "99",
			RequestedMemory: "99Gi",
			AvailableCPU:    "1",
			AvailableMemory: "1Gi",
		},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Fatal("large total allocatable must not hide exhausted request headroom")
	}
	foundHeadroomFailure := false
	for _, check := range plan.Checks {
		if !check.Pass && strings.Contains(check.Message, "after scheduled Pod requests") {
			foundHeadroomFailure = true
		}
	}
	if !foundHeadroomFailure {
		t.Fatalf("expected request-headroom failure, got %+v", plan.Checks)
	}
	if plan.SchedulableNow != 2 {
		t.Fatalf("SchedulableNow = %d, want 2 from 1 CPU / 500m", plan.SchedulableNow)
	}
}

func TestAnalyzeCapacityPlan_UsesEffectivePodRequest(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   2,
		MaxReplicas:       2,
		TargetMaxReplicas: 4,
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "500m", Memory: "128Mi"},
		},
		PodRequestCPU:    "2",
		PodRequestMemory: "1Gi",
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.RequiredCPU != "4" {
		t.Fatalf("RequiredCPU = %q, want 4 from effective Pod request", plan.RequiredCPU)
	}
	if plan.RequiredMemory != "2Gi" {
		t.Fatalf("RequiredMemory = %q, want 2Gi from effective Pod request", plan.RequiredMemory)
	}
}

func TestAnalyzeCapacityPlan_InvalidQuantityIsUnknownAndUnsafe(t *testing.T) {
	input := CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   2,
		MaxReplicas:       2,
		TargetMaxReplicas: 4,
		PodRequestCPU:     "not-a-quantity",
		PodRequestMemory:  "128Mi",
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Fatal("malformed capacity input must never produce a Safe recommendation")
	}
	foundUnknown := false
	for _, check := range plan.Checks {
		if check.Unknown && strings.Contains(check.Message, "PodRequestCPU") {
			foundUnknown = true
			break
		}
	}
	if !foundUnknown {
		t.Fatalf("expected an explicit invalid-quantity observation, got %+v", plan.Checks)
	}
}

func TestValidateCapacityQuantityInputs_RejectsWhitespaceAndSkipsDependentChecks(t *testing.T) {
	tests := []struct {
		name       string
		domain     CapacityObservationDomain
		source     string
		mutate     func(*CapacityPlanInput)
		skippedIDs []CapacityCheckID
	}{
		{
			name:   "effective Pod request",
			domain: CapacityObservationPodResources,
			source: "PodRequestCPU",
			mutate: func(input *CapacityPlanInput) {
				input.PodRequestCPU = " \t "
			},
			skippedIDs: []CapacityCheckID{
				CapacityCheckQuota, CapacityCheckQuotaCPU,
				CapacityCheckLimitRange, CapacityCheckLimitRangeMinimum, CapacityCheckLimitRangeMaximum,
				CapacityCheckNodeCapacity, CapacityCheckNodeCPU, CapacityCheckNodeMemory,
			},
		},
		{
			name:   "quota",
			domain: CapacityObservationResourceQuotas,
			source: `ResourceQuota "compute" requests.cpu used`,
			mutate: func(input *CapacityPlanInput) {
				input.Quotas[0].Used = " "
			},
			skippedIDs: []CapacityCheckID{
				CapacityCheckQuota, CapacityCheckQuotaCPU,
			},
		},
		{
			name:   "LimitRange",
			domain: CapacityObservationLimitRanges,
			source: `LimitRange "requests" cpu maximum`,
			mutate: func(input *CapacityPlanInput) {
				input.LimitRanges[0].Max = "\n"
			},
			skippedIDs: []CapacityCheckID{
				CapacityCheckLimitRange, CapacityCheckLimitRangeMinimum, CapacityCheckLimitRangeMaximum,
			},
		},
		{
			name:   "node capacity",
			domain: CapacityObservationNodeCapacity,
			source: "node allocatable CPU",
			mutate: func(input *CapacityPlanInput) {
				input.NodeCapacity.AllocCPU = "\t"
			},
			skippedIDs: []CapacityCheckID{
				CapacityCheckNodeCapacity, CapacityCheckNodeCPU, CapacityCheckNodeMemory,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validCapacityQuantityInput()
			tc.mutate(&input)

			validationErrors := validateCapacityQuantityInputs(input)
			if len(validationErrors) != 1 {
				t.Fatalf("validation errors = %+v, want exactly one", validationErrors)
			}
			if validationErrors[0].Domain != tc.domain || validationErrors[0].Source != tc.source {
				t.Fatalf("validation error = %+v, want domain=%q source=%q", validationErrors[0], tc.domain, tc.source)
			}
			if !strings.Contains(validationErrors[0].Message, "whitespace-only") {
				t.Fatalf("validation message = %q, want whitespace-only explanation", validationErrors[0].Message)
			}

			plan := AnalyzeCapacityPlan(input)
			if plan.Safe {
				t.Fatal("whitespace-only quantity must not produce a Safe recommendation")
			}
			assertCapacityCheckIDsAbsent(t, plan.Checks, tc.skippedIDs...)
			if (tc.domain == CapacityObservationPodResources ||
				tc.domain == CapacityObservationNodeCapacity) &&
				plan.SchedulableNow != 0 {
				t.Fatalf("SchedulableNow = %d, want 0 when its inputs are invalid", plan.SchedulableNow)
			}
		})
	}
}

func TestValidateCapacityQuantityInputs_RejectsNegativeNonHeadroomValues(t *testing.T) {
	tests := []struct {
		name   string
		domain CapacityObservationDomain
		mutate func(*CapacityPlanInput)
	}{
		{
			name:   "effective Pod CPU request",
			domain: CapacityObservationPodResources,
			mutate: func(input *CapacityPlanInput) { input.PodRequestCPU = "-100m" },
		},
		{
			name:   "container memory request",
			domain: CapacityObservationPodResources,
			mutate: func(input *CapacityPlanInput) { input.ContainerResources[0].Memory = "-1Mi" },
		},
		{
			name:   "quota hard limit",
			domain: CapacityObservationResourceQuotas,
			mutate: func(input *CapacityPlanInput) { input.Quotas[0].Hard = "-10" },
		},
		{
			name:   "quota usage",
			domain: CapacityObservationResourceQuotas,
			mutate: func(input *CapacityPlanInput) { input.Quotas[0].Used = "-1" },
		},
		{
			name:   "LimitRange minimum",
			domain: CapacityObservationLimitRanges,
			mutate: func(input *CapacityPlanInput) { input.LimitRanges[0].Min = "-1m" },
		},
		{
			name:   "LimitRange maximum",
			domain: CapacityObservationLimitRanges,
			mutate: func(input *CapacityPlanInput) { input.LimitRanges[0].Max = "-1" },
		},
		{
			name:   "node allocatable",
			domain: CapacityObservationNodeCapacity,
			mutate: func(input *CapacityPlanInput) { input.NodeCapacity.AllocCPU = "-1" },
		},
		{
			name:   "node requested",
			domain: CapacityObservationNodeCapacity,
			mutate: func(input *CapacityPlanInput) { input.NodeCapacity.RequestedMemory = "-1Gi" },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validCapacityQuantityInput()
			tc.mutate(&input)

			validationErrors := validateCapacityQuantityInputs(input)
			if len(validationErrors) != 1 {
				t.Fatalf("validation errors = %+v, want exactly one", validationErrors)
			}
			if validationErrors[0].Domain != tc.domain {
				t.Fatalf("validation domain = %q, want %q", validationErrors[0].Domain, tc.domain)
			}
			if !strings.Contains(validationErrors[0].Message, "must not be negative") {
				t.Fatalf("validation message = %q, want negative-value explanation", validationErrors[0].Message)
			}
			if plan := AnalyzeCapacityPlan(input); plan.Safe {
				t.Fatal("negative capacity input must not produce a Safe recommendation")
			} else if tc.domain == CapacityObservationPodResources &&
				(plan.RequiredCPU != "0" || plan.RequiredMemory != "0") {
				t.Fatalf("invalid Pod resources must not be projected: CPU=%q memory=%q", plan.RequiredCPU, plan.RequiredMemory)
			}
		})
	}
}

func TestAnalyzeCapacityPlan_NegativeAvailableHeadroomIsAValidShortfall(t *testing.T) {
	input := validCapacityQuantityInput()
	input.ClusterAutoscaler = false
	input.NodeCapacity.AvailableCPU = "-500m"
	input.NodeCapacity.AvailableMemory = "-1Gi"

	if validationErrors := validateCapacityQuantityInputs(input); len(validationErrors) != 0 {
		t.Fatalf("negative available headroom should be valid, got %+v", validationErrors)
	}

	plan := AnalyzeCapacityPlan(input)
	if plan.Safe {
		t.Fatal("negative available headroom must fail the capacity check")
	}
	if !plan.NodeAutoscalerRequired {
		t.Fatal("node shortfall must require node autoscaling even when Cluster Autoscaler is not detected")
	}
	if !hasNodeCapacityShortfall(plan.Checks) {
		t.Fatalf("expected a typed node shortfall, got %+v", plan.Checks)
	}
	for _, check := range plan.Checks {
		if check.capacityStatus() == CapacityCheckUnknown {
			t.Fatalf("valid negative available headroom must not become unknown: %+v", plan.Checks)
		}
	}
}

func TestAnalyzeCapacityPlan_TypedObservationDomainDoesNotDependOnSourceText(t *testing.T) {
	input := validCapacityQuantityInput()
	input.ObservationErrors = []CapacityObservationError{{
		Domain:  CapacityObservationResourceQuotas,
		Source:  "localized quota inventory label",
		Message: "forbidden",
	}}

	plan := AnalyzeCapacityPlan(input)
	if plan.Safe {
		t.Fatal("unknown quota observation must not produce a Safe recommendation")
	}
	assertCapacityCheckIDsAbsent(t, plan.Checks, CapacityCheckQuota, CapacityCheckQuotaCPU)
}

func TestAnalyzeCapacityPlan_NoSchedulableNodesRequiresNodeAutoscalingWithoutDetectedAutoscaler(t *testing.T) {
	input := validCapacityQuantityInput()
	input.ClusterAutoscaler = false
	input.NodeCapacity.TotalNodes = 0

	plan := AnalyzeCapacityPlan(input)
	if !plan.NodeAutoscalerRequired {
		t.Fatal("no schedulable nodes must require node autoscaling independently of autoscaler detection")
	}
	if plan.Safe {
		t.Fatal("no schedulable nodes without a detected autoscaler must not be Safe")
	}
}

func TestAnalyzeCapacityPlanRejectsExplicitTargetAtOrBelowCurrentMaximum(t *testing.T) {
	input := validCapacityQuantityInput()
	input.TargetMaxReplicas = input.MaxReplicas

	plan := AnalyzeCapacityPlan(input)

	if plan.TargetMaxReplicas != input.MaxReplicas {
		t.Fatalf("invalid explicit target was silently replaced: got %d, want %d", plan.TargetMaxReplicas, input.MaxReplicas)
	}
	if plan.Safe {
		t.Fatal("a target that does not raise maxReplicas must not be Safe")
	}
	if plan.DryRunCommand != "" {
		t.Fatalf("invalid target produced an actionable patch command: %q", plan.DryRunCommand)
	}
	if !hasObservationDomainFromChecks(plan.Checks, CapacityObservationPlanInput) {
		t.Fatalf("plan-input validation check is missing: %+v", plan.Checks)
	}
}

func TestAnalyzeCapacityPlanDefaultTargetDoesNotOverflowInt32(t *testing.T) {
	input := validCapacityQuantityInput()
	input.CurrentReplicas = math.MaxInt32
	input.MaxReplicas = math.MaxInt32
	input.TargetMaxReplicas = 0

	plan := AnalyzeCapacityPlan(input)

	if plan.TargetMaxReplicas != math.MaxInt32 {
		t.Fatalf("target maxReplicas overflowed: %d", plan.TargetMaxReplicas)
	}
	if plan.AdditionalPods != 0 || plan.Safe || plan.DryRunCommand != "" {
		t.Fatalf("int32 ceiling produced an actionable plan: %+v", plan)
	}
}

func TestAnalyzeCapacityPlanDefaultTargetSaturatesBeforeInt32Conversion(t *testing.T) {
	input := validCapacityQuantityInput()
	input.CurrentReplicas = 1_500_000_000
	input.MaxReplicas = 1_500_000_000
	input.TargetMaxReplicas = 0

	plan := AnalyzeCapacityPlan(input)

	if plan.TargetMaxReplicas != math.MaxInt32 {
		t.Fatalf("target maxReplicas = %d, want saturated MaxInt32", plan.TargetMaxReplicas)
	}
	if plan.TargetMaxReplicas < input.MaxReplicas {
		t.Fatalf("target maxReplicas wrapped below current maximum: %+v", plan)
	}
}

func TestAnalyzeCapacityPlanAdvisoryObservationFailuresDoNotBlockSafeScaleUp(t *testing.T) {
	for _, domain := range []CapacityObservationDomain{
		CapacityObservationPDBs,
		CapacityObservationClusterAutoscaler,
	} {
		t.Run(string(domain), func(t *testing.T) {
			input := validCapacityQuantityInput()
			input.ObservationErrors = []CapacityObservationError{{
				Domain:  domain,
				Source:  string(domain),
				Message: "forbidden",
			}}

			plan := AnalyzeCapacityPlan(input)
			if !plan.Safe {
				t.Fatalf("advisory-only observation blocked scale-up: %+v", plan.Checks)
			}
		})
	}
}

func TestAnalyzeCapacityPlanRequiresAutoscalerObservationForNodeFallback(t *testing.T) {
	input := validCapacityQuantityInput()
	input.NodeCapacity.AvailableCPU = "0"
	input.ObservationErrors = []CapacityObservationError{{
		Domain:  CapacityObservationClusterAutoscaler,
		Source:  "Cluster Autoscaler detection",
		Message: "forbidden",
	}}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Fatal("unknown autoscaler presence must block an unmet node-capacity fallback")
	}
	if !strings.Contains(plan.Recommendation, "unknown") {
		t.Fatalf("recommendation does not explain the unknown fallback: %q", plan.Recommendation)
	}
}

func TestAnalyzeCapacityPlanAccountsForPerNodeFragmentation(t *testing.T) {
	input := validCapacityQuantityInput()
	input.CurrentReplicas = 1
	input.MaxReplicas = 1
	input.TargetMaxReplicas = 2
	input.PodRequestCPU = "750m"
	input.PodRequestMemory = "128Mi"
	input.ContainerResources[0].CPU = "750m"
	input.NodeCapacity.AvailableCPU = "1"
	input.NodeCapacity.AvailableMemory = "2Gi"
	input.NodeCapacity.NodeHeadrooms = []blocker.NodeResourceHeadroom{
		{Name: "node-a", AvailableCPU: "500m", AvailableMemory: "1Gi"},
		{Name: "node-b", AvailableCPU: "500m", AvailableMemory: "1Gi"},
	}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe || plan.SchedulableNow != 0 {
		t.Fatalf("fragmented headroom was treated as schedulable: %+v", plan)
	}
	if !hasCheckStatus(plan.Checks, CapacityCheckNodeSchedulable, CapacityCheckFail) {
		t.Fatalf("fragmentation check missing: %+v", plan.Checks)
	}
}

func TestAnalyzeCapacityPlanChecksPodLimitRange(t *testing.T) {
	input := validCapacityQuantityInput()
	input.PodRequestCPU = "1200m"
	input.ContainerResources[0].CPU = "1200m"
	input.LimitRanges = []LimitRangeConstraint{{
		Name: "pod-budget", Type: "Pod", Resource: "cpu", Max: "1",
	}}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe || !hasCheckStatus(plan.Checks, CapacityCheckLimitRangeMaximum, CapacityCheckFail) {
		t.Fatalf("Pod LimitRange violation was not enforced: %+v", plan.Checks)
	}
}

func TestAnalyzeCapacityPlanChecksLimitQuota(t *testing.T) {
	input := validCapacityQuantityInput()
	input.PodLimitCPU = "1"
	input.ContainerResources[0].LimitCPU = "1"
	input.Quotas = []CapacityQuotaInfo{{
		Name: "limits", Resource: "limits.cpu", Used: "9", Hard: "10",
	}}

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe || !hasCheckStatus(plan.Checks, CapacityCheckQuotaLimitCPU, CapacityCheckFail) {
		t.Fatalf("limits.cpu quota shortfall was not enforced: %+v", plan.Checks)
	}
}

func TestAnalyzeCapacityPlanTreatsUnsyncedQuotaUsageAsUnknown(t *testing.T) {
	input := validCapacityQuantityInput()
	observed := false
	input.Quotas[0].UsageObserved = &observed

	plan := AnalyzeCapacityPlan(input)

	if plan.Safe || !hasObservationDomainFromChecks(plan.Checks, CapacityObservationResourceQuotas) {
		t.Fatalf("missing quota status.used was treated as zero usage: %+v", plan.Checks)
	}
}

func validCapacityQuantityInput() CapacityPlanInput {
	return CapacityPlanInput{
		Namespace:         "default",
		HPAName:           "web",
		Target:            "Deployment/web",
		CurrentReplicas:   2,
		MaxReplicas:       2,
		TargetMaxReplicas: 4,
		PodRequestCPU:     "500m",
		PodRequestMemory:  "256Mi",
		PodLimitCPU:       "1",
		ContainerResources: []CapacityContainerResources{
			{Name: "app", CPU: "500m", Memory: "256Mi", LimitCPU: "1"},
		},
		Quotas: []CapacityQuotaInfo{
			{Name: "compute", Resource: "requests.cpu", Used: "1", Hard: "10"},
		},
		LimitRanges: []LimitRangeConstraint{
			{Name: "requests", Type: "Container", Resource: "cpu", Min: "100m", Max: "2"},
		},
		NodeCapacity: &blocker.NodeCapacitySummary{
			TotalNodes:            2,
			SchedulableNodes:      2,
			SchedulableNodesKnown: true,
			AllocCPU:              "8",
			AllocMemory:           "16Gi",
			RequestedCPU:          "2",
			RequestedMemory:       "4Gi",
			AvailableCPU:          "6",
			AvailableMemory:       "12Gi",
			PodCapacityKnown:      true,
			AllocPods:             220,
			RequestedPods:         2,
			AvailablePods:         218,
		},
		ReadyPods: 2,
	}
}

func hasObservationDomainFromChecks(checks []CapacityCheckResult, domain CapacityObservationDomain) bool {
	for _, check := range checks {
		if check.ObservationDomain == domain {
			return true
		}
	}
	return false
}

func hasCheckStatus(checks []CapacityCheckResult, id CapacityCheckID, status CapacityCheckStatus) bool {
	for _, check := range checks {
		if check.CheckID == id && check.capacityStatus() == status {
			return true
		}
	}
	return false
}

func assertCapacityCheckIDsAbsent(t *testing.T, checks []CapacityCheckResult, ids ...CapacityCheckID) {
	t.Helper()
	for _, check := range checks {
		for _, id := range ids {
			if check.CheckID == id {
				t.Fatalf("check %q should have been skipped, got %+v", id, checks)
			}
		}
	}
}
