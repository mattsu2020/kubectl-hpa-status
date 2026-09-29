package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	apps "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPatchExportSeparatesHyphenatedIdentities(t *testing.T) {
	dir := t.TempDir()
	a, e := patchFileName(dir, "a-b", "c")
	if e != nil {
		t.Fatal(e)
	}
	b, e := patchFileName(dir, "a", "b-c")
	if e != nil {
		t.Fatal(e)
	}
	if a == b {
		t.Fatalf("different HPAs have the same output path: %s", a)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	for _, path := range []string{a, b} {
		if err := writePatchExportFile(root, filepath.Base(path), []byte(path)); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{a, b} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != path {
			t.Fatalf("export %q: content=%q err=%v", path, data, err)
		}
	}
}
func TestOwnershipMatchingReplicas(t *testing.T) {
	for _, manager := range []string{"horizontal-pod-autoscaler", "", "argocd"} {
		t.Run(manager, func(t *testing.T) {
			n := int32(3)
			d := &apps.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"}, Spec: apps.DeploymentSpec{Replicas: &n}}
			if manager != "" {
				d.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: manager, Operation: metav1.ManagedFieldsOperationUpdate, FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:replicas":{}}}`)}}}
			}
			h := &autoscaling.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"}, Spec: autoscaling.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscaling.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "web"}}, Status: autoscaling.HorizontalPodAutoscalerStatus{DesiredReplicas: 3}}
			r, err := buildOwnershipReport(context.Background(), &kube.Client{Interface: fake.NewSimpleClientset(d), Namespace: "default"}, h)
			if err != nil {
				t.Fatal(err)
			}
			wantRisk := manager == "argocd"
			if (len(r.Risks) > 0) != wantRisk || (len(r.Recommendations) > 0) != wantRisk {
				t.Fatalf("manager=%q: risks=%v recommendations=%v; want risk=%v", manager, r.Risks, r.Recommendations, wantRisk)
			}
		})
	}
}
