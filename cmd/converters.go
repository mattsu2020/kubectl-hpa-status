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

// convertResourceRequests translates the kube-layer ResourceRequests DTO into
// the analysis model shape consumed by pkg/hpa.CheckResourceConsistency.
// Returns nil when the input is nil so optional analysis fields stay unset.
func convertResourceRequests(rr *kube.ResourceRequests) *hpaanalysis.ResourceRequests {
	if rr == nil {
		return nil
	}
	out := &hpaanalysis.ResourceRequests{
		Containers: make([]hpaanalysis.ContainerResources, 0, len(rr.Containers)),
	}
	for _, c := range rr.Containers {
		out.Containers = append(out.Containers, hpaanalysis.ContainerResources{
			Name:     c.Name,
			Requests: cloneStringMap(c.Requests),
			Limits:   cloneStringMap(c.Limits),
		})
	}
	return out
}

// cloneStringMap returns a shallow copy of m, or nil when m is empty so the
// analysis struct keeps its omitempty semantics.
func cloneStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
