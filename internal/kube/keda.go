package kube

import (
	"context"
	"fmt"
	"math"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var scaledObjectGVR = schema.GroupVersionResource{
	Group:    "keda.sh",
	Version:  "v1alpha1",
	Resource: "scaledobjects",
}

// KEDAInfo holds extracted information about a KEDA ScaledObject.
type KEDAInfo struct {
	ScaledObjectName string               `json:"scaledObjectName" yaml:"scaledObjectName"`
	Triggers         []KEDATrigger        `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	PollingInterval  *int32               `json:"pollingInterval,omitempty" yaml:"pollingInterval,omitempty"`
	CooldownPeriod   *int32               `json:"cooldownPeriod,omitempty" yaml:"cooldownPeriod,omitempty"`
	MinReplicaCount  *int32               `json:"minReplicaCount,omitempty" yaml:"minReplicaCount,omitempty"`
	MaxReplicaCount  *int32               `json:"maxReplicaCount,omitempty" yaml:"maxReplicaCount,omitempty"`
	Conditions       []KEDACondition      `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	Advanced         map[string]string    `json:"advanced,omitempty" yaml:"advanced,omitempty"`
	Fallback         *KEDAFallback        `json:"fallback,omitempty" yaml:"fallback,omitempty"`
	ScalingPolicies  []KEDAScalingPolicy  `json:"scalingPolicies,omitempty" yaml:"scalingPolicies,omitempty"`
}

// KEDATrigger represents a single KEDA scaler trigger.
type KEDATrigger struct {
	Type              string            `json:"type" yaml:"type"`
	Name              string            `json:"name,omitempty" yaml:"name,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Status            string            `json:"status,omitempty" yaml:"status,omitempty"`                        // "Active", "Inactive", "Unknown"
	Message           string            `json:"message,omitempty" yaml:"message,omitempty"`
	AuthenticationRef string            `json:"authenticationRef,omitempty" yaml:"authenticationRef,omitempty"`
}

// KEDACondition represents a condition from the ScaledObject status.
type KEDACondition struct {
	Type    string `json:"type" yaml:"type"`
	Status  string `json:"status" yaml:"status"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// KEDAFallback holds fallback configuration from a ScaledObject.
type KEDAFallback struct {
	FailureThreshold int32 `json:"failureThreshold" yaml:"failureThreshold"`
	Replicas         int32 `json:"replicas" yaml:"replicas"`
}

// KEDAScalingPolicy represents a scaling policy from a ScaledObject.
type KEDAScalingPolicy struct {
	Type          string `json:"type" yaml:"type"` // "scaleUp" or "scaleDown"
	Value         int32  `json:"value" yaml:"value"`
	PeriodSeconds int32  `json:"periodSeconds" yaml:"periodSeconds"`
}

// DetectKEDA checks whether an HPA is KEDA-managed by inspecting labels and annotations.
func DetectKEDA(hpa *autoscalingv2.HorizontalPodAutoscaler) (isKEDA bool, scaledObjectName string) {
	if hpa == nil {
		return false, ""
	}
	for key, value := range hpa.Labels {
		if strings.Contains(strings.ToLower(key), "keda.sh") || strings.Contains(strings.ToLower(value), "keda") {
			if name, ok := extractScaledObjectName(hpa); ok {
				return true, name
			}
			return true, ""
		}
	}
	for key, value := range hpa.Annotations {
		if strings.Contains(strings.ToLower(key), "keda.sh") || strings.Contains(strings.ToLower(value), "keda") {
			if name, ok := extractScaledObjectName(hpa); ok {
				return true, name
			}
			return true, ""
		}
	}
	if strings.HasPrefix(hpa.Name, "keda-hpa-") {
		if name, ok := extractScaledObjectName(hpa); ok {
			return true, name
		}
		return true, ""
	}
	return false, ""
}

// FetchScaledObject retrieves a KEDA ScaledObject using the dynamic client.
func FetchScaledObject(ctx context.Context, client dynamic.Interface, namespace, name string) (*unstructured.Unstructured, error) {
	return client.Resource(scaledObjectGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

// ExtractKEDAInfo parses an unstructured ScaledObject into a structured KEDAInfo.
func ExtractKEDAInfo(u *unstructured.Unstructured) KEDAInfo {
	if u == nil {
		return KEDAInfo{}
	}
	info := KEDAInfo{
		ScaledObjectName: u.GetName(),
	}

	spec, ok := u.Object["spec"].(map[string]any)
	if ok {
		info.Triggers = extractTriggers(spec)
		info.PollingInterval = extractInt32Ptr(spec, "pollingInterval")
		info.CooldownPeriod = extractInt32Ptr(spec, "cooldownPeriod")
		info.MinReplicaCount = extractInt32Ptr(spec, "minReplicaCount")
		info.MaxReplicaCount = extractInt32Ptr(spec, "maxReplicaCount")
		if advanced, ok := spec["advanced"].(map[string]any); ok {
			info.Advanced = extractAdvanced(advanced)
		}
		info.Fallback = extractFallback(spec)
		info.ScalingPolicies = extractScalingPolicies(spec)
	}

	status, ok := u.Object["status"].(map[string]any)
	if ok {
		info.Conditions = extractKEDAConditions(status)
		// Merge trigger health status into triggers extracted from spec.
		extractTriggerStatus(u, info.Triggers)
	}

	return info
}

// NewDynamicClient creates a dynamic client from the same Options used for the typed client.
func NewDynamicClient(opts Options) (dynamic.Interface, string, error) {
	loadingRules := newLoadingRules(opts)
	overrides := newOverrides(opts)

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	namespace := opts.Namespace
	if namespace == "" {
		var err error
		namespace, _, err = clientConfig.Namespace()
		if err != nil {
			return nil, "", err
		}
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, "", err
	}

	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, "", err
	}

	return dynClient, namespace, nil
}

// FindScaledObjectForHPA attempts to locate the ScaledObject that owns the given HPA.
// It tries the label-based name first, then falls back to listing ScaledObjects in the namespace.
func FindScaledObjectForHPA(ctx context.Context, dynClient dynamic.Interface, _ kubernetes.Interface, hpa *autoscalingv2.HorizontalPodAutoscaler) (*unstructured.Unstructured, error) {
	if _, name := DetectKEDA(hpa); name != "" {
		return FetchScaledObject(ctx, dynClient, hpa.Namespace, name)
	}

	// Fallback: list ScaledObjects and find one that references this HPA's scaleTargetRef.
	list, err := dynClient.Resource(scaledObjectGVR).Namespace(hpa.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list ScaledObjects in namespace %s: %w", hpa.Namespace, err)
	}

	for i := range list.Items {
		ref := extractScaleTargetRef(&list.Items[i])
		if ref != nil && ref.Name == hpa.Spec.ScaleTargetRef.Name && ref.Kind == hpa.Spec.ScaleTargetRef.Kind {
			return &list.Items[i], nil
		}
	}

	return nil, fmt.Errorf("no ScaledObject found for HPA %s/%s", hpa.Namespace, hpa.Name)
}

func extractScaledObjectName(hpa *autoscalingv2.HorizontalPodAutoscaler) (string, bool) {
	if hpa.Labels != nil {
		if name, ok := hpa.Labels["scaledobject.keda.sh/name"]; ok && name != "" {
			return name, true
		}
	}
	if hpa.Annotations != nil {
		if name, ok := hpa.Annotations["scaledobject.keda.sh/name"]; ok && name != "" {
			return name, true
		}
	}
	// Derive from HPA name pattern "keda-hpa-<scaledobject>"
	if strings.HasPrefix(hpa.Name, "keda-hpa-") {
		return strings.TrimPrefix(hpa.Name, "keda-hpa-"), true
	}
	return "", false
}

func extractTriggers(spec map[string]any) []KEDATrigger {
	raw, ok := spec["triggers"]
	if !ok {
		return nil
	}
	triggersRaw, ok := raw.([]any)
	if !ok {
		return nil
	}
	triggers := make([]KEDATrigger, 0, len(triggersRaw))
	for _, t := range triggersRaw {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		trigger := KEDATrigger{
			Type: stringValue(tm, "type"),
			Name: stringValue(tm, "name"),
		}
		if metadata, ok := tm["metadata"].(map[string]any); ok {
			trigger.Metadata = make(map[string]string, len(metadata))
			for k, v := range metadata {
				trigger.Metadata[k] = fmt.Sprintf("%v", v)
			}
		}
		// Extract authenticationRef.name from the trigger spec.
		if authRef, ok := tm["authenticationRef"].(map[string]any); ok {
			trigger.AuthenticationRef = stringValue(authRef, "name")
		}
		triggers = append(triggers, trigger)
	}
	return triggers
}

// extractTriggerStatus reads status.health from the ScaledObject and merges
// per-trigger health status (Active/Inactive/Unknown) into the triggers slice.
func extractTriggerStatus(u *unstructured.Unstructured, triggers []KEDATrigger) {
	status, ok := u.Object["status"].(map[string]any)
	if !ok {
		return
	}
	health, ok := status["health"].(map[string]any)
	if !ok {
		// No per-trigger health; try conditions for overall status.
		return
	}

	// KEDA v2: status.health is a map keyed by trigger name or index.
	for i := range triggers {
		t := &triggers[i]
		// Try matching by trigger name first, then by type.
		if entry, ok := health[t.Name].(map[string]any); ok && t.Name != "" {
			t.Status = mapHealthStatus(stringValue(entry, "status"))
			t.Message = stringValue(entry, "message")
		} else if entry, ok := health[t.Type].(map[string]any); ok {
			t.Status = mapHealthStatus(stringValue(entry, "status"))
			t.Message = stringValue(entry, "message")
		}
	}
}

// mapHealthStatus converts KEDA health status strings to a normalized form.
func mapHealthStatus(s string) string {
	switch strings.ToLower(s) {
	case "active", "happy", "true":
		return "Active"
	case "inactive", "false":
		return "Inactive"
	case "unknown", "":
		return "Unknown"
	default:
		return s
	}
}

// extractFallback reads spec.fallback from the ScaledObject.
func extractFallback(spec map[string]any) *KEDAFallback {
	raw, ok := spec["fallback"]
	if !ok {
		return nil
	}
	fm, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	threshold := extractInt32Ptr(fm, "failureThreshold")
	replicas := extractInt32Ptr(fm, "replicas")
	if threshold == nil && replicas == nil {
		return nil
	}
	fallback := &KEDAFallback{}
	if threshold != nil {
		fallback.FailureThreshold = *threshold
	}
	if replicas != nil {
		fallback.Replicas = *replicas
	}
	return fallback
}

// extractScalingPolicies reads scaling policies from
// spec.advanced.horizontalPodAutoscalerConfig.behavior.
func extractScalingPolicies(spec map[string]any) []KEDAScalingPolicy {
	advanced, ok := spec["advanced"].(map[string]any)
	if !ok {
		return nil
	}
	hpaConfig, ok := advanced["horizontalPodAutoscalerConfig"].(map[string]any)
	if !ok {
		return nil
	}
	behavior, ok := hpaConfig["behavior"].(map[string]any)
	if !ok {
		return nil
	}

	var policies []KEDAScalingPolicy
	for _, direction := range []string{"scaleUp", "scaleDown"} {
		rules, ok := behavior[direction].(map[string]any)
		if !ok {
			continue
		}
		rawPolicies, ok := rules["policies"].([]any)
		if !ok {
			continue
		}
		for _, p := range rawPolicies {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			value := extractInt32Ptr(pm, "value")
			period := extractInt32Ptr(pm, "periodSeconds")
			if value == nil {
				continue
			}
			sp := KEDAScalingPolicy{
				Type:  direction,
				Value: *value,
			}
			if period != nil {
				sp.PeriodSeconds = *period
			}
			policies = append(policies, sp)
		}
	}
	return policies
}

func extractKEDAConditions(status map[string]any) []KEDACondition {
	raw, ok := status["conditions"]
	if !ok {
		return nil
	}
	conditionsRaw, ok := raw.([]any)
	if !ok {
		return nil
	}
	conditions := make([]KEDACondition, 0, len(conditionsRaw))
	for _, c := range conditionsRaw {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		conditions = append(conditions, KEDACondition{
			Type:    stringValue(cm, "type"),
			Status:  stringValue(cm, "status"),
			Reason:  stringValue(cm, "reason"),
			Message: stringValue(cm, "message"),
		})
	}
	return conditions
}

func extractScaleTargetRef(u *unstructured.Unstructured) *autoscalingv2.CrossVersionObjectReference {
	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		return nil
	}
	ref, ok := spec["scaleTargetRef"].(map[string]any)
	if !ok {
		return nil
	}
	return &autoscalingv2.CrossVersionObjectReference{
		APIVersion: stringValue(ref, "apiVersion"),
		Kind:       stringValue(ref, "kind"),
		Name:       stringValue(ref, "name"),
	}
}

func extractInt32Ptr(m map[string]any, key string) *int32 {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case int64:
		if v < math.MinInt32 || v > math.MaxInt32 {
			return nil
		}
		val := int32(v) //nolint:gosec // overflow checked above
		return &val
	case int:
		if v < math.MinInt32 || v > math.MaxInt32 {
			return nil
		}
		val := int32(v) //nolint:gosec // overflow checked above
		return &val
	case float64:
		if v < math.MinInt32 || v > math.MaxInt32 {
			return nil
		}
		val := int32(v)
		return &val
	default:
		return nil
	}
}

func extractAdvanced(advanced map[string]any) map[string]string {
	result := make(map[string]string, len(advanced))
	for k, v := range advanced {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}

func stringValue(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}
