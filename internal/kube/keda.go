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
	ScaledObjectName string            `json:"scaledObjectName" yaml:"scaledObjectName"`
	Triggers         []KEDATrigger     `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	PollingInterval  *int32            `json:"pollingInterval,omitempty" yaml:"pollingInterval,omitempty"`
	CooldownPeriod   *int32            `json:"cooldownPeriod,omitempty" yaml:"cooldownPeriod,omitempty"`
	MinReplicaCount  *int32            `json:"minReplicaCount,omitempty" yaml:"minReplicaCount,omitempty"`
	MaxReplicaCount  *int32            `json:"maxReplicaCount,omitempty" yaml:"maxReplicaCount,omitempty"`
	Conditions       []KEDACondition   `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	Advanced         map[string]string `json:"advanced,omitempty" yaml:"advanced,omitempty"`
}

// KEDATrigger represents a single KEDA scaler trigger.
type KEDATrigger struct {
	Type     string            `json:"type" yaml:"type"`
	Name     string            `json:"name,omitempty" yaml:"name,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// KEDACondition represents a condition from the ScaledObject status.
type KEDACondition struct {
	Type    string `json:"type" yaml:"type"`
	Status  string `json:"status" yaml:"status"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
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
	}

	status, ok := u.Object["status"].(map[string]any)
	if ok {
		info.Conditions = extractKEDAConditions(status)
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
		triggers = append(triggers, trigger)
	}
	return triggers
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
