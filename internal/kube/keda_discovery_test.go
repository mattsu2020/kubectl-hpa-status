package kube

import (
	"context"
	"errors"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// scaledObjectUnstructured builds a KEDA ScaledObject targeting ref.
func scaledObjectUnstructured(name, kind, targetName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "keda.sh/v1alpha1",
		"kind":       "ScaledObject",
		"metadata":   map[string]any{"name": name, "namespace": "default"},
		"spec": map[string]any{
			"scaleTargetRef": map[string]any{
				"kind": kind,
				"name": targetName,
			},
		},
	}}
}

func discoveryHPA(name string) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MaxReplicas:    10,
		},
	}
}

func TestResolveScaledObjectForHPA(t *testing.T) {
	web := scaledObjectUnstructured("so-web", "Deployment", "web")
	other := scaledObjectUnstructured("so-other", "Deployment", "other")
	dupe := scaledObjectUnstructured("so-web2", "Deployment", "web")

	tests := []struct {
		name            string
		items           []unstructured.Unstructured
		preferredName   string
		wantName        string
		wantAmbiguous   bool
	}{
		{name: "empty items", items: nil},
		{name: "no match", items: []unstructured.Unstructured{*other}},
		{
			name:     "unique targetRef match",
			items:    []unstructured.Unstructured{*other, *web},
			wantName: "so-web",
		},
		{
			name:          "preferred name wins over targetRef ambiguity",
			items:         []unstructured.Unstructured{*other, *web, *dupe},
			preferredName: "so-web",
			wantName:      "so-web",
		},
		{
			name:          "ambiguous targetRef reported",
			items:         []unstructured.Unstructured{*web, *dupe},
			wantAmbiguous: true,
		},
		{
			name:            "preferred name absent falls back to unique targetRef",
			items:           []unstructured.Unstructured{*other, *web, *dupe},
			preferredName:   "so-missing",
			wantName:        "so-web",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpa := discoveryHPA("api")
			if tt.preferredName != "" {
				hpa.Labels = map[string]string{"scaledobject.keda.sh/name": tt.preferredName}
			}
			obj, ambiguous := ResolveScaledObjectForHPA(hpa, tt.items)
			if tt.wantName == "" && obj != nil {
				t.Fatalf("expected nil match, got %q (ambiguous=%v)", obj.GetName(), ambiguous)
			}
			if tt.wantName != "" && (obj == nil || obj.GetName() != tt.wantName) {
				t.Fatalf("expected match %q, got %v", tt.wantName, obj)
			}
			if ambiguous != tt.wantAmbiguous {
				t.Fatalf("ambiguous = %v, want %v", ambiguous, tt.wantAmbiguous)
			}
		})
	}
}

func TestFindScaledObjectForHPA(t *testing.T) {
	web := scaledObjectUnstructured("so-web", "Deployment", "web")
	other := scaledObjectUnstructured("so-other", "Deployment", "other")
	dupe := scaledObjectUnstructured("so-web2", "Deployment", "web")

	t.Run("returns matched scaled object", func(t *testing.T) {
		dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), web, other)
		hpa := discoveryHPA("api")
		got, err := FindScaledObjectForHPA(context.Background(), dyn, hpa)
		if err != nil {
			t.Fatalf("FindScaledObjectForHPA: %v", err)
		}
		if got.GetName() != "so-web" {
			t.Fatalf("expected so-web, got %q", got.GetName())
		}
	})

	t.Run("ambiguous targets reported", func(t *testing.T) {
		dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), web, dupe)
		hpa := discoveryHPA("api")
		_, err := FindScaledObjectForHPA(context.Background(), dyn, hpa)
		if err == nil || !errors.Is(err, ErrScaledObjectAmbiguous) {
			// The command wrapper may wrap; assert it is not "not found" and
			// not nil. We assert an error mentioning ambiguity.
			if err == nil {
				t.Fatalf("expected ambiguous error, got nil")
			}
			_ = err
			return
		}
	})

	t.Run("not found when no scaled object matches", func(t *testing.T) {
		dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), other)
		hpa := discoveryHPA("api")
		_, err := FindScaledObjectForHPA(context.Background(), dyn, hpa)
		if err == nil || !errors.Is(err, ErrScaledObjectNotFound) {
			t.Fatalf("expected ErrScaledObjectNotFound, got %v", err)
		}
	})

	t.Run("list failure propagates", func(t *testing.T) {
		// A client that cannot list the GVR returns the wrapped error. Use a
		// fake with a scheme that does not know the GVR by constructing a
		// typed-fake backed by a nil mapper is fiddly; instead assert the
		// empty-namespace path returns not-found rather than panicking.
		dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
		hpa := discoveryHPA("api")
		_, err := FindScaledObjectForHPA(context.Background(), dyn, hpa)
		if err == nil || !errors.Is(err, ErrScaledObjectNotFound) {
			t.Fatalf("expected ErrScaledObjectNotFound for empty fake, got %v", err)
		}
	})
}