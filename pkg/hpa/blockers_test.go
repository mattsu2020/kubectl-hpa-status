package hpa

import (
	"strings"
	"testing"
)

func TestAnalyzeBlockers_ScaleOutWithPendingAndQuota(t *testing.T) {
	input := BlockerInput{
		DesiredReplicas:    12,
		CurrentReplicas:    8,
		TargetReadyReplicas: 8,
		TargetDesiredReplicas: 12,
		MinReplicas:        2,
		MaxReplicas:        20,
		ReadyPods:          8,
		TotalPods:          12,
		PendingPods: []BlockerPodInfo{
			{Name: "web-1", Phase: "Pending", Unschedulable: true, Reasons: []string{"Insufficient cpu"}},
			{Name: "web-2", Phase: "Pending", Unschedulable: true, Reasons: []string{"Insufficient cpu"}},
			{Name: "web-3", Phase: "Pending", Unschedulable: true, Reasons: []string{"Insufficient cpu"}},
			{Name: "web-4", Phase: "Pending"},
		},
		FailedSchedulingEvents: []string{"0/3 nodes available: Insufficient cpu (3)"},
		Quotas: []BlockerQuotaInfo{
			{Name: "compute", Resource: "requests.cpu", Used: "45", Hard: "48", Ratio: 0.9375},
		},
		ScalingActive: true,
	}

	report := AnalyzeBlockers(input)

	if !report.HPAWantsScale {
		t.Error("expected HPAWantsScale to be true")
	}
	if report.DesiredReplicas != 12 {
		t.Errorf("expected DesiredReplicas=12, got %d", report.DesiredReplicas)
	}
	if report.ReadyReplicas != 8 {
		t.Errorf("expected ReadyReplicas=8, got %d", report.ReadyReplicas)
	}
	if !strings.Contains(report.Summary, "12 replicas") || !strings.Contains(report.Summary, "8 pods are Ready") {
		t.Errorf("expected summary to mention 12 replicas and 8 ready, got %s", report.Summary)
	}

	// Should have HIGH findings for pending pods, unschedulable, scheduling, and quota.
	highCount := 0
	for _, b := range report.Blockers {
		if b.Severity == BlockerHigh {
			highCount++
		}
	}
	if highCount < 2 {
		t.Errorf("expected at least 2 HIGH findings, got %d (blockers: %v)", highCount, report.Blockers)
	}

	if report.Interpretation == "" {
		t.Error("expected non-empty interpretation")
	}
	if !strings.Contains(report.Interpretation, "HPA appears to be working") {
		t.Errorf("expected interpretation to mention HPA working, got %s", report.Interpretation)
	}
	if len(report.NextCommands) == 0 {
		t.Error("expected next commands to be populated")
	}
}

func TestAnalyzeBlockers_NoScaleOutNeeded(t *testing.T) {
	input := BlockerInput{
		DesiredReplicas:    8,
		CurrentReplicas:    8,
		TargetReadyReplicas: 8,
		ScalingActive:      true,
	}

	report := AnalyzeBlockers(input)

	if report.HPAWantsScale {
		t.Error("expected HPAWantsScale to be false")
	}
	if !strings.Contains(report.Summary, "not requesting scale-out") {
		t.Errorf("expected summary about no scale-out, got %s", report.Summary)
	}
}

func TestAnalyzeBlockers_ContainerFailure(t *testing.T) {
	input := BlockerInput{
		DesiredReplicas:    5,
		CurrentReplicas:    3,
		TargetReadyReplicas: 2,
		ContainerStatuses: []ContainerStatusSummary{
			{Pod: "web-1", Container: "app", Waiting: true, WaitingReason: "CrashLoopBackOff", RestartCount: 7},
		},
		ScalingActive: true,
	}

	report := AnalyzeBlockers(input)

	hasAppBlocker := false
	for _, b := range report.Blockers {
		if b.Category == "application" {
			hasAppBlocker = true
			if !strings.Contains(b.Message, "CrashLoopBackOff") {
				t.Errorf("expected CrashLoopBackOff in message, got %s", b.Message)
			}
		}
	}
	if !hasAppBlocker {
		t.Error("expected an application-category blocker")
	}
	if !strings.Contains(report.Interpretation, "application or image issues") {
		t.Errorf("expected interpretation to mention application issues, got %s", report.Interpretation)
	}
}

func TestAnalyzeBlockers_BlockerSeverityOrder(t *testing.T) {
	input := BlockerInput{
		DesiredReplicas:    12,
		CurrentReplicas:    8,
		TargetReadyReplicas: 8,
		PendingPods: []BlockerPodInfo{
			{Name: "web-1", Phase: "Pending", Unschedulable: true},
		},
		Quotas: []BlockerQuotaInfo{
			{Name: "compute", Resource: "requests.cpu", Used: "40", Hard: "48", Ratio: 0.83},
		},
		ScalingActive: true,
	}

	report := AnalyzeBlockers(input)

	// Verify HIGH findings come before MEDIUM, MEDIUM before INFO.
	lastOrder := -1
	for _, b := range report.Blockers {
		order := severityOrder(b.Severity)
		if order < lastOrder {
			t.Errorf("blockers not sorted by severity: %s (%d) after order %d", b.Severity, order, lastOrder)
		}
		lastOrder = order
	}
}

func TestAnalyzeBlockers_EmptyInput(t *testing.T) {
	input := BlockerInput{
		DesiredReplicas:    3,
		CurrentReplicas:    3,
		TargetReadyReplicas: 3,
		ScalingActive:      true,
	}

	report := AnalyzeBlockers(input)

	if report.HPAWantsScale {
		t.Error("expected HPAWantsScale to be false for equal replicas")
	}
	// Should still have at least the INFO metrics healthy finding.
	if len(report.Blockers) == 0 {
		t.Error("expected at least the INFO metrics healthy finding")
	}
}

func TestSortFindingsBySeverity(t *testing.T) {
	findings := []BlockerFinding{
		{ID: "info", Severity: BlockerInfo, Message: "info"},
		{ID: "high", Severity: BlockerHigh, Message: "high"},
		{ID: "medium", Severity: BlockerMedium, Message: "medium"},
		{ID: "high2", Severity: BlockerHigh, Message: "high2"},
	}

	sorted := sortFindingsBySeverity(findings)

	if sorted[0].Severity != BlockerHigh {
		t.Errorf("expected first to be HIGH, got %s", sorted[0].Severity)
	}
	if sorted[1].Severity != BlockerHigh {
		t.Errorf("expected second to be HIGH, got %s", sorted[1].Severity)
	}
	if sorted[2].Severity != BlockerMedium {
		t.Errorf("expected third to be MEDIUM, got %s", sorted[2].Severity)
	}
	if sorted[3].Severity != BlockerInfo {
		t.Errorf("expected fourth to be INFO, got %s", sorted[3].Severity)
	}
	// Verify stable sort preserves relative order of same severity.
	if sorted[0].ID != "high" || sorted[1].ID != "high2" {
		t.Errorf("stable sort did not preserve relative order of HIGH findings: %s, %s", sorted[0].ID, sorted[1].ID)
	}
}
