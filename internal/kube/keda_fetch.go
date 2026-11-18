package kube

import (
	"context"
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// FetchScaledObject retrieves a KEDA ScaledObject using the dynamic client.
func FetchScaledObject(ctx context.Context, client dynamic.Interface, namespace, name string) (*unstructured.Unstructured, error) {
	return retryTransient(ctx, func() (*unstructured.Unstructured, error) {
		return client.Resource(scaledObjectGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	})
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
// When the ScaledObject name is derivable (the scaledobject.keda.sh/name
// label/annotation or the keda-hpa-<name> convention), it first Gets that
// exact object — a single cheap read instead of a namespace-wide paginated
// list per HPA. A NotFound (or no derivable name) falls back to listing the
// namespace and applying the canonical matching rule, because the derived
// name is a convention, not a guarantee.
func FindScaledObjectForHPA(ctx context.Context, dynClient dynamic.Interface, hpa *autoscalingv2.HorizontalPodAutoscaler) (*unstructured.Unstructured, error) {
	if name := extractScaledObjectName(hpa); name != "" {
		obj, err := FetchScaledObject(ctx, dynClient, hpa.Namespace, name)
		if err == nil && scaledObjectMatchesTargetOrUnset(obj, hpa) {
			return obj, nil
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
	}

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
// rule: prefer the explicitly named object (when its scaleTargetRef does not
// conflict with the HPA), otherwise require one unique scaleTargetRef match.
func ResolveScaledObjectForHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, items []unstructured.Unstructured) (*unstructured.Unstructured, bool) {
	preferred := DetectKEDA(hpa).Name
	if preferred != "" {
		for i := range items {
			if items[i].GetName() == preferred && scaledObjectMatchesTargetOrUnset(&items[i], hpa) {
				return &items[i], false
			}
		}
	}
	candidates := make([]*unstructured.Unstructured, 0, 1)
	for i := range items {
		ref := extractScaleTargetRef(&items[i])
		if ref == nil {
			continue
		}
		refKind := ref.Kind
		if refKind == "" {
			refKind = "Deployment"
		}
		if ref.Name == hpa.Spec.ScaleTargetRef.Name && refKind == hpa.Spec.ScaleTargetRef.Kind {
			candidates = append(candidates, &items[i])
		}
	}
	if len(candidates) == 1 {
		return candidates[0], false
	}
	return nil, len(candidates) > 1
}

func scaledObjectMatchesTargetOrUnset(u *unstructured.Unstructured, hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	if u == nil || hpa == nil {
		return false
	}
	ref := extractScaleTargetRef(u)
	if ref == nil || (ref.Kind == "" && ref.Name == "") {
		return true
	}
	if ref.Name != "" && ref.Name != hpa.Spec.ScaleTargetRef.Name {
		return false
	}
	if ref.Kind != "" && ref.Kind != hpa.Spec.ScaleTargetRef.Kind {
		return false
	}
	return true
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
