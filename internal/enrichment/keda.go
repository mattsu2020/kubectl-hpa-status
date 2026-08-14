// Package enrichment provides KEDA and VPA enrichment logic for HPA analysis.
package enrichment

import (
	"context"
	"fmt"
	"strings"

	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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
// Callers that need diagnostic state should use EnrichReport, which preserves
// the distinction between skipped, active, and failed enrichment.
func EnrichKEDA(ctx context.Context, ec *Context, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpakeda.Analysis {
	result, _ := enrichKEDA(ctx, ec, hpa)
	return result
}

func enrichKEDA(ctx context.Context, ec *Context, hpa *autoscalingv2.HorizontalPodAutoscaler) (*hpakeda.Analysis, Entry) {
	entry := Entry{Source: SourceKEDA, State: StateSkipped}
	det := kube.DetectKEDA(hpa)
	if !det.Managed {
		entry.Reason = "HPA is not KEDA-managed"
		return nil, entry
	}
	if ec == nil || ec.dynClient == nil {
		entry.State = StateError
		entry.Reason = "dynamic client is unavailable"
		return nil, entry
	}

	scaledObject, err := kube.FindScaledObjectForHPA(ctx, ec.dynClient, hpa)
	if err != nil {
		entry.State = StateError
		entry.Reason = err.Error()
		return nil, entry
	}

	info := kube.ExtractKEDAInfo(scaledObject)
	entry.State = StateActive
	return buildKEDAAnalysis(info, hpa), entry
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
	indexes, warnings := loadKEDAIndexes(ctx, ec, hpas)
	results := make(map[string]*hpakeda.Analysis)
	for i := range hpas {
		if key, analysis := analyzeKEDAHPA(&hpas[i], indexes); analysis != nil {
			results[key] = analysis
		}
	}
	return results, warnings
}

type kedaIndexes struct {
	byName   map[string]*unstructured.Unstructured
	byTarget map[string][]*unstructured.Unstructured
}

func loadKEDAIndexes(ctx context.Context, ec *Context, hpas []autoscalingv2.HorizontalPodAutoscaler) (kedaIndexes, map[string][]string) {
	namespaces := map[string]bool{}
	for i := range hpas {
		namespaces[hpas[i].Namespace] = true
	}

	warnings := map[string][]string{}
	indexes := kedaIndexes{
		byName:   map[string]*unstructured.Unstructured{},
		byTarget: map[string][]*unstructured.Unstructured{},
	}
	for ns := range namespaces {
		soList, err := kube.FetchScaledObjects(ctx, ec.dynClient, ns)
		if err != nil {
			warnings[ns] = append(warnings[ns], fmt.Sprintf("KEDA ScaledObject list failed: %v", err))
			continue
		}
		for i := range soList {
			item := soList[i]
			indexes.byName[ns+"/"+item.GetName()] = &item
			ref, _, _ := unstructured.NestedMap(item.Object, "spec", "scaleTargetRef")
			kind, _, _ := unstructured.NestedString(ref, "kind")
			name, _, _ := unstructured.NestedString(ref, "name")
			if kind != "" && name != "" {
				key := ns + "/" + kind + "/" + name
				indexes.byTarget[key] = append(indexes.byTarget[key], &item)
			}
		}
	}
	return indexes, warnings
}

func analyzeKEDAHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, indexes kedaIndexes) (string, *hpakeda.Analysis) {
	det := kube.DetectKEDA(hpa)
	if !det.Managed {
		return "", nil
	}

	candidates := indexes.byTarget[hpa.Namespace+"/"+hpa.Spec.ScaleTargetRef.Kind+"/"+hpa.Spec.ScaleTargetRef.Name]
	items := make([]unstructured.Unstructured, 0, len(candidates)+1)
	if named := indexes.byName[hpa.Namespace+"/"+det.Name]; named != nil {
		items = append(items, *named)
	}
	for _, candidate := range candidates {
		if len(items) == 0 || items[0].GetName() != candidate.GetName() {
			items = append(items, *candidate)
		}
	}
	scaledObj, ambiguous := kube.ResolveScaledObjectForHPA(hpa, items)

	key := hpa.Namespace + "/" + hpa.Name
	if scaledObj == nil {
		line := "[observed] HPA appears KEDA-managed but no matching ScaledObject found"
		if ambiguous {
			line = "[observed] multiple ScaledObjects target this workload; ownership is ambiguous"
		}
		return key, &hpakeda.Analysis{Lines: []string{line}}
	}

	return key, buildKEDAAnalysis(kube.ExtractKEDAInfo(scaledObj), hpa)
}
