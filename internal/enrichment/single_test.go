package enrichment

import (
	"context"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestEnrichKEDA_DynClientNil(t *testing.T) {
	ec := &Context{kedaEnabled: true}
	hpa := kedaManagedHPA("default", "web", "web")

	analysis, entry := enrichKEDA(context.Background(), ec, hpa)
	if analysis != nil {
		t.Errorf("expected nil analysis, got %+v", analysis)
	}
	if entry.State != StateError || entry.Reason == "" {
		t.Errorf("expected error state with reason, got %+v", entry)
	}
}

func TestEnrichKEDA_ScaledObjectFound(t *testing.T) {
	scheme := runtime.NewScheme()
	so := scaledObjectUnstructured("default", "web-so", "Deployment", "web")
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects"}: "ScaledObjectList",
		},
		&so,
	)
	ec := &Context{kedaEnabled: true, dynClient: dyn}
	hpa := kedaManagedHPA("default", "web", "web")

	analysis, entry := enrichKEDA(context.Background(), ec, hpa)
	if analysis == nil {
		t.Fatal("expected non-nil analysis for matched ScaledObject")
	}
	if entry.State != StateActive {
		t.Errorf("expected active state, got %+v", entry)
	}
}

func TestEnrichKEDA_FindError(t *testing.T) {
	scheme := runtime.NewScheme()
	// No list-kind registered for ScaledObjects, and the HPA carries no
	// scaledObjectName annotation, so FindScaledObjectForHPA falls back to
	// FetchScaledObjects (a List call) and fails with the unregistered-kind panic
	// path avoided here by registering an empty list kind and no HPA label.
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects"}: "ScaledObjectList",
		},
	)
	ec := &Context{kedaEnabled: true, dynClient: dyn}
	hpa := kedaManagedHPA("default", "web", "web")
	hpa.Labels = map[string]string{"app.kubernetes.io/managed-by": "keda"}

	analysis, entry := enrichKEDA(context.Background(), ec, hpa)
	if analysis != nil {
		t.Errorf("expected nil analysis on lookup failure, got %+v", analysis)
	}
	if entry.State != StateError || entry.Reason == "" {
		t.Errorf("expected error state with reason when the named ScaledObject is missing, got %+v", entry)
	}
}

func TestEnrichVPA_NoConflict(t *testing.T) {
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{vpaGVRForTest: "VerticalPodAutoscalerList"},
	)
	ec := &Context{vpaEnabled: true, dynClient: dyn}
	hpa := hpaWithResourceMetric("default", "web", "web")
	report := &hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Namespace: "default", Name: "web"}}

	entry := EnrichVPA(context.Background(), ec, hpa, report)
	if entry.State != StateSkipped || entry.Reason == "" {
		t.Errorf("expected skipped state with reason, got %+v", entry)
	}
	if report.Analysis.VPAConflict != nil {
		t.Errorf("expected no VPA conflict recorded, got %+v", report.Analysis.VPAConflict)
	}
}

func TestEnrichVPA_ConflictFound(t *testing.T) {
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{vpaGVRForTest: "VerticalPodAutoscalerList"},
		vpaUnstructured("default", "web-vpa", "Deployment", "web", "Auto"),
	)
	ec := &Context{vpaEnabled: true, dynClient: dyn}
	hpa := hpaWithResourceMetric("default", "web", "web")
	report := &hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Namespace: "default", Name: "web"}}

	entry := EnrichVPA(context.Background(), ec, hpa, report)
	if entry.State != StateActive {
		t.Errorf("expected active state, got %+v", entry)
	}
	if report.Analysis.VPAConflict == nil {
		t.Fatal("expected VPA conflict to be recorded on the report")
	}
}

func TestEnrichReport_FullPath(t *testing.T) {
	scheme := runtime.NewScheme()
	so := scaledObjectUnstructured("default", "web-so", "Deployment", "web")
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects"}: "ScaledObjectList",
			vpaGVRForTest: "VerticalPodAutoscalerList",
		},
		&so,
		vpaUnstructured("default", "web-vpa", "Deployment", "web", "Auto"),
	)
	ec := &Context{kedaEnabled: true, vpaEnabled: true, dynClient: dyn}
	hpa := kedaManagedHPA("default", "web", "web")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type:     autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU},
	}}
	report := &hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Namespace: "default", Name: "web"}}

	EnrichReport(context.Background(), ec, hpa, report, hpaanalysis.HealthWeights{})

	if report.Analysis.KEDAInfo == nil {
		t.Error("expected KEDA info to be populated")
	}
	if report.Analysis.VPAConflict == nil {
		t.Error("expected VPA conflict to be populated")
	}
	if report.Analysis.EnrichmentStatus.KEDA == nil || report.Analysis.EnrichmentStatus.VPA == nil {
		t.Errorf("expected enrichment status for both KEDA and VPA, got %+v", report.Analysis.EnrichmentStatus)
	}
}
