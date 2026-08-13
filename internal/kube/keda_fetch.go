package kube

import (
	"context"
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// FetchScaledObject retrieves a KEDA ScaledObject using the dynamic client.
func FetchScaledObject(ctx context.Context, client dynamic.Interface, namespace, name string) (*unstructured.Unstructured, error) {
	return client.Resource(scaledObjectGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

// NewDynamicClient creates a dynamic client from the same Options used for the typed client.
func NewDynamicClient(opts Options) (dynamic.Interface, string, error) {
	namespace, restConfig, err := resolveNamespaceAndRestConfig(opts)
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
func FindScaledObjectForHPA(ctx context.Context, dynClient dynamic.Interface, hpa *autoscalingv2.HorizontalPodAutoscaler) (*unstructured.Unstructured, error) {
	items, err := FetchScaledObjects(ctx, dynClient, hpa.Namespace)
	if err != nil {
		return nil, err
	}
	matched, ambiguous := ResolveScaledObjectForHPA(hpa, items)
	if matched != nil {
		return matched, nil
	}
	if ambiguous {
		return nil, fmt.Errorf("hpa %s/%s: %w", hpa.Namespace, hpa.Name, ErrScaledObjectAmbiguous)
	}
	return nil, fmt.Errorf("hpa %s/%s: %w", hpa.Namespace, hpa.Name, ErrScaledObjectNotFound)
}

// ResolveScaledObjectForHPA applies the canonical single and batch matching
// rule: prefer the explicitly named object, otherwise require one unique
// scaleTargetRef match. The boolean reports an ambiguous target match.
func ResolveScaledObjectForHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, items []unstructured.Unstructured) (*unstructured.Unstructured, bool) {
	preferred := DetectKEDA(hpa).Name
	if preferred != "" {
		for i := range items {
			if items[i].GetName() == preferred {
				return &items[i], false
			}
		}
	}
	candidates := make([]*unstructured.Unstructured, 0, 1)
	for i := range items {
		ref := extractScaleTargetRef(&items[i])
		if ref != nil && ref.Name == hpa.Spec.ScaleTargetRef.Name && ref.Kind == hpa.Spec.ScaleTargetRef.Kind {
			candidates = append(candidates, &items[i])
		}
	}
	if len(candidates) == 1 {
		return candidates[0], false
	}
	return nil, len(candidates) > 1
}

// FetchScaledObjects lists all KEDA ScaledObjects in a namespace using the
// shared Kubernetes continue-token contract.
func FetchScaledObjects(ctx context.Context, dynClient dynamic.Interface, namespace string) ([]unstructured.Unstructured, error) {
	items, err := collectListPages(ctx, metav1.ListOptions{}, func(ctx context.Context, page metav1.ListOptions) ([]unstructured.Unstructured, string, error) {
		list, err := dynClient.Resource(scaledObjectGVR).Namespace(namespace).List(ctx, page)
		if err != nil {
			return nil, "", err
		}
		return list.Items, list.GetContinue(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list ScaledObjects in namespace %s: %w", namespace, err)
	}
	return items, nil
}

// extractScaledObjectName derives the ScaledObject name backing this HPA from
// the well-known scaledobject.keda.sh/name label/annotation, falling back to
// the "keda-hpa-<name>" prefix convention. Returns "" when no derivation is
// possible.
func extractScaledObjectName(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if hpa.Labels != nil {
		if name, ok := hpa.Labels["scaledobject.keda.sh/name"]; ok && name != "" {
			return name
		}
	}
	if hpa.Annotations != nil {
		if name, ok := hpa.Annotations["scaledobject.keda.sh/name"]; ok && name != "" {
			return name
		}
	}
	// Derive from HPA name pattern "keda-hpa-<scaledobject>"
	if strings.HasPrefix(hpa.Name, "keda-hpa-") {
		return strings.TrimPrefix(hpa.Name, "keda-hpa-")
	}
	return ""
}
