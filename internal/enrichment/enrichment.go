// Package enrichment provides KEDA and VPA enrichment logic for HPA analysis.
// It encapsulates CRD detection, dynamic client creation, and batched
// enrichment operations, decoupled from CLI flag handling.
package enrichment

import (
	"context"
	"fmt"
	"strings"

	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kubeconv"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// Config holds the parameters needed to create an enrichment context.
// This decouples enrichment from the CLI options struct.
type Config struct {
	// Kube carries the full client connection settings (namespace, context,
	// kubeconfig, cluster, rate limits, request timeout) so enrichment
	// clients honor the same tuning flags as the primary typed client.
	Kube kube.Options
	KEDA string // "auto" (default), "on" (force), "off" (disable)
	VPA  string // "auto" (default), "on" (force), "off" (disable)
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
	status      Status
}

// Status returns the enrichment status for diagnostic output.
func (ec *Context) Status() Status {
	if ec == nil {
		return Status{}
	}
	return ec.status
}

// NewContext creates an enrichment context from the given config. It checks
// CRD availability via API discovery and creates a dynamic client only when
// at least one enrichment source is available. Always returns a non-nil Context
// with status populated to explain why enrichment may be unavailable.
func NewContext(_ context.Context, cfg Config) *Context {
	kedaEntry := &Entry{Source: SourceKEDA, State: StateDisabled}
	vpaEntry := &Entry{Source: SourceVPA, State: StateDisabled}
	if requested(cfg.KEDA) {
		kedaEntry.State = StateUnavailable
		kedaEntry.Reason = "not requested"
	}
	if requested(cfg.VPA) {
		vpaEntry.State = StateUnavailable
		vpaEntry.Reason = "not requested"
	}

	status := Status{KEDA: kedaEntry, VPA: vpaEntry}

	if !requested(cfg.KEDA) && !requested(cfg.VPA) {
		kedaEntry.State = StateDisabled
		vpaEntry.State = StateDisabled
		return &Context{status: status}
	}

	disco, err := kube.NewDiscoveryClient(cfg.Kube)
	if err != nil {
		setEnrichmentError(kedaEntry, requested(cfg.KEDA), fmt.Sprintf("discovery client creation failed: %v", err))
		setEnrichmentError(vpaEntry, requested(cfg.VPA), fmt.Sprintf("discovery client creation failed: %v", err))
		return &Context{status: status}
	}

	crdAvail := kube.DetectCRDs(disco)

	kedaEnabled := isEnabled(cfg.KEDA, crdAvail.KEDA)
	vpaEnabled := isEnabled(cfg.VPA, crdAvail.VPA)

	// Surface the real discovery outcome in each Entry.Reason. When discovery
	// itself failed (RBAC denial, network timeout), the wrapped error replaces
	// the misleading hard-coded "CRD ... not found" string so operators see the
	// actual cause. A nil error means the CRD is simply absent.
	applyCRDAvailability(kedaEntry, requested(cfg.KEDA), crdAvail.KEDA, crdReason(crdAvail.KEDError))
	applyCRDAvailability(vpaEntry, requested(cfg.VPA), crdAvail.VPA, crdReason(crdAvail.VPAError))

	if !kedaEnabled && !vpaEnabled {
		return &Context{status: status}
	}

	dynClient, ns, err := kube.NewDynamicClient(cfg.Kube)
	if err != nil {
		setEnrichmentError(kedaEntry, kedaEnabled, fmt.Sprintf("dynamic client creation failed: %v", err))
		setEnrichmentError(vpaEntry, vpaEnabled, fmt.Sprintf("dynamic client creation failed: %v", err))
		return &Context{status: status}
	}

	// Mark enabled sources as available (per-HPA state will be set during enrichment)
	clearEnrichmentReason(kedaEntry, kedaEnabled)
	clearEnrichmentReason(vpaEntry, vpaEnabled)

	return &Context{
		dynClient:   dynClient,
		namespace:   ns,
		crdAvail:    crdAvail,
		kedaEnabled: kedaEnabled,
		vpaEnabled:  vpaEnabled,
		status:      status,
	}
}

// crdReason formats a DetectCRDs per-source error for display in an enrichment
// Status entry. A nil error means the CRD is simply absent, so we keep the
// historical short string; a non-nil error carries the real discovery failure
// (RBAC denial, network timeout, etc.) and is surfaced verbatim so operators
// see the actual cause instead of a misleading "not found".
func crdReason(err error) string {
	if err == nil {
		return "CRD not found in API discovery"
	}
	return err.Error()
}

// setEnrichmentError marks the entry as errored with the given reason when enabled is true.
func setEnrichmentError(entry *Entry, enabled bool, reason string) {
	if !enabled {
		return
	}
	entry.State = StateError
	entry.Reason = reason
}

// applyCRDAvailability records the per-source CRD availability, setting a reason string when the CRD is missing.
func applyCRDAvailability(entry *Entry, requested, available bool, missingReason string) {
	if !requested {
		return
	}
	entry.State = StateUnavailable // will be updated per-HPA
	if !available {
		entry.Reason = missingReason
	}
}

// isEnabled interprets a tri-state mode ("auto"|"on"|"off") against CRD
// presence. "on" forces enablement, "off" disables, "auto" (and any
// unrecognized/empty value) enables only when the CRD is present.
func isEnabled(mode string, crdPresent bool) bool {
	switch mode {
	case "on", "true", "1":
		return true
	case "off", "false", "0", "":
		return false
	default: // "auto" or unrecognized
		return crdPresent
	}
}

// requested reports whether the mode asks for enrichment at all (on or auto),
// as opposed to off/empty which skip discovery entirely. It also accepts the
// legacy bool spellings ("true"/"1") so existing --keda=true invocations keep
// working after the flag became a tri-state string.
func requested(mode string) bool {
	switch mode {
	case "on", "auto", "true", "1":
		return true
	default:
		return false
	}
}

// clearEnrichmentReason resets the entry's reason when enabled (marking it ready for per-HPA updates).
func clearEnrichmentReason(entry *Entry, enabled bool) {
	if !enabled {
		return
	}
	entry.State = StateUnavailable
	entry.Reason = ""
}

// KEDAEnabled reports whether KEDA enrichment is active.
func (ec *Context) KEDAEnabled() bool { return ec != nil && ec.kedaEnabled }

// VPAEnabled reports whether VPA enrichment is active.
func (ec *Context) VPAEnabled() bool { return ec != nil && ec.vpaEnabled }

// buildKEDAAnalysis converts a KEDAInfo into a KEDAAnalysis with trigger
// summaries, condition lines, fallback info, and cross-reference interpretation.
func buildKEDAAnalysis(info kube.KEDAInfo, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpakeda.Analysis {
	triggers := make([]hpakeda.TriggerSummary, 0, len(info.Triggers))
	for _, t := range info.Triggers {
		triggers = append(triggers, hpakeda.TriggerSummary{
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

	var fallback *hpakeda.FallbackInfo
	if info.Fallback != nil {
		fallback = &hpakeda.FallbackInfo{
			FailureThreshold: info.Fallback.FailureThreshold,
			Replicas:         info.Fallback.Replicas,
		}
	}

	kedaAnalysis := &hpakeda.Analysis{
		ScaledObjectName: info.ScaledObjectName,
		Triggers:         triggers,
		PollingInterval:  info.PollingInterval,
		CooldownPeriod:   info.CooldownPeriod,
		MinReplicaCount:  info.MinReplicaCount,
		MaxReplicaCount:  info.MaxReplicaCount,
		IdleReplicaCount: info.IdleReplicaCount,
		Lines:            conditionLines,
		Fallback:         fallback,
	}

	kedaAnalysis.Lines = append(kedaAnalysis.Lines, hpakeda.Analyze(hpa, kedaAnalysis)...)

	return kedaAnalysis
}

// EnrichKEDA performs KEDA ScaledObject enrichment for a single HPA.
// Returns nil if the HPA is not KEDA-managed or enrichment fails.
func EnrichKEDA(ctx context.Context, ec *Context, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpakeda.Analysis {
	det := kube.DetectKEDA(hpa)
	if !det.Managed {
		return nil
	}

	scaledObject, err := kube.FindScaledObjectForHPA(ctx, ec.dynClient, nil, hpa)
	if err != nil {
		return &hpakeda.Analysis{
			Lines: []string{fmt.Sprintf("[observed] HPA appears KEDA-managed but no ScaledObject found: %v", err)},
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

	analysisVPA := convertVPAInfo(vpaInfo)
	report.Analysis.VPAConflict = hpavpa.NewConflictInfo(analysisVPA)
	report.Analysis.Interpretation = append(report.Analysis.Interpretation, hpavpa.Analyze(hpa, analysisVPA)...)
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
	report.Analysis.EnrichmentStatus = ec.status.ToAnalysisStatus()
}

// BatchKEDA performs batched KEDA enrichment for multiple HPAs.
// It lists ScaledObjects once per namespace and matches by scaleTargetRef.
// The returned warnings map records per-namespace list failures (namespace →
// messages) so callers can surface them (e.g. into Analysis.Warnings) instead
// of silently treating a permissions error as "no ScaledObjects found".
func BatchKEDA(ctx context.Context, ec *Context, hpas []autoscalingv2.HorizontalPodAutoscaler) (map[string]*hpakeda.Analysis, map[string][]string) {
	if ec == nil || !ec.kedaEnabled {
		return nil, nil
	}

	namespaces := map[string]bool{}
	for i := range hpas {
		namespaces[hpas[i].Namespace] = true
	}

	warnings := map[string][]string{}
	allScaledObjects := map[string][]*unstructured.Unstructured{}
	for ns := range namespaces {
		soList, err := ec.dynClient.Resource(kube.ScaledObjectGVR()).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			warnings[ns] = append(warnings[ns], fmt.Sprintf("KEDA ScaledObject list failed: %v", err))
			continue
		}
		for i := range soList.Items {
			item := soList.Items[i]
			allScaledObjects[ns] = append(allScaledObjects[ns], &item)
		}
	}

	results := map[string]*hpakeda.Analysis{}
	for i := range hpas {
		hpa := &hpas[i]
		det := kube.DetectKEDA(hpa)
		if !det.Managed {
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
			results[key] = &hpakeda.Analysis{
				Lines: []string{"[observed] HPA appears KEDA-managed but no matching ScaledObject found"},
			}
			continue
		}

		info := kube.ExtractKEDAInfo(scaledObj)
		results[key] = buildKEDAAnalysis(info, hpa)
	}

	return results, warnings
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
			allVPAs[ns] = append(allVPAs[ns], info)
		}
	}

	results := map[string]*hpavpa.ConflictInfo{}
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
				results[hpa.Namespace+"/"+hpa.Name] = hpavpa.NewConflictInfo(convertVPAInfo(&vpa))
				break
			}
		}
	}

	return results, warnings
}

// convertVPAInfo translates the kube-layer VPAInfo DTO into the analysis
// model shape consumed by pkg/hpa analyzers. The internal/kube package must
// not depend on pkg/hpa, so this conversion is centralized in internal/kubeconv
// (kubeconv.VPAInfo); this wrapper keeps the enrichment-internal call sites
// stable while sharing the single canonical mapping.
func convertVPAInfo(vpa *kube.VPAInfo) *hpavpa.Info {
	return kubeconv.VPAInfo(vpa)
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
