package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"

	"github.com/mattsu2020/kubectl-hpa-status/internal/enrichment"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type conflictScanReport struct {
	Items []conflictItem `json:"items" yaml:"items"`
}

type conflictItem struct {
	Namespace string   `json:"namespace" yaml:"namespace"`
	Target    string   `json:"target" yaml:"target"`
	HPAs      []string `json:"hpas,omitempty" yaml:"hpas,omitempty"`
	Risks     []string `json:"risks" yaml:"risks"`
	Evidence  []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
}

func runConflictScan(ctx context.Context, out io.Writer, opts *options) error {
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}
	namespace := client.Namespace
	if opts.AllNamespaces {
		namespace = metav1.NamespaceAll
	}
	hpas, err := client.ListHPAs(ctx, namespace, metav1.ListOptions{LabelSelector: opts.Selector}, opts.ChunkSize)
	if err != nil {
		return fmt.Errorf("failed to list HPAs: %w", err)
	}

	report := conflictScanReport{Items: detectHPAConflicts(ctx, opts, hpas.Items)}
	return writeConflictScanReport(out, opts, report)
}

func detectHPAConflicts(ctx context.Context, opts *options, hpas []autoscalingv2.HorizontalPodAutoscaler) []conflictItem {
	byTarget := map[string][]autoscalingv2.HorizontalPodAutoscaler{}
	for i := range hpas {
		key := hpaTargetKey(&hpas[i])
		byTarget[key] = append(byTarget[key], hpas[i])
	}

	itemsByKey := map[string]*conflictItem{}
	for key, group := range byTarget {
		if len(group) < 2 {
			continue
		}
		item := ensureConflictItem(itemsByKey, key, group[0].Namespace, targetLabel(&group[0]))
		for i := range group {
			appendUnique(&item.HPAs, group[i].Name)
		}
		appendUnique(&item.Risks, "multiple HPAs target the same scale subresource")
		appendUnique(&item.Evidence, fmt.Sprintf("%d HPAs reference %s", len(group), targetLabel(&group[0])))
	}

	for i := range hpas {
		hpa := &hpas[i]
		if det := kube.DetectKEDA(hpa); det.Managed {
			key := hpaTargetKey(hpa)
			item := ensureConflictItem(itemsByKey, key, hpa.Namespace, targetLabel(hpa))
			appendUnique(&item.HPAs, hpa.Name)
			appendUnique(&item.Risks, "KEDA-managed HPA should be changed through the ScaledObject, not by manual HPA edits")
			if det.Name != "" {
				appendUnique(&item.Evidence, "KEDA ScaledObject: "+det.Name)
			} else {
				appendUnique(&item.Evidence, "KEDA ownership inferred from labels, annotations, or name")
			}
		}
	}

	if conflictScanNeedsVPA(hpas) {
		for key, vpa := range conflictVPAResults(ctx, opts, hpas) {
			if vpa == nil {
				continue
			}
			hpa := findHPAByNamespacedName(hpas, key)
			if hpa == nil {
				continue
			}
			item := ensureConflictItem(itemsByKey, hpaTargetKey(hpa), hpa.Namespace, targetLabel(hpa))
			appendUnique(&item.HPAs, hpa.Name)
			appendUnique(&item.Risks, "HPA and VPA both influence CPU or memory scaling for the same workload")
			appendUnique(&item.Evidence, fmt.Sprintf("VPA %s updateMode=%s", vpa.VPAName, vpa.UpdateMode))
		}
	}

	items := make([]conflictItem, 0, len(itemsByKey))
	for _, item := range itemsByKey {
		sort.Strings(item.HPAs)
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].Target < items[j].Target
	})
	return items
}

func conflictVPAResults(ctx context.Context, opts *options, hpas []autoscalingv2.HorizontalPodAutoscaler) map[string]*hpavpa.ConflictInfo {
	ec := enrichment.NewContext(ctx, enrichment.Config{
		Kube: opts.KubeOptions(),
		VPA:  "on",
	})
	results, _ := enrichment.BatchVPA(ctx, ec, hpas)
	return results
}

func conflictScanNeedsVPA(hpas []autoscalingv2.HorizontalPodAutoscaler) bool {
	for i := range hpas {
		for _, metric := range hpas[i].Spec.Metrics {
			if metric.Type == autoscalingv2.ResourceMetricSourceType && metric.Resource != nil {
				name := string(metric.Resource.Name)
				if name == "cpu" || name == "memory" {
					return true
				}
			}
			if metric.Type == autoscalingv2.ContainerResourceMetricSourceType && metric.ContainerResource != nil {
				name := string(metric.ContainerResource.Name)
				if name == "cpu" || name == "memory" {
					return true
				}
			}
		}
	}
	return false
}

func writeConflictScanReport(out io.Writer, opts *options, report conflictScanReport) error {
	format, _ := selectOutputFromOptions(opts)
	switch format {
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	case "yaml":
		data, err := yaml.Marshal(report)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	default:
		if len(report.Items) == 0 {
			_, err := fmt.Fprintln(out, "No HPA controller conflicts detected.")
			return err
		}
		_, _ = fmt.Fprintln(out, "Conflicts:")
		for _, item := range report.Items {
			_, _ = fmt.Fprintf(out, "  %s/%s\n", item.Namespace, targetNameOnly(item.Target))
			_, _ = fmt.Fprintf(out, "    target: %s\n", item.Target)
			if len(item.HPAs) > 0 {
				_, _ = fmt.Fprintln(out, "    HPAs:")
				for _, hpa := range item.HPAs {
					_, _ = fmt.Fprintf(out, "      - %s\n", hpa)
				}
			}
			for _, risk := range item.Risks {
				_, _ = fmt.Fprintf(out, "    Risk: %s\n", risk)
			}
			for _, evidence := range item.Evidence {
				_, _ = fmt.Fprintf(out, "    Evidence: %s\n", evidence)
			}
		}
		return nil
	}
}

func hpaTargetKey(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	ref := hpa.Spec.ScaleTargetRef
	return hpa.Namespace + "/" + ref.Kind + "/" + ref.Name
}

func targetLabel(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	ref := hpa.Spec.ScaleTargetRef
	return ref.Kind + "/" + ref.Name
}

func targetNameOnly(target string) string {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return target
}

func ensureConflictItem(items map[string]*conflictItem, key, namespace, target string) *conflictItem {
	if item, ok := items[key]; ok {
		return item
	}
	item := &conflictItem{Namespace: namespace, Target: target}
	items[key] = item
	return item
}

func findHPAByNamespacedName(hpas []autoscalingv2.HorizontalPodAutoscaler, key string) *autoscalingv2.HorizontalPodAutoscaler {
	for i := range hpas {
		if hpas[i].Namespace+"/"+hpas[i].Name == key {
			return &hpas[i]
		}
	}
	return nil
}

func appendUnique(values *[]string, value string) {
	for _, existing := range *values {
		if existing == value {
			return
		}
	}
	*values = append(*values, value)
}
