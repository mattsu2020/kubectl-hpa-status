package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
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
	TargetAPIVersion    string                  `json:"targetApiVersion,omitempty" yaml:"targetApiVersion,omitempty"`
	TargetKind          string                  `json:"targetKind" yaml:"targetKind"`
	TargetName          string                  `json:"targetName" yaml:"targetName"`
	UpdateMode          string                  `json:"updateMode" yaml:"updateMode"`
	ControlledResources []string                `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	ContainerPolicies   []VPAContainerPolicy    `json:"containerPolicies,omitempty" yaml:"containerPolicies,omitempty"`
	Recommendations     []VPARecommendationInfo `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
}

// VPAContainerPolicy preserves the per-container policy semantics required for
// conflict detection. ControlledResourcesSpecified distinguishes an omitted
// field (defaults to cpu+memory) from an explicitly empty list.
type VPAContainerPolicy struct {
	ContainerName                string   `json:"containerName" yaml:"containerName"`
	Mode                         string   `json:"mode,omitempty" yaml:"mode,omitempty"`
	ControlledResources          []string `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	ControlledResourcesSpecified bool     `json:"-" yaml:"-"`
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
	items, err := collectListPages(ctx, metav1.ListOptions{}, func(ctx context.Context, page metav1.ListOptions) ([]unstructured.Unstructured, string, error) {
		list, err := dynClient.Resource(vpaGVR).Namespace(namespace).List(ctx, page)
		if err != nil {
			return nil, "", err
		}
		return list.Items, list.GetContinue(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list VPAs in namespace %s: %w", namespace, err)
	}
	return items, nil
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
		info.TargetAPIVersion = stringValue(targetRef, "apiVersion")
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
	info.ContainerPolicies = extractContainerPolicies(spec)
	info.ControlledResources = aggregateControlledResources(info.ContainerPolicies)
	info.Recommendations = extractVPARecommendations(u)

	return info
}

func extractContainerPolicies(spec map[string]any) []VPAContainerPolicy {
	resourcePolicy, ok := spec["resourcePolicy"].(map[string]any)
	if !ok {
		return nil
	}
	containerPolicies, ok := resourcePolicy["containerPolicies"].([]any)
	if !ok {
		return nil
	}
	policies := make([]VPAContainerPolicy, 0, len(containerPolicies))
	for _, item := range containerPolicies {
		policy, ok := item.(map[string]any)
		if !ok {
			continue
		}
		extracted := VPAContainerPolicy{
			ContainerName: stringValue(policy, "containerName"),
			Mode:          stringValue(policy, "mode"),
		}
		rawResources, specified := policy["controlledResources"]
		extracted.ControlledResourcesSpecified = specified
		if resources, ok := rawResources.([]any); ok {
			for _, resource := range resources {
				name := strings.ToLower(fmt.Sprint(resource))
				if name != "" {
					extracted.ControlledResources = append(extracted.ControlledResources, name)
				}
			}
			sort.Strings(extracted.ControlledResources)
		}
		policies = append(policies, extracted)
	}
	return policies
}

func aggregateControlledResources(policies []VPAContainerPolicy) []string {
	if len(policies) == 0 {
		return nil
	}
	resourceSet := map[string]bool{}
	for _, policy := range policies {
		if strings.EqualFold(policy.Mode, "Off") {
			continue
		}
		for _, resource := range effectivePolicyResources(policy) {
			resourceSet[resource] = true
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

// VPAConflictsWithHPA reports whether an active VPA and an HPA control the
// same CPU or memory resource on the same scale target. VPAs in Off mode are
// recommendation-only and therefore do not conflict. When controlledResources
// is omitted, VPA defaults it to CPU and memory.
func VPAConflictsWithHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, vpa *VPAInfo) bool {
	if hpa == nil || vpa == nil || strings.EqualFold(vpa.UpdateMode, "Off") {
		return false
	}
	if vpa.TargetKind != hpa.Spec.ScaleTargetRef.Kind ||
		vpa.TargetName != hpa.Spec.ScaleTargetRef.Name {
		return false
	}
	if vpa.TargetAPIVersion != "" &&
		hpa.Spec.ScaleTargetRef.APIVersion != "" &&
		!strings.EqualFold(vpa.TargetAPIVersion, hpa.Spec.ScaleTargetRef.APIVersion) {
		return false
	}
	return len(VPAConflictResources(hpa, vpa)) > 0
}

// FindConflictingVPA finds VPAs whose targetRef matches the HPA's scaleTargetRef.
// Only returns a VPA when the HPA uses CPU or memory resource metrics.
// Returns (nil, nil) when the HPA has no resource metrics or no conflicting
// VPA exists; callers must check for a nil pointer before use.
//
//nolint:nilnil // nil result with no error means "no conflicting VPA"
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
		if VPAConflictsWithHPA(hpa, &info) {
			return &info, nil
		}
	}

	return nil, nil
}

func vpaControlsHPAResource(hpa *autoscalingv2.HorizontalPodAutoscaler, controlledResources []string) bool {
	if hpa == nil {
		return false
	}
	controlled := make(map[string]struct{}, len(controlledResources))
	if len(controlledResources) == 0 {
		// VPA defaults controlledResources to cpu and memory when omitted.
		controlled[string(corev1.ResourceCPU)] = struct{}{}
		controlled[string(corev1.ResourceMemory)] = struct{}{}
	} else {
		for _, name := range controlledResources {
			controlled[strings.ToLower(name)] = struct{}{}
		}
	}
	for _, metric := range hpa.Spec.Metrics {
		name, ok := hpaResourceMetricName(metric)
		if !ok || !isVPAControlledResource(name) {
			continue
		}
		if _, ok := controlled[name]; ok {
			return true
		}
	}
	return false
}

// VPAConflictResources returns the unique CPU/memory resources for which the
// HPA metric and effective VPA container policy overlap.
func VPAConflictResources(hpa *autoscalingv2.HorizontalPodAutoscaler, vpa *VPAInfo) []string {
	if hpa == nil || vpa == nil || strings.EqualFold(vpa.UpdateMode, "Off") {
		return nil
	}
	conflicts := map[string]struct{}{}
	for _, metric := range hpa.Spec.Metrics {
		resource, container, aggregate, ok := hpaResourceMetric(metric)
		if !ok || !isVPAControlledResource(resource) {
			continue
		}
		var controlled bool
		switch {
		case len(vpa.ContainerPolicies) == 0:
			controlled = flatResourcesControl(vpa.ControlledResources, resource)
		case aggregate:
			controlled = policiesControlAggregateResource(vpa.ContainerPolicies, resource)
		default:
			controlled = policiesControlContainerResource(vpa.ContainerPolicies, container, resource)
		}
		if controlled {
			conflicts[resource] = struct{}{}
		}
	}
	resources := make([]string, 0, len(conflicts))
	for resource := range conflicts {
		resources = append(resources, resource)
	}
	sort.Strings(resources)
	return resources
}

func hpaResourceMetric(metric autoscalingv2.MetricSpec) (resource, container string, aggregate, ok bool) {
	switch metric.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource != nil && isUtilizationMetricTarget(metric.Resource.Target) {
			return strings.ToLower(string(metric.Resource.Name)), "", true, true
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if metric.ContainerResource != nil && isUtilizationMetricTarget(metric.ContainerResource.Target) {
			return strings.ToLower(string(metric.ContainerResource.Name)), metric.ContainerResource.Container, false, true
		}
	}
	return "", "", false, false
}

func flatResourcesControl(resources []string, wanted string) bool {
	if len(resources) == 0 {
		return isVPAControlledResource(wanted)
	}
	for _, resource := range resources {
		if strings.EqualFold(resource, wanted) {
			return true
		}
	}
	return false
}

func effectivePolicyResources(policy VPAContainerPolicy) []string {
	if !policy.ControlledResourcesSpecified {
		return []string{string(corev1.ResourceCPU), string(corev1.ResourceMemory)}
	}
	return policy.ControlledResources
}

func policyControlsResource(policy VPAContainerPolicy, resource string) bool {
	if strings.EqualFold(policy.Mode, "Off") {
		return false
	}
	return flatResourcesControlSpecified(effectivePolicyResources(policy), resource)
}

func flatResourcesControlSpecified(resources []string, wanted string) bool {
	for _, resource := range resources {
		if strings.EqualFold(resource, wanted) {
			return true
		}
	}
	return false
}

func policiesControlContainerResource(policies []VPAContainerPolicy, container, resource string) bool {
	var exact, wildcard []VPAContainerPolicy
	for _, policy := range policies {
		switch policy.ContainerName {
		case container:
			exact = append(exact, policy)
		case "*":
			wildcard = append(wildcard, policy)
		}
	}
	if len(exact) > 0 {
		return anyPolicyControls(exact, resource)
	}
	if len(wildcard) > 0 {
		return anyPolicyControls(wildcard, resource)
	}
	// Containers without an explicit or wildcard policy use VPA defaults.
	return isVPAControlledResource(resource)
}

func policiesControlAggregateResource(policies []VPAContainerPolicy, resource string) bool {
	var wildcard []VPAContainerPolicy
	for _, policy := range policies {
		if policy.ContainerName == "*" {
			wildcard = append(wildcard, policy)
			continue
		}
		if policyControlsResource(policy, resource) {
			return true
		}
	}
	if len(wildcard) > 0 {
		return anyPolicyControls(wildcard, resource)
	}
	// Without a wildcard, containers lacking named policies use defaults. The
	// workload's complete container list is unavailable here, so fail closed.
	return isVPAControlledResource(resource)
}

func anyPolicyControls(policies []VPAContainerPolicy, resource string) bool {
	for _, policy := range policies {
		if policyControlsResource(policy, resource) {
			return true
		}
	}
	return false
}

// hasResourceMetrics returns true when the HPA uses CPU or memory resource metrics.
func hasResourceMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	if hpa == nil {
		return false
	}
	for _, metric := range hpa.Spec.Metrics {
		if name, ok := hpaResourceMetricName(metric); ok && isVPAControlledResource(name) {
			return true
		}
	}
	return false
}

func hpaResourceMetricName(metric autoscalingv2.MetricSpec) (string, bool) {
	switch metric.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource != nil && isUtilizationMetricTarget(metric.Resource.Target) {
			return strings.ToLower(string(metric.Resource.Name)), true
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if metric.ContainerResource != nil && isUtilizationMetricTarget(metric.ContainerResource.Target) {
			return strings.ToLower(string(metric.ContainerResource.Name)), true
		}
	}
	return "", false
}

func isUtilizationMetricTarget(target autoscalingv2.MetricTarget) bool {
	return target.Type == autoscalingv2.UtilizationMetricType || target.Type == ""
}

func isVPAControlledResource(name string) bool {
	return name == string(corev1.ResourceCPU) || name == string(corev1.ResourceMemory)
}
