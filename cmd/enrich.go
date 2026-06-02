package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// enrichmentContext holds reusable clients and CRD availability for
// enrichment operations. Created once per command invocation and shared
// across all HPA processing. Safe for concurrent use after construction
// because dynamic.Interface is goroutine-safe and CRDAvailability is
// read-only.
type enrichmentContext struct {
	dynClient   dynamic.Interface
	namespace   string
	crdAvail    kube.CRDAvailability
	kedaEnabled bool
	vpaEnabled  bool
}

// newEnrichmentContext creates an enrichment context. It checks CRD
// availability via API discovery and creates a dynamic client only
// when at least one enrichment source is available. Returns nil when
// both --keda and --vpa are false or when neither CRD is installed.
func newEnrichmentContext(ctx context.Context, opts *options) *enrichmentContext {
	if !opts.keda && !opts.vpa {
		return nil
	}

	disco, err := kube.NewDiscoveryClient(kube.Options{
		Namespace:  opts.namespace,
		Context:    opts.contextName,
		Kubeconfig: opts.kubeconfig,
		Cluster:    opts.cluster,
	})
	if err != nil {
		return nil
	}

	crdAvail := kube.DetectCRDs(disco)

	kedaEnabled := opts.keda && crdAvail.KEDA
	vpaEnabled := opts.vpa && crdAvail.VPA

	if !kedaEnabled && !vpaEnabled {
		return nil
	}

	dynClient, ns, err := kube.NewDynamicClient(kube.Options{
		Namespace:  opts.namespace,
		Context:    opts.contextName,
		Kubeconfig: opts.kubeconfig,
		Cluster:    opts.cluster,
	})
	if err != nil {
		return nil
	}

	return &enrichmentContext{
		dynClient:   dynClient,
		namespace:   ns,
		crdAvail:    crdAvail,
		kedaEnabled: kedaEnabled,
		vpaEnabled:  vpaEnabled,
	}
}

// buildKEDAAnalysis converts a KEDAInfo into a KEDAAnalysis with trigger
// summaries, condition lines, fallback info, and cross-reference interpretation.
// Shared by both single-HPA and batched enrichment paths.
func buildKEDAAnalysis(info kube.KEDAInfo, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.KEDAAnalysis {
	triggers := make([]hpaanalysis.KEDATriggerSummary, 0, len(info.Triggers))
	for _, t := range info.Triggers {
		triggers = append(triggers, hpaanalysis.KEDATriggerSummary{
			Type:         t.Type,
			Name:         t.Name,
			Status:       t.Status,
			Message:      t.Message,
			MetricName:   t.MetricName,
			Threshold:    t.Threshold,
			CurrentValue: t.CurrentValue,
			AuthRef:      t.AuthenticationRef,
		})
	}

	var conditionLines []string
	for _, c := range info.Conditions {
		if strings.EqualFold(c.Status, "False") {
			conditionLines = append(conditionLines, fmt.Sprintf("condition %q is False (reason: %s): %s", c.Type, c.Reason, c.Message))
		}
	}

	if len(conditionLines) == 0 && len(info.Conditions) > 0 {
		conditionLines = []string{fmt.Sprintf("ScaledObject reports %d condition(s), all healthy.", len(info.Conditions))}
	}

	var fallback *hpaanalysis.KEDAFallbackInfo
	if info.Fallback != nil {
		fallback = &hpaanalysis.KEDAFallbackInfo{
			FailureThreshold: info.Fallback.FailureThreshold,
			Replicas:         info.Fallback.Replicas,
		}
	}

	kedaAnalysis := &hpaanalysis.KEDAAnalysis{
		ScaledObjectName: info.ScaledObjectName,
		Triggers:         triggers,
		PollingInterval:  info.PollingInterval,
		CooldownPeriod:   info.CooldownPeriod,
		MinReplicaCount:  info.MinReplicaCount,
		MaxReplicaCount:  info.MaxReplicaCount,
		Lines:            conditionLines,
		Fallback:         fallback,
	}

	kedaAnalysis.Lines = append(kedaAnalysis.Lines, hpaanalysis.AnalyzeKEDA(hpa, kedaAnalysis)...)

	return kedaAnalysis
}

// enrichKEDA performs KEDA ScaledObject enrichment for a single HPA.
// Returns nil if the HPA is not KEDA-managed or enrichment fails.
func enrichKEDA(ctx context.Context, ec *enrichmentContext, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.KEDAAnalysis {
	isKEDA, _ := kube.DetectKEDA(hpa)
	if !isKEDA {
		return nil
	}

	scaledObject, err := kube.FindScaledObjectForHPA(ctx, ec.dynClient, nil, hpa)
	if err != nil {
		return &hpaanalysis.KEDAAnalysis{
			Lines: []string{fmt.Sprintf("[confidence: high] HPA appears KEDA-managed but no ScaledObject found: %v", err)},
		}
	}

	info := kube.ExtractKEDAInfo(scaledObject)
	return buildKEDAAnalysis(info, hpa)
}

// enrichVPA performs VPA conflict enrichment for a single HPA.
// Silently skips on any error (CRD absent, client failure, no conflict).
func enrichVPA(ctx context.Context, ec *enrichmentContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	vpaInfo, err := kube.FindConflictingVPA(ctx, ec.dynClient, report.Analysis.Namespace, hpa)
	if err != nil {
		return
	}
	if vpaInfo == nil {
		return
	}

	report.Analysis.VPAConflict = hpaanalysis.NewVPAConflictInfo(vpaInfo)
	report.Analysis.Interpretation = append(report.Analysis.Interpretation, hpaanalysis.AnalyzeVPA(hpa, vpaInfo)...)
}

// enrichReport applies KEDA and VPA enrichment to a StatusReport and
// adjusts the health score with enrichment penalties.
func enrichReport(ctx context.Context, ec *enrichmentContext, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, weights hpaanalysis.HealthWeights) {
	if ec == nil {
		return
	}

	if ec.kedaEnabled {
		report.Analysis.KEDAInfo = enrichKEDA(ctx, ec, hpa)
	}

	if ec.vpaEnabled {
		enrichVPA(ctx, ec, hpa, report)
	}

	if report.Analysis.KEDAInfo != nil || report.Analysis.VPAConflict != nil {
		hpaanalysis.ApplyEnrichmentPenalties(&report.Analysis, weights)
	}
}

// enrichListKEDA performs batched KEDA enrichment for multiple HPAs.
// It lists ScaledObjects once per namespace and matches by scaleTargetRef.
func enrichListKEDA(ctx context.Context, ec *enrichmentContext, hpas []autoscalingv2.HorizontalPodAutoscaler) map[string]*hpaanalysis.KEDAAnalysis {
	if ec == nil || !ec.kedaEnabled {
		return nil
	}

	// Collect unique namespaces for batched queries.
	namespaces := map[string]bool{}
	for i := range hpas {
		namespaces[hpas[i].Namespace] = true
	}

	// Fetch all ScaledObjects per namespace.
	allScaledObjects := map[string][]*unstructured.Unstructured{}
	for ns := range namespaces {
		soList, err := ec.dynClient.Resource(kube.ScaledObjectGVR()).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for i := range soList.Items {
			item := soList.Items[i]
			allScaledObjects[ns] = append(allScaledObjects[ns], &item)
		}
	}

	results := map[string]*hpaanalysis.KEDAAnalysis{}
	for i := range hpas {
		hpa := &hpas[i]
		isKEDA, _ := kube.DetectKEDA(hpa)
		if !isKEDA {
			continue
		}

		// Find matching ScaledObject from pre-fetched list.
		var scaledObj *unstructured.Unstructured
		for _, so := range allScaledObjects[hpa.Namespace] {
			if scaledObjectMatchesHPA(so, hpa) {
				scaledObj = so
				break
			}
		}

		key := hpa.Namespace + "/" + hpa.Name
		if scaledObj == nil {
			results[key] = &hpaanalysis.KEDAAnalysis{
				Lines: []string{"[confidence: high] HPA appears KEDA-managed but no matching ScaledObject found"},
			}
			continue
		}

		info := kube.ExtractKEDAInfo(scaledObj)
		results[key] = buildKEDAAnalysis(info, hpa)
	}

	return results
}

// enrichListVPA performs batched VPA enrichment for multiple HPAs.
// It lists VPAs once per namespace and matches by targetRef.
func enrichListVPA(ctx context.Context, ec *enrichmentContext, hpas []autoscalingv2.HorizontalPodAutoscaler) map[string]*hpaanalysis.VPAConflictInfo {
	if ec == nil || !ec.vpaEnabled {
		return nil
	}

	// Collect unique namespaces for batched queries.
	namespaces := map[string]bool{}
	for i := range hpas {
		namespaces[hpas[i].Namespace] = true
	}

	// Fetch all VPAs per namespace.
	allVPAs := map[string][]kube.VPAInfo{}
	for ns := range namespaces {
		vpaList, err := kube.FetchVPAs(ctx, ec.dynClient, ns)
		if err != nil {
			continue
		}
		for i := range vpaList {
			info := kube.ExtractVPAInfo(&vpaList[i])
			allVPAs[ns] = append(allVPAs[ns], info)
		}
	}

	results := map[string]*hpaanalysis.VPAConflictInfo{}
	for i := range hpas {
		hpa := &hpas[i]

		// Only check HPAs that use resource metrics.
		if !hasHPAResourceMetrics(hpa) {
			continue
		}

		for _, vpa := range allVPAs[hpa.Namespace] {
			if vpa.UpdateMode == "Off" {
				continue
			}
			if vpaTargetMatchesHPA(vpa, hpa) {
				results[hpa.Namespace+"/"+hpa.Name] = hpaanalysis.NewVPAConflictInfo(&vpa)
				break
			}
		}
	}

	return results
}

// scaledObjectMatchesHPA checks if a ScaledObject's scaleTargetRef
// matches the HPA's scaleTargetRef.
func scaledObjectMatchesHPA(so *unstructured.Unstructured, hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	ref, _, _ := unstructured.NestedMap(so.Object, "spec", "scaleTargetRef")
	if len(ref) == 0 {
		return false
	}

	soKind, _, _ := unstructured.NestedString(ref, "kind")
	soName, _, _ := unstructured.NestedString(ref, "name")
	if soKind == "" || soName == "" {
		return false
	}

	return hpa.Spec.ScaleTargetRef.Kind == soKind && hpa.Spec.ScaleTargetRef.Name == soName
}

// vpaTargetMatchesHPA checks if a VPA's targetRef matches the HPA's scaleTargetRef.
func vpaTargetMatchesHPA(vpa kube.VPAInfo, hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	return vpa.TargetKind == hpa.Spec.ScaleTargetRef.Kind &&
		vpa.TargetName == hpa.Spec.ScaleTargetRef.Name
}

// hasHPAResourceMetrics returns true if the HPA uses CPU or memory resource metrics.
func hasHPAResourceMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType {
			return true
		}
	}
	return false
}
