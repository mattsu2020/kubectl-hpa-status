package cmd

import (
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// This file centralizes the type-conversion helpers that translate the
// internal/kube data shapes (fetched from the Kubernetes API) into the
// pkg/hpa analysis shapes used by renderers. Several subcommands previously
// carried near-identical copies of these conversions; they are consolidated
// here to keep the mapping in one place.

// convertPendingPodInfos translates internal PendingPodDetail records into
// the PendingPodInfo shape consumed by capacity/autoscaler-map analysis.
// Returns nil for an empty input so optional analysis fields stay unset.
func convertPendingPodInfos(details []kube.PendingPodDetail) []hpaanalysis.PendingPodInfo {
	if len(details) == 0 {
		return nil
	}
	result := make([]hpaanalysis.PendingPodInfo, 0, len(details))
	for _, d := range details {
		result = append(result, hpaanalysis.PendingPodInfo{
			Name:          d.Name,
			Phase:         "Pending",
			Unschedulable: d.Unschedulable,
			Reasons:       d.Reasons,
		})
	}
	return result
}
