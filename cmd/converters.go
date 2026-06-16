package cmd

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// This file centralizes the conversion helpers that translate kube.* data
// types into the pkg/hpa analysis types. Several commands (capacity, capacity
// plan, blockers, autoscaler-map) need the same translation; collecting them
// here removes the copy-paste duplication that previously lived in each file.

// convertPendingPodDetail maps one kube.PendingPodDetail to a pending-pod
// destination type via a builder callback. Centralizing the loop removes the
// three identical copies that previously lived in capacity.go,
// autoscaler_map.go, and capacity_plan.go.
func convertPendingPodDetail[T any](details []kube.PendingPodDetail, build func(kube.PendingPodDetail) T) []T {
	if len(details) == 0 {
		return nil
	}
	result := makeSlice[T](len(details))
	for _, d := range details {
		result = append(result, build(d))
	}
	return result
}

func makeSlice[T any](capacity int) []T { return make([]T, 0, capacity) }

// convertToPendingPodInfos translates into the analysis-side PendingPodInfo
// (Phase always "Pending"). Shared by capacity, capacity-plan, autoscaler-map.
func convertToPendingPodInfos(details []kube.PendingPodDetail) []hpaanalysis.PendingPodInfo {
	return convertPendingPodDetail(details, func(d kube.PendingPodDetail) hpaanalysis.PendingPodInfo {
		return hpaanalysis.PendingPodInfo{
			Name:          d.Name,
			Phase:         "Pending",
			Unschedulable: d.Unschedulable,
			Reasons:       d.Reasons,
		}
	})
}

// convertToBlockerPodInfos translates into the blocker-analysis destination
// type (same four fields, different package-local struct).
func convertToBlockerPodInfos(details []kube.PendingPodDetail) []hpaanalysis.BlockerPodInfo {
	return convertPendingPodDetail(details, func(d kube.PendingPodDetail) hpaanalysis.BlockerPodInfo {
		return hpaanalysis.BlockerPodInfo{
			Name:          d.Name,
			Phase:         "Pending",
			Unschedulable: d.Unschedulable,
			Reasons:       d.Reasons,
		}
	})
}

// convertQuotas translates ResourceQuota info into the analysis-side
// QuotaConstraint, populating the human-readable Message.
func convertQuotas(infos []kube.QuotaInfo) []hpaanalysis.QuotaConstraint {
	if len(infos) == 0 {
		return nil
	}
	result := make([]hpaanalysis.QuotaConstraint, 0, len(infos))
	for _, q := range infos {
		result = append(result, hpaanalysis.QuotaConstraint{
			Name:     q.Name,
			Resource: q.Resource,
			Used:     q.Used,
			Hard:     q.Hard,
			Message:  fmt.Sprintf("ResourceQuota %q is near limit for %s (used=%s, hard=%s)", q.Name, q.Resource, q.Used, q.Hard),
		})
	}
	return result
}

// convertPDBs translates PDB info into PDBInterference, deriving a
// human-readable Disruption message from minAvailable / maxUnavailable.
func convertPDBs(infos []kube.PDBInfo) []hpaanalysis.PDBInterference {
	if len(infos) == 0 {
		return nil
	}
	result := make([]hpaanalysis.PDBInterference, 0, len(infos))
	for _, p := range infos {
		result = append(result, hpaanalysis.PDBInterference{
			Name:           p.Name,
			MinAvailable:   p.MinAvailable,
			MaxUnavailable: p.MaxUnavailable,
			Disruption:     pdbDisruptionMessage(p),
		})
	}
	return result
}

// convertPDBsPlain translates PDB info into PDBInterference without the
// Disruption message. Used by capacity_plan, which renders its own wording.
func convertPDBsPlain(infos []kube.PDBInfo) []hpaanalysis.PDBInterference {
	if len(infos) == 0 {
		return nil
	}
	result := make([]hpaanalysis.PDBInterference, 0, len(infos))
	for _, p := range infos {
		result = append(result, hpaanalysis.PDBInterference{
			Name:           p.Name,
			MinAvailable:   p.MinAvailable,
			MaxUnavailable: p.MaxUnavailable,
		})
	}
	return result
}

// pdbDisruptionMessage builds the canonical disruption description shown in
// the capacity analysis. Exported logic so both convertPDBs and tests agree.
func pdbDisruptionMessage(p kube.PDBInfo) string {
	switch {
	case p.MinAvailable != "":
		return fmt.Sprintf("minAvailable=%s may delay scale-down during disruptions", p.MinAvailable)
	case p.MaxUnavailable != "":
		return fmt.Sprintf("maxUnavailable=%s may limit concurrent disruptions", p.MaxUnavailable)
	default:
		return "PDB present but no availability constraint specified"
	}
}
