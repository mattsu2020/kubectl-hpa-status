package cmd

import (
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// Converter unit tests (convertPendingPodInfos, convertToBlockerPodInfos,
// convertPDBs*, convertQuotas, pdbDisruptionMessage). Split out of the former
// root_extra_test.go grab-bag so each helper's tests live next to converters.go.

// --- convertPendingPodInfos tests ---

func TestConvertPendingPodInfos_Empty(t *testing.T) {
	result := convertPendingPodInfos(nil)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestConvertPendingPodInfos_WithData(t *testing.T) {
	details := []kube.PendingPodDetail{
		{Name: "pod-1", Unschedulable: true, Reasons: []string{"Insufficient cpu"}},
		{Name: "pod-2", Unschedulable: false, Reasons: nil},
	}
	result := convertPendingPodInfos(details)
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0].Name != "pod-1" {
		t.Fatalf("expected pod-1, got %s", result[0].Name)
	}
	if result[0].Phase != "Pending" {
		t.Fatalf("expected Phase=Pending, got %s", result[0].Phase)
	}
	if !result[0].Unschedulable {
		t.Fatal("expected Unschedulable=true")
	}
	if result[1].Unschedulable {
		t.Fatal("expected Unschedulable=false for pod-2")
	}
}

// --- convertToBlockerPodInfos tests (shared conversion path) ---

func TestConvertToBlockerPodInfos_Empty(t *testing.T) {
	result := convertToBlockerPodInfos(nil)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestConvertToBlockerPodInfos_WithData(t *testing.T) {
	details := []kube.PendingPodDetail{
		{Name: "pod-1", Unschedulable: true, Reasons: []string{"Insufficient memory"}},
	}
	result := convertToBlockerPodInfos(details)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Name != "pod-1" {
		t.Fatalf("expected pod-1, got %s", result[0].Name)
	}
	if result[0].Phase != "Pending" {
		t.Fatalf("expected Phase=Pending, got %s", result[0].Phase)
	}
	if !result[0].Unschedulable {
		t.Fatal("expected Unschedulable=true")
	}
}

// --- convertPDBsPlain tests (capacity_plan path: no Disruption message) ---

func TestConvertPDBsPlain_NoDisruption(t *testing.T) {
	infos := []kube.PDBInfo{{Name: "web-pdb", MinAvailable: "3"}}
	result := convertPDBsPlain(infos)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Disruption != "" {
		t.Fatalf("expected empty Disruption for plain conversion, got %q", result[0].Disruption)
	}
	if result[0].MinAvailable != "3" {
		t.Fatalf("expected MinAvailable=3, got %s", result[0].MinAvailable)
	}
}

// --- pdbDisruptionMessage tests ---

func TestPDBDisruptionMessage(t *testing.T) {
	tests := []struct {
		name string
		pdb  kube.PDBInfo
		want string
	}{
		{"minAvailable wins over both", kube.PDBInfo{MinAvailable: "3", MaxUnavailable: "1"}, "minAvailable"},
		{"maxUnavailable when no min", kube.PDBInfo{MaxUnavailable: "2"}, "maxUnavailable=2"},
		{"default when empty", kube.PDBInfo{}, "no availability constraint"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pdbDisruptionMessage(tc.pdb)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("pdbDisruptionMessage(%+v) = %q, want substring %q", tc.pdb, got, tc.want)
			}
		})
	}
}

// --- convertQuotas tests ---

func TestConvertQuotas_Empty(t *testing.T) {
	result := convertQuotas(nil)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestConvertQuotas_WithData(t *testing.T) {
	infos := []kube.QuotaInfo{
		{Name: "compute-quota", Resource: "cpu", Used: "4", Hard: "8"},
	}
	result := convertQuotas(infos)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Name != "compute-quota" {
		t.Fatalf("expected compute-quota, got %s", result[0].Name)
	}
	if result[0].Resource != "cpu" {
		t.Fatalf("expected cpu, got %s", result[0].Resource)
	}
	if !strings.Contains(result[0].Message, "compute-quota") {
		t.Fatalf("expected quota name in message, got %q", result[0].Message)
	}
}

// --- convertPDBs tests ---

func TestConvertPDBs_Empty(t *testing.T) {
	result := convertPDBs(nil)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestConvertPDBs_MinAvailable(t *testing.T) {
	infos := []kube.PDBInfo{
		{Name: "web-pdb", MinAvailable: "3"},
	}
	result := convertPDBs(infos)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Name != "web-pdb" {
		t.Fatalf("expected web-pdb, got %s", result[0].Name)
	}
	if !strings.Contains(result[0].Disruption, "minAvailable") {
		t.Fatalf("expected minAvailable in disruption message, got %q", result[0].Disruption)
	}
}

func TestConvertPDBs_MaxUnavailable(t *testing.T) {
	infos := []kube.PDBInfo{
		{Name: "api-pdb", MaxUnavailable: "1"},
	}
	result := convertPDBs(infos)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if !strings.Contains(result[0].Disruption, "maxUnavailable") {
		t.Fatalf("expected maxUnavailable in disruption message, got %q", result[0].Disruption)
	}
}

func TestConvertPDBs_NoConstraints(t *testing.T) {
	infos := []kube.PDBInfo{
		{Name: "empty-pdb"},
	}
	result := convertPDBs(infos)
	if !strings.Contains(result[0].Disruption, "no availability constraint") {
		t.Fatalf("expected default message, got %q", result[0].Disruption)
	}
}
