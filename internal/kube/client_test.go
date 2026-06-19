package kube

import (
	"errors"
	stdtesting "testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubefake "k8s.io/client-go/discovery/fake"
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
