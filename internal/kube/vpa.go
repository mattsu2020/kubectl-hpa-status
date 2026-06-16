package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

// VPAInfo holds the parsed fields of a VerticalPodAutoscaler relevant to HPA
// conflict analysis. This is a kube-layer DTO; callers in cmd/ convert it to
// the analysis model (pkg/hpa.VPAInfo).
type VPAInfo struct {
	Name                string                  `json:"name" yaml:"name"`
	TargetRef           string                  `json:"targetRef" yaml:"targetRef"`
	TargetKind          string                  `json:"targetKind" yaml:"targetKind"`
	TargetName          string                  `json:"targetName" yaml:"targetName"`
	UpdateMode          string                  `json:"updateMode" yaml:"updateMode"`
	ControlledResources []string                `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	Recommendations     []VPARecommendationInfo `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
}

// VPARecommendationInfo captures the visible recommendation values for one
// container/resource pair, as extracted from a VPA object. This is a
// kube-layer DTO.
type VPARecommendationInfo struct {
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Target    string `json:"target,omitempty" yaml:"target,omitempty"`
	Lower     string `json:"lower,omitempty" yaml:"lower,omitempty"`
	Upper     string `json:"upper,omitempty" yaml:"upper,omitempty"`
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
	info.ControlledResources = extractControlledResources(spec)
	info.Recommendations = extractVPARecommendations(u)

	return info
}

func extractControlledResources(spec map[string]any) []string {
	resourceSet := map[string]bool{}
	resourcePolicy, ok := spec["resourcePolicy"].(map[string]any)
	if !ok {
		return nil
	}
	containerPolicies, ok := resourcePolicy["containerPolicies"].([]any)
	if !ok {
		return nil
	}
	for _, item := range containerPolicies {
		policy, ok := item.(map[string]any)
		if !ok {
			continue
		}
		resources, ok := policy["controlledResources"].([]any)
		if !ok {
			continue
		}
		for _, resource := range resources {
			name := strings.ToLower(fmt.Sprint(resource))
			if name != "" {
				resourceSet[name] = true
			}
		}
	}
	var resources []string
	for resource := range resourceSet {
		resources = append(resources, resource)
	}
	sort.Strings(resources)
	return resources
}

func extractVPARecommendations(u *unstructured.Unstructured) []VPARecommendationInfo {
	status, ok := u.Object["status"].(map[string]any)
	if !ok {
		return nil
	}
	recommendation, ok := status["recommendation"].(map[string]any)
	if !ok {
		return nil
	}
	containerRecommendations, ok := recommendation["containerRecommendations"].([]any)
	if !ok {
		return nil
	}
	var out []VPARecommendationInfo
	for _, item := range containerRecommendations {
		rec, ok := item.(map[string]any)
		if !ok {
			continue
		}
		container := stringValue(rec, "containerName")
		target := resourceMap(rec, "target")
		lower := resourceMap(rec, "lowerBound")
		upper := resourceMap(rec, "upperBound")
		for _, resource := range []string{"cpu", "memory"} {
			info := VPARecommendationInfo{
				Container: container,
				Resource:  resource,
				Target:    target[resource],
				Lower:     lower[resource],
				Upper:     upper[resource],
			}
			if info.Target != "" || info.Lower != "" || info.Upper != "" {
				out = append(out, info)
			}
		}
	}
	return out
}

func resourceMap(parent map[string]any, field string) map[string]string {
	values := map[string]string{}
	raw, ok := parent[field].(map[string]any)
	if !ok {
		return values
	}
	for key, value := range raw {
		values[strings.ToLower(key)] = fmt.Sprint(value)
	}
	return values
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
