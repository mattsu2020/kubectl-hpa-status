package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewDecisionSignalsAdapter(t *testing.T) {
	adapter := NewDecisionSignalsAdapter()
	if adapter == nil {
		t.Fatal("NewDecisionSignalsAdapter() returned nil")
	}

	cfg := adapter.DetectAvailability(nil)
	if cfg.Available {
		t.Error("DetectAvailability() should report unavailable for current K8s versions")
	}
	if cfg.APIVersion != "v2" {
		t.Errorf("APIVersion = %q, want %q", cfg.APIVersion, "v2")
	}
}

func TestDetectKEP6111Fields(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}
	if detectKEP6111Fields(hpa) {
		t.Error("detectKEP6111Fields() should return false for current K8s versions")
	}
}

func TestEstimationAdapterFromStructuredOutput(t *testing.T) {
	adapter := NewDecisionSignalsAdapter()
	result := adapter.FromStructuredOutput(nil)
	if result != nil {
		t.Error("FromStructuredOutput() should return nil for estimation adapter")
	}
}

func TestEstimationAdapterDetectAvailability(t *testing.T) {
	adapter := NewDecisionSignalsAdapter()
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}
	cfg := adapter.DetectAvailability(hpa)
	if cfg.Available {
		t.Error("should not report available")
	}
	if cfg.DetectionMethod == "" {
		t.Error("DetectionMethod should not be empty")
	}
}
