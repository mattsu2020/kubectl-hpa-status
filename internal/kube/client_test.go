package kube

import (
	"errors"
	"os"
	"path/filepath"
	stdtesting "testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubefake "k8s.io/client-go/discovery/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/testing"
)

func TestDetectCRDs_BothPresent(t *stdtesting.T) {
	fakeDiscovery := &kubefake.FakeDiscovery{
		Fake: &testing.Fake{},
	}
	fakeDiscovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "keda.sh/v1alpha1",
			APIResources: []metav1.APIResource{
				{Name: "scaledobjects", Namespaced: true, Kind: "ScaledObject"},
			},
		},
		{
			GroupVersion: "autoscaling.k8s.io/v1",
			APIResources: []metav1.APIResource{
				{Name: "verticalpodautoscalers", Namespaced: true, Kind: "VerticalPodAutoscaler"},
			},
		},
	}

	avail := DetectCRDs(fakeDiscovery)
	if !avail.KEDA {
		t.Error("expected KEDA to be detected")
	}
	if !avail.VPA {
		t.Error("expected VPA to be detected")
	}
	if avail.KEDError != nil {
		t.Errorf("did not expect KEDA error when CRD present, got: %v", avail.KEDError)
	}
	if avail.VPAError != nil {
		t.Errorf("did not expect VPA error when CRD present, got: %v", avail.VPAError)
	}
}

func TestDetectCRDs_NeitherPresent(t *stdtesting.T) {
	fakeDiscovery := &kubefake.FakeDiscovery{
		Fake: &testing.Fake{},
	}
	fakeDiscovery.Resources = []*metav1.APIResourceList{}

	avail := DetectCRDs(fakeDiscovery)
	if avail.KEDA {
		t.Error("did not expect KEDA detection")
	}
	if avail.VPA {
		t.Error("did not expect VPA detection")
	}
	// Absent CRDs still populate the wrapped sentinel error so callers can
	// distinguish "absent" from "discovery failed" downstream.
	if !errors.Is(avail.KEDError, ErrKEDACRDNotDetected) {
		t.Errorf("expected KEDError to wrap ErrKEDACRDNotDetected, got: %v", avail.KEDError)
	}
	if !errors.Is(avail.VPAError, ErrVPACRDNotDetected) {
		t.Errorf("expected VPAError to wrap ErrVPACRDNotDetected, got: %v", avail.VPAError)
	}
}

func TestDetectCRDs_KEDAOnly(t *stdtesting.T) {
	fakeDiscovery := &kubefake.FakeDiscovery{
		Fake: &testing.Fake{},
	}
	fakeDiscovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "keda.sh/v1alpha1",
			APIResources: []metav1.APIResource{
				{Name: "scaledobjects", Namespaced: true, Kind: "ScaledObject"},
			},
		},
	}

	avail := DetectCRDs(fakeDiscovery)
	if !avail.KEDA {
		t.Error("expected KEDA to be detected")
	}
	if avail.VPA {
		t.Error("did not expect VPA detection")
	}
	if avail.KEDError != nil {
		t.Errorf("did not expect KEDA error when CRD present, got: %v", avail.KEDError)
	}
	if !errors.Is(avail.VPAError, ErrVPACRDNotDetected) {
		t.Errorf("expected VPAError to wrap ErrVPACRDNotDetected, got: %v", avail.VPAError)
	}
}

func TestScaledObjectGVR(t *stdtesting.T) {
	gvr := ScaledObjectGVR()
	expected := schema.GroupVersionResource{
		Group:    "keda.sh",
		Version:  "v1alpha1",
		Resource: "scaledobjects",
	}
	if gvr != expected {
		t.Errorf("expected %v, got %v", expected, gvr)
	}
}

func TestNewClientBundle_SharesHTTPTransport(t *stdtesting.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	cfg := `apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: local
  context:
    cluster: local
    user: local
    namespace: prod
current-context: local
users:
- name: local
  user:
    token: dummy-token
`
	if err := os.WriteFile(kubeconfig, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	bundle, err := NewClientBundle(Options{Kubeconfig: kubeconfig})
	if err != nil {
		t.Fatalf("NewClientBundle failed: %v", err)
	}
	if bundle.Client == nil || bundle.Discovery == nil || bundle.Dynamic == nil || bundle.HTTP == nil {
		t.Fatal("expected all bundle clients to be initialized")
	}
	if bundle.Client.Namespace != "prod" {
		t.Errorf("expected namespace prod, got %q", bundle.Client.Namespace)
	}
	if bundle.Client.SharedHTTPClient() != bundle.HTTP {
		t.Error("expected Client and ClientBundle to share the same HTTP client")
	}
	disco2, err := bundle.Client.DiscoveryClient()
	if err != nil || disco2 != bundle.Discovery {
		t.Errorf("expected cached DiscoveryClient instance, got %v, err=%v", disco2, err)
	}
	dyn2, err := bundle.Client.DynamicClient()
	if err != nil || dyn2 != bundle.Dynamic {
		t.Errorf("expected cached DynamicClient instance, got %v, err=%v", dyn2, err)
	}
}

func TestClient_DiscoveryClientFallsBackToInjectedInterface(t *stdtesting.T) {
	fakeClient := k8sfake.NewSimpleClientset()
	c, err := NewClient(Options{}, WithInterface(fakeClient), WithNamespace("staging"))
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	disco, err := c.DiscoveryClient()
	if err != nil {
		t.Fatalf("DiscoveryClient failed: %v", err)
	}
	if disco == nil {
		t.Fatal("expected non-nil discovery client from injected interface")
	}
}
