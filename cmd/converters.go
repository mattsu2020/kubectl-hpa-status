package cmd

import (
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kubeconv"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// This file is a thin facade that re-exports the kubeconv package under the
// unexported names the rest of cmd/ uses. The conversion logic lives in
// internal/kubeconv so it can be reused by other packages without depending
// on cmd/. When the cmd/ sub-package split lands, callers should migrate to
// kubeconv.* directly and this file can be deleted.

func convertPendingPodInfos(details []kube.PendingPodDetail) []hpaanalysis.PendingPodInfo {
	return kubeconv.PendingPodInfos(details)
}

func convertToBlockerPodInfos(details []kube.PendingPodDetail) []hpaanalysis.BlockerPodInfo {
	return kubeconv.ToBlockerPodInfos(details)
}

func convertResourceRequests(rr *kube.ResourceRequests) *hpaanalysis.ResourceRequests {
	return kubeconv.ResourceRequests(rr)
}

func convertQuotas(infos []kube.QuotaInfo) []hpaanalysis.QuotaConstraint {
	return kubeconv.Quotas(infos)
}

func convertQuotaDetail[T any](infos []kube.QuotaInfo, build func(kube.QuotaInfo) T) []T {
	return kubeconv.QuotaDetail(infos, build)
}

func convertPDBs(infos []kube.PDBInfo) []hpaanalysis.PDBInterference {
	return kubeconv.PDBs(infos)
}

func convertPDBsPlain(infos []kube.PDBInfo) []hpaanalysis.PDBInterference {
	return kubeconv.PDBsPlain(infos)
}

func pdbDisruptionMessage(p kube.PDBInfo) string {
	return kubeconv.PDBDisruptionMessage(p)
}
