// Package enrichment provides KEDA and VPA enrichment logic for HPA analysis.
// It encapsulates CRD detection, dynamic client creation, and batched
// enrichment operations, decoupled from CLI flag handling.
package enrichment

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

// Config holds the parameters needed to create an enrichment context.
// This decouples enrichment from the CLI options struct.
type Config struct {
	Namespace  string
	Context    string
	Kubeconfig string
	Cluster    string
	KEDA       bool
	VPA        bool
}

// Context holds reusable clients and CRD availability for enrichment
// operations. Created once per command invocation and shared across all
// HPA processing. Safe for concurrent use after construction because
// dynamic.Interface is goroutine-safe and CRDAvailability is read-only.
type Context struct {
	dynClient   dynamic.Interface
	namespace   string
	crdAvail    kube.CRDAvailability
	kedaEnabled bool
	vpaEnabled  bool
	status      EnrichmentStatus
}

// Status returns the enrichment status for diagnostic output.
func (ec *Context) Status() EnrichmentStatus {
	if ec == nil {
		return EnrichmentStatus{}
	}
	return ec.status
}

// NewContext creates an enrichment context from the given config. It checks
// CRD availability via API discovery and creates a dynamic client only when
// at least one enrichment source is available. Always returns a non-nil Context
// with status populated to explain why enrichment may be unavailable.
func NewContext(_ context.Context, cfg Config) *Context {
	kedaEntry := &EnrichmentEntry{Source: EnrichmentSourceKEDA, State: EnrichmentStateDisabled}
	vpaEntry := &EnrichmentEntry{Source: EnrichmentSourceVPA, State: EnrichmentStateDisabled}
	if cfg.KEDA {
		kedaEntry.State = EnrichmentStateUnavailable
		kedaEntry.Reason = "not requested"
	}
	if cfg.VPA {
		vpaEntry.State = EnrichmentStateUnavailable
		vpaEntry.Reason = "not requested"
	}

	status := EnrichmentStatus{KEDA: kedaEntry, VPA: vpaEntry}

	if !cfg.KEDA && !cfg.VPA {
		kedaEntry.State = EnrichmentStateDisabled
		vpaEntry.State = EnrichmentStateDisabled
		return &Context{status: status}
	}

	disco, err := kube.NewDiscoveryClient(kube.Options{
		Namespace:  cfg.Namespace,
		Context:    cfg.Context,
		Kubeconfig: cfg.Kubeconfig,
		Cluster:    cfg.Cluster,
	})
	if err != nil {
		if cfg.KEDA {
			kedaEntry.State = EnrichmentStateError
			kedaEntry.Reason = fmt.Sprintf("discovery client creation failed: %v", err)
		}
		if cfg.VPA {
			vpaEntry.State = EnrichmentStateError
			vpaEntry.Reason = fmt.Sprintf("discovery client creation failed: %v", err)
		}
		return &Context{status: status}
	}

	crdAvail := kube.DetectCRDs(disco)

	kedaEnabled := cfg.KEDA && crdAvail.KEDA
	vpaEnabled := cfg.VPA && crdAvail.VPA

	if cfg.KEDA {
		if crdAvail.KEDA {
			kedaEntry.State = EnrichmentStateUnavailable // will be updated per-HPA
		} else {
			kedaEntry.State = EnrichmentStateUnavailable
			kedaEntry.Reason = "CRD keda.sh/v1alpha1 not found in API discovery"
		}
	}
	if cfg.VPA {
		if crdAvail.VPA {
			vpaEntry.State = EnrichmentStateUnavailable // will be updated per-HPA
		} else {
			vpaEntry.State = EnrichmentStateUnavailable
			vpaEntry.Reason = "CRD autoscaling.k8s.io/v1 not found in API discovery"
		}
	}

	if !kedaEnabled && !vpaEnabled {
		return &Context{status: status}
	}

	dynClient, ns, err := kube.NewDynamicClient(kube.Options{
		Namespace:  cfg.Namespace,
		Context:    cfg.Context,
		Kubeconfig: cfg.Kubeconfig,
		Cluster:    cfg.Cluster,
	})
	if err != nil {
		if kedaEnabled {
			kedaEntry.State = EnrichmentStateError
			kedaEntry.Reason = fmt.Sprintf("dynamic client creation failed: %v", err)
		}
		if vpaEnabled {
			vpaEntry.State = EnrichmentStateError
			vpaEntry.Reason = fmt.Sprintf("dynamic client creation failed: %v", err)
		}
		return &Context{status: status}
	}

	// Mark enabled sources as available (per-HPA state will be set during enrichment)
	if kedaEnabled {
		kedaEntry.State = EnrichmentStateUnavailable
		kedaEntry.Reason = ""
	}
	if vpaEnabled {
		vpaEntry.State = EnrichmentStateUnavailable
		vpaEntry.Reason = ""
	}

	return &Context{
		dynClient:   dynClient,
		namespace:   ns,
		crdAvail:    crdAvail,
		kedaEnabled: kedaEnabled,
		vpaEnabled:  vpaEnabled,
		status:      status,
	}
}

// KEDAEnabled reports whether KEDA enrichment is active.
func (ec *Context) KEDAEnabled() bool { return ec != nil && ec.kedaEnabled }

// VPAEnabled reports whether VPA enrichment is active.
func (ec *Context) VPAEnabled() bool { return ec != nil && ec.vpaEnabled }

// buildKEDAAnalysis converts a KEDAInfo into a KEDAAnalysis with trigger
// summaries, condition lines, fallback info, and cross-reference interpretation.
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

// EnrichKEDA performs KEDA ScaledObject enrichment for a single HPA.
// Returns nil if the HPA is not KEDA-managed or enrichment fails.
func EnrichKEDA(ctx context.Context, ec *Context, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.KEDAAnalysis {
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

// EnrichVPA performs VPA conflict enrichment for a single HPA.
// Silently skips on any error (CRD absent, client failure, no conflict).
func EnrichVPA(ctx context.Context, ec *Context, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
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

// EnrichReport applies KEDA and VPA enrichment to a StatusReport and
// adjusts the health score with enrichment penalties.
func EnrichReport(ctx context.Context, ec *Context, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, weights hpaanalysis.HealthWeights) {
	if ec == nil || (!ec.kedaEnabled && !ec.vpaEnabled) {
		return
	}

	if ec.kedaEnabled {
		report.Analysis.KEDAInfo = EnrichKEDA(ctx, ec, hpa)
	}

	if ec.vpaEnabled {
		EnrichVPA(ctx, ec, hpa, report)
	}

	if report.Analysis.KEDAInfo != nil || report.Analysis.VPAConflict != nil {
		hpaanalysis.ApplyEnrichmentPenalties(&report.Analysis, weights)
	}

	// Attach enrichment status to analysis for diagnostic output.
	report.Analysis.EnrichmentStatus = ec.status
}

// BatchKEDA performs batched KEDA enrichment for multiple HPAs.
// It lists ScaledObjects once per namespace and matches by scaleTargetRef.
func BatchKEDA(ctx context.Context, ec *Context, hpas []autoscalingv2.HorizontalPodAutoscaler) map[string]*hpaanalysis.KEDAAnalysis {
	if ec == nil || !ec.kedaEnabled {
		return nil
	}

	namespaces := map[string]bool{}
	for i := range hpas {
		namespaces[hpas[i].Namespace] = true
	}

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

// BatchVPA performs batched VPA enrichment for multiple HPAs.
// It lists VPAs once per namespace and matches by targetRef.
func BatchVPA(ctx context.Context, ec *Context, hpas []autoscalingv2.HorizontalPodAutoscaler) map[string]*hpaanalysis.VPAConflictInfo {
	if ec == nil || !ec.vpaEnabled {
		return nil
	}

	namespaces := map[string]bool{}
	for i := range hpas {
		namespaces[hpas[i].Namespace] = true
	}

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
