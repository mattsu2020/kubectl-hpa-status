package cmd

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// int32ptrTest returns a pointer to v for table-driven replica assertions.
func int32ptrTest(v int32) *int32 { return &v }

func TestExtractReplicasFromUnstructured(t *testing.T) {
	tests := []struct {
		name       string
		object     map[string]any
		targetKind string
		targetName string
		want       *int32
		wantFound  bool
	}{
		{
			name: "deployment with replicas",
			object: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata":   map[string]any{"name": "web"},
				"spec":       map[string]any{"replicas": int64(3)},
			},
			targetKind: "Deployment",
			targetName: "web",
			want:       int32ptrTest(3),
			wantFound:  true,
		},
		{
			name: "lowercase deployment kind normalized",
			object: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "deployment",
				"metadata":   map[string]any{"name": "web"},
				"spec":       map[string]any{"replicas": int64(7)},
			},
			targetKind: "Deployment",
			targetName: "web",
			want:       int32ptrTest(7),
			wantFound:  true,
		},
		{
			name: "statefulset apps suffix normalized",
			object: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "StatefulSet.apps",
				"metadata":   map[string]any{"name": "cache"},
				"spec":       map[string]any{"replicas": int64(5)},
			},
			targetKind: "StatefulSet",
			targetName: "cache",
			want:       int32ptrTest(5),
			wantFound:  true,
		},
		{
			name: "name mismatch returns not found",
			object: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata":   map[string]any{"name": "other"},
				"spec":       map[string]any{"replicas": int64(3)},
			},
			targetKind: "Deployment",
			targetName: "web",
			want:       nil,
			wantFound:  false,
		},
		{
			name: "kind mismatch returns not found",
			object: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata":   map[string]any{"name": "web"},
				"spec":       map[string]any{"replicas": int64(3)},
			},
			targetKind: "StatefulSet",
			targetName: "web",
			want:       nil,
			wantFound:  false,
		},
		{
			name: "missing replicas returns not found without error",
			object: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata":   map[string]any{"name": "web"},
				"spec":       map[string]any{},
			},
			targetKind: "Deployment",
			targetName: "web",
			want:       nil,
			wantFound:  false,
		},
		{
			name: "unsupported kind returns not found",
			object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]any{"name": "web"},
				"spec":       map[string]any{"replicas": int64(3)},
			},
			targetKind: "ConfigMap",
			targetName: "web",
			want:       nil,
			wantFound:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &unstructured.Unstructured{Object: tt.object}
			got, found := extractReplicasFromUnstructured(u, tt.targetKind, tt.targetName)
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("got = %v, want nil", *got)
				}
			} else if got == nil {
				t.Fatalf("got = nil, want %d", *tt.want)
			} else if *got != *tt.want {
				t.Fatalf("got = %d, want %d", *got, *tt.want)
			}
		})
	}
}

func TestExtractGitOpsAnnotations(t *testing.T) {
	t.Run("nil annotations is a no-op", func(t *testing.T) {
		argoCD := map[string]string{}
		flux := map[string]string{}
		// Should not panic on nil input.
		extractGitOpsAnnotations(nil, argoCD, flux)
		if len(argoCD) != 0 || len(flux) != 0 {
			t.Fatalf("expected empty maps for nil input, got argo=%v flux=%v", argoCD, flux)
		}
	})

	t.Run("partitions argocd and flux annotations", func(t *testing.T) {
		annotations := map[string]string{
			"argocd.argoproj.io/sync-wave":     "1",
			"argocd.argoproj.io/hook":          "Sync",
			"kustomize.toolkit.fluxcd.io/name": "app",
			"helm.toolkit.fluxcd.io/namespace": "infra",
			"app.kubernetes.io/name":           "unrelated",
		}
		argoCD := map[string]string{}
		flux := map[string]string{}
		extractGitOpsAnnotations(annotations, argoCD, flux)

		if len(argoCD) != 2 {
			t.Fatalf("argoCD len = %d, want 2: %v", len(argoCD), argoCD)
		}
		if argoCD["argocd.argoproj.io/sync-wave"] != "1" {
			t.Fatalf("missing argocd sync-wave annotation")
		}
		if len(flux) != 2 {
			t.Fatalf("flux len = %d, want 2: %v", len(flux), flux)
		}
		if flux["kustomize.toolkit.fluxcd.io/name"] != "app" {
			t.Fatalf("missing flux kustomize annotation")
		}
		if flux["helm.toolkit.fluxcd.io/namespace"] != "infra" {
			t.Fatalf("missing flux helm annotation")
		}
	})

	t.Run("empty annotations leaves maps untouched", func(t *testing.T) {
		argoCD := map[string]string{}
		flux := map[string]string{}
		extractGitOpsAnnotations(map[string]string{}, argoCD, flux)
		if len(argoCD) != 0 || len(flux) != 0 {
			t.Fatalf("expected empty maps, got argo=%v flux=%v", argoCD, flux)
		}
	})
}
