// Package enrichment provides KEDA and VPA enrichment logic for HPA analysis.
package enrichment

import (
	"context"
	"fmt"

	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kubeconv"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/client-go/dynamic"
)

// EnrichVPA performs VPA conflict enrichment for a single HPA.
// The returned Entry distinguishes no conflict from API/RBAC failures.
func EnrichVPA(ctx context.Context, ec *Context, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) Entry {
	entry := Entry{Source: SourceVPA, State: StateSkipped}
	if ec == nil || ec.dynClient == nil {
		entry.State = StateError
		entry.Reason = "dynamic client is unavailable"
		return entry
	}
	vpaInfo, err := FindConflictingVPA(ctx, ec.dynClient, report.Analysis.Meta.Namespace, hpa)
	if err != nil {
		entry.State = StateError
		entry.Reason = err.Error()
		return entry
	}
	if vpaInfo == nil {
		entry.Reason = "no conflicting VPA found"
		return entry
	}

	analysisVPA := kubeconv.VPAInfo(vpaInfo)
	report.Analysis.Advisory.VPAConflict = hpavpa.NewConflictInfoForHPA(hpa, analysisVPA)
	report.Analysis.Actions.Interpretation = append(report.Analysis.Actions.Interpretation, hpavpa.Analyze(hpa, analysisVPA)...)
	entry.State = StateActive
	return entry
}

// BatchVPA performs batched VPA enrichment for multiple HPAs.
// It lists VPAs once per namespace and matches by targetRef.
// The returned warnings map records per-namespace list failures (namespace →
// messages) so callers can surface them (e.g. into Analysis.Warnings) instead
// of silently treating a permissions error as "no VPAs found".
func BatchVPA(ctx context.Context, ec *Context, hpas []autoscalingv2.HorizontalPodAutoscaler) (map[string]*hpavpa.ConflictInfo, map[string][]string) {
	if ec == nil || !ec.vpaEnabled {
		return nil, nil
	}

	namespaces := map[string]bool{}
	for i := range hpas {
		namespaces[hpas[i].Namespace] = true
	}

	warnings := map[string][]string{}
	allVPAs := map[string][]kube.VPAInfo{}
	for ns := range namespaces {
		vpaList, err := kube.FetchVPAs(ctx, ec.dynClient, ns)
		if err != nil {
			warnings[ns] = append(warnings[ns], fmt.Sprintf("VPA list failed: %v", err))
			continue
		}
		for i := range vpaList {
			info := kube.ExtractVPAInfo(&vpaList[i])
			key := ns + "/" + info.TargetKind + "/" + info.TargetName
			allVPAs[key] = append(allVPAs[key], info)
		}
	}

	results := map[string]*hpavpa.ConflictInfo{}
	for i := range hpas {
		hpa := &hpas[i]

		key := hpa.Namespace + "/" + hpa.Spec.ScaleTargetRef.Kind + "/" + hpa.Spec.ScaleTargetRef.Name
		for _, vpa := range allVPAs[key] {
			analysisVPA := kubeconv.VPAInfo(&vpa)
			if hpavpa.ConflictsWithHPA(hpa, analysisVPA) {
				results[hpa.Namespace+"/"+hpa.Name] = hpavpa.NewConflictInfoForHPA(hpa, analysisVPA)
				break
			}
		}
	}

	return results, warnings
}

// FindConflictingVPA keeps API access in the enrichment boundary while the
// conflict predicate itself remains in the public VPA domain package.
//
//nolint:nilnil // nil result with no error means no conflicting VPA exists.
func FindConflictingVPA(ctx context.Context, dynClient dynamic.Interface, namespace string, hpa *autoscalingv2.HorizontalPodAutoscaler) (*kube.VPAInfo, error) {
	vpas, err := kube.FetchVPAs(ctx, dynClient, namespace)
	if err != nil {
		return nil, err
	}
	for i := range vpas {
		info := kube.ExtractVPAInfo(&vpas[i])
		if hpavpa.ConflictsWithHPA(hpa, kubeconv.VPAInfo(&info)) {
			return &info, nil
		}
	}
	return nil, nil
}
