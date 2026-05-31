package kube

import (
	"context"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var vpaGVR = schema.GroupVersionResource{
	Group:    "autoscaling.k8s.io",
	Version:  "v1",
	Resource: "verticalpodautoscalers",
}

// VPAInfo holds extracted information about a VerticalPodAutoscaler.
type VPAInfo struct {
	Name       string `json:"name" yaml:"name"`
	TargetRef  string `json:"targetRef" yaml:"targetRef"`   // "Kind/Name"
	TargetKind string `json:"targetKind" yaml:"targetKind"` // e.g. "Deployment"
	TargetName string `json:"targetName" yaml:"targetName"`
	UpdateMode string `json:"updateMode" yaml:"updateMode"` // e.g. "Auto", "Recommender", "Off"
}

// FetchVPAs lists all VPAs in the given namespace using the dynamic client.
func FetchVPAs(ctx context.Context, dynClient dynamic.Interface, namespace string) ([]unstructured.Unstructured, error) {
	list, err := dynClient.Resource(vpaGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list VPAs in namespace %s: %w", namespace, err)
	}
	return list.Items, nil
}

// ExtractVPAInfo parses a VPA unstructured object into VPAInfo.
func ExtractVPAInfo(u *unstructured.Unstructured) VPAInfo {
	if u == nil {
		return VPAInfo{}
	}
	info := VPAInfo{
		Name: u.GetName(),
	}

	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		return info
	}

	// Extract targetRef
	if targetRef, ok := spec["targetRef"].(map[string]any); ok {
		kind := stringValue(targetRef, "kind")
		name := stringValue(targetRef, "name")
		info.TargetKind = kind
		info.TargetName = name
		info.TargetRef = kind + "/" + name
	}

	// Extract updatePolicy.updateMode
	if updatePolicy, ok := spec["updatePolicy"].(map[string]any); ok {
		info.UpdateMode = stringValue(updatePolicy, "updateMode")
	}

	return info
}

// FindConflictingVPA finds VPAs whose targetRef matches the HPA's scaleTargetRef.
// Only returns a VPA when the HPA uses CPU or memory resource metrics.
// Returns nil if no conflicting VPA is found.
func FindConflictingVPA(ctx context.Context, dynClient dynamic.Interface, namespace string, hpa *autoscalingv2.HorizontalPodAutoscaler) (*VPAInfo, error) {
	if !hasResourceMetrics(hpa) {
		return nil, nil
	}

	vpas, err := FetchVPAs(ctx, dynClient, namespace)
	if err != nil {
		return nil, err
	}

	for i := range vpas {
		info := ExtractVPAInfo(&vpas[i])
		if info.TargetKind == hpa.Spec.ScaleTargetRef.Kind && info.TargetName == hpa.Spec.ScaleTargetRef.Name {
			// Skip VPAs in "Off" mode — they only recommend, never apply.
			if info.UpdateMode == "Off" {
				continue
			}
			return &info, nil
		}
	}

	return nil, nil
}

// hasResourceMetrics returns true when the HPA uses CPU or memory resource metrics.
func hasResourceMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ResourceMetricSourceType && m.Resource != nil {
			name := string(m.Resource.Name)
			if name == "cpu" || name == "memory" {
				return true
			}
		}
		if m.Type == autoscalingv2.ContainerResourceMetricSourceType && m.ContainerResource != nil {
			name := string(m.ContainerResource.Name)
			if name == "cpu" || name == "memory" {
				return true
			}
		}
	}
	return false
}
