package hpa

import (
	"strings"
	"testing"
)

func TestScaleOutDesiredRule(t *testing.T) {
	t.Run("wants scale out", func(t *testing.T) {
		input := BlockerInput{DesiredReplicas: 12, CurrentReplicas: 8}
		findings := scaleOutDesiredRule(input)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		if findings[0].ID != "scale-out-desired" {
			t.Errorf("expected ID scale-out-desired, got %s", findings[0].ID)
		}
		if findings[0].Severity != BlockerHigh {
			t.Errorf("expected HIGH severity, got %s", findings[0].Severity)
		}
	})

	t.Run("no scale out needed", func(t *testing.T) {
		input := BlockerInput{DesiredReplicas: 8, CurrentReplicas: 8}
		findings := scaleOutDesiredRule(input)
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %d", len(findings))
		}
	})

	t.Run("scale down", func(t *testing.T) {
		input := BlockerInput{DesiredReplicas: 5, CurrentReplicas: 8}
		findings := scaleOutDesiredRule(input)
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings for scale-down, got %d", len(findings))
		}
	})
}

func TestPendingPodsRule(t *testing.T) {
	t.Run("pending pods exist", func(t *testing.T) {
		input := BlockerInput{
			DesiredReplicas: 12,
			CurrentReplicas: 8,
			PendingPods: []BlockerPodInfo{
				{Name: "web-abc", Phase: "Pending", Unschedulable: true},
				{Name: "web-def", Phase: "Pending"},
			},
		}
		findings := pendingPodsRule(input)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		if findings[0].Severity != BlockerHigh {
			t.Errorf("expected HIGH severity, got %s", findings[0].Severity)
		}
		if !strings.Contains(findings[0].Message, "2 pods are Pending") {
			t.Errorf("expected message to mention 2 pods, got %s", findings[0].Message)
		}
	})

	t.Run("no pending pods", func(t *testing.T) {
		input := BlockerInput{DesiredReplicas: 8, CurrentReplicas: 8}
		findings := pendingPodsRule(input)
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %d", len(findings))
		}
	})
}

func TestUnschedulableRule(t *testing.T) {
	t.Run("unschedulable pods", func(t *testing.T) {
		input := BlockerInput{
			PendingPods: []BlockerPodInfo{
				{Name: "web-abc", Phase: "Pending", Unschedulable: true, Reasons: []string{"0/3 nodes available"}},
			},
		}
		findings := unschedulableRule(input)
		if len(findings) == 0 {
			t.Fatal("expected at least 1 finding")
		}
		if findings[0].ID != "unschedulable-pods" {
			t.Errorf("expected ID unschedulable-pods, got %s", findings[0].ID)
		}
	})

	t.Run("failed scheduling event", func(t *testing.T) {
		input := BlockerInput{
			FailedSchedulingEvents: []string{"0/3 nodes available: Insufficient cpu (3)"},
		}
		findings := unschedulableRule(input)
		if len(findings) == 0 {
			t.Fatal("expected at least 1 finding for FailedScheduling event")
		}
		hasFailedScheduling := false
		for _, f := range findings {
			if f.ID == "failed-scheduling" {
				hasFailedScheduling = true
			}
		}
		if !hasFailedScheduling {
			t.Error("expected a failed-scheduling finding")
		}
	})

	t.Run("no scheduling issues", func(t *testing.T) {
		input := BlockerInput{}
		findings := unschedulableRule(input)
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %d", len(findings))
		}
	})
}

func TestContainerFailureRule(t *testing.T) {
	t.Run("image pull backoff", func(t *testing.T) {
		input := BlockerInput{
			ContainerStatuses: []ContainerStatusSummary{
				{Pod: "web-abc", Container: "app", Waiting: true, WaitingReason: "ImagePullBackOff"},
			},
		}
		findings := containerFailureRule(input)
		if len(findings) == 0 {
			t.Fatal("expected at least 1 finding")
		}
		if findings[0].ID != "image-pull-failure" {
			t.Errorf("expected ID image-pull-failure, got %s", findings[0].ID)
		}
		if findings[0].Category != "application" {
			t.Errorf("expected category application, got %s", findings[0].Category)
		}
	})

	t.Run("crash loop back off", func(t *testing.T) {
		input := BlockerInput{
			ContainerStatuses: []ContainerStatusSummary{
				{Pod: "web-abc", Container: "app", Waiting: true, WaitingReason: "CrashLoopBackOff", RestartCount: 5},
			},
		}
		findings := containerFailureRule(input)
		if len(findings) == 0 {
			t.Fatal("expected at least 1 finding")
		}
		if findings[0].ID != "crash-loop" {
			t.Errorf("expected ID crash-loop, got %s", findings[0].ID)
		}
		if !strings.Contains(findings[0].Message, "CrashLoopBackOff") {
			t.Errorf("expected message to mention CrashLoopBackOff, got %s", findings[0].Message)
		}
	})

	t.Run("no container issues", func(t *testing.T) {
		input := BlockerInput{
			ContainerStatuses: []ContainerStatusSummary{
				{Pod: "web-abc", Container: "app", Waiting: false},
			},
		}
		findings := containerFailureRule(input)
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %d", len(findings))
		}
	})

	t.Run("multiple pods consolidated", func(t *testing.T) {
		input := BlockerInput{
			ContainerStatuses: []ContainerStatusSummary{
				{Pod: "web-1", Container: "app", Waiting: true, WaitingReason: "ImagePullBackOff"},
				{Pod: "web-2", Container: "app", Waiting: true, WaitingReason: "ImagePullBackOff"},
				{Pod: "web-3", Container: "app", Waiting: true, WaitingReason: "ImagePullBackOff"},
			},
		}
		findings := containerFailureRule(input)
		imagePullCount := 0
		for _, f := range findings {
			if f.ID == "image-pull-failure" {
				imagePullCount++
			}
		}
		if imagePullCount != 1 {
			t.Errorf("expected 1 consolidated image-pull-failure, got %d", imagePullCount)
		}
		if len(findings) > 0 && !strings.Contains(findings[0].Message, "3 pods") {
			t.Errorf("expected consolidated message to mention '3 pods', got %s", findings[0].Message)
		}
	})
}

func TestQuotaNearLimitRule(t *testing.T) {
	t.Run("quota near limit", func(t *testing.T) {
		input := BlockerInput{
			Quotas: []BlockerQuotaInfo{
				{Name: "compute", Resource: "requests.cpu", Used: "47.5", Hard: "48", Ratio: 0.99},
			},
		}
		findings := quotaNearLimitRule(input)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		if findings[0].Severity != BlockerHigh {
			t.Errorf("expected HIGH severity for 99%% usage, got %s", findings[0].Severity)
		}
		if findings[0].Category != "quota" {
			t.Errorf("expected category quota, got %s", findings[0].Category)
		}
	})

	t.Run("quota at 85 percent", func(t *testing.T) {
		input := BlockerInput{
			Quotas: []BlockerQuotaInfo{
				{Name: "compute", Resource: "requests.cpu", Used: "40", Hard: "48", Ratio: 0.85},
			},
		}
		findings := quotaNearLimitRule(input)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		if findings[0].Severity != BlockerMedium {
			t.Errorf("expected MEDIUM severity for 85%% usage, got %s", findings[0].Severity)
		}
	})

	t.Run("no quota constraints", func(t *testing.T) {
		input := BlockerInput{}
		findings := quotaNearLimitRule(input)
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %d", len(findings))
		}
	})
}

func TestReadinessStalledRule(t *testing.T) {
	t.Run("readiness stalled", func(t *testing.T) {
		input := BlockerInput{
			DesiredReplicas:    12,
			CurrentReplicas:    12,
			TargetReadyReplicas: 8,
			PendingPods:        nil,
		}
		findings := readinessStalledRule(input)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		if findings[0].ID != "readiness-stalled" {
			t.Errorf("expected ID readiness-stalled, got %s", findings[0].ID)
		}
		if findings[0].Severity != BlockerMedium {
			t.Errorf("expected MEDIUM severity, got %s", findings[0].Severity)
		}
	})

	t.Run("not stalled with pending pods", func(t *testing.T) {
		input := BlockerInput{
			DesiredReplicas:    12,
			CurrentReplicas:    12,
			TargetReadyReplicas: 8,
			PendingPods: []BlockerPodInfo{{Name: "web-abc", Phase: "Pending"}},
		}
		findings := readinessStalledRule(input)
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings when pending pods exist, got %d", len(findings))
		}
	})

	t.Run("all ready", func(t *testing.T) {
		input := BlockerInput{
			DesiredReplicas:    8,
			CurrentReplicas:    8,
			TargetReadyReplicas: 8,
		}
		findings := readinessStalledRule(input)
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings when all ready, got %d", len(findings))
		}
	})
}

func TestNodeCapacityRule(t *testing.T) {
	t.Run("no nodes", func(t *testing.T) {
		input := BlockerInput{
			NodeCapacity: &NodeCapacitySummary{TotalNodes: 0},
		}
		findings := nodeCapacityRule(input)
		if len(findings) == 0 {
			t.Fatal("expected finding for no nodes")
		}
		if findings[0].ID != "no-nodes" {
			t.Errorf("expected ID no-nodes, got %s", findings[0].ID)
		}
	})

	t.Run("all nodes tainted", func(t *testing.T) {
		input := BlockerInput{
			NodeCapacity: &NodeCapacitySummary{TotalNodes: 3, TaintedNodes: 3},
		}
		findings := nodeCapacityRule(input)
		if len(findings) == 0 {
			t.Fatal("expected finding for all tainted nodes")
		}
		if findings[0].ID != "all-nodes-tainted" {
			t.Errorf("expected ID all-nodes-tainted, got %s", findings[0].ID)
		}
	})

	t.Run("no node data", func(t *testing.T) {
		input := BlockerInput{}
		findings := nodeCapacityRule(input)
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings when no node data, got %d", len(findings))
		}
	})
}

func TestMetricsHealthyInfoRule(t *testing.T) {
	t.Run("scaling active", func(t *testing.T) {
		input := BlockerInput{ScalingActive: true}
		findings := metricsHealthyInfoRule(input)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		if findings[0].Severity != BlockerInfo {
			t.Errorf("expected INFO severity, got %s", findings[0].Severity)
		}
		if !strings.Contains(findings[0].Message, "No recent metrics retrieval errors") {
			t.Errorf("expected healthy message, got %s", findings[0].Message)
		}
	})

	t.Run("scaling inactive", func(t *testing.T) {
		input := BlockerInput{ScalingActive: false}
		findings := metricsHealthyInfoRule(input)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		if !strings.Contains(findings[0].Message, "ScalingActive is False") {
			t.Errorf("expected inactive message, got %s", findings[0].Message)
		}
	})
}
